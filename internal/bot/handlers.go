package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/gtrrcat/word-teacher-bot/internal/db" // замени на свой модуль
)

// Храним последний выбранный фильтр для каждого пользователя (в памяти)
var userLastFilter = struct {
	sync.RWMutex
	m map[int64]filterInfo
}{m: make(map[int64]filterInfo)}

type filterInfo struct {
	filterType  string // "level", "category", или "any"
	filterValue string // значение уровня или категории, для "any" пусто
}

// RegisterHandlers регистрирует все обработчики команд
func RegisterHandlers(bh *th.BotHandler, bot *telego.Bot) {
	// ---- /start ----
	bh.Handle(func(ctx *th.Context, update telego.Update) error {
		userID := update.Message.From.ID
		// Проверяем, есть ли пользователь в БД
		var exists bool
		err := db.Conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE telegram_id = $1)", userID).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			// Регистрируем нового пользователя
			_, err = db.Conn.Exec(ctx, "INSERT INTO users (telegram_id) VALUES ($1)", userID)
			if err != nil {
				return err
			}
		}

		// Отправляем приветствие с inline-кнопками
		keyboard := tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("📚 По уровням").WithCallbackData("menu_levels"),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("🏷 По категориям").WithCallbackData("menu_categories"),
			),
		)

		_, err = ctx.Bot().SendMessage(ctx, tu.Message(
			tu.ID(update.Message.Chat.ID),
			"Привет! Я помогу тебе учить английские слова. Выбери, как хочешь получать слова:",
		).WithReplyMarkup(keyboard))
		return err
	}, th.CommandEqual("start"))

	// ---- Обработчик всех callback-запросов (нажатий на кнопки) ----
	bh.HandleCallbackQuery(func(ctx *th.Context, query telego.CallbackQuery) error {
		data := query.Data
		chatID := query.Message.GetChat().ID
		userID := query.From.ID

		// Убираем "часики" у кнопки
		_ = ctx.Bot().AnswerCallbackQuery(ctx, tu.CallbackQuery(query.ID))

		// Получаем последний фильтр пользователя (если есть)
		userLastFilter.RLock()
		lastFilter, exists := userLastFilter.m[userID]
		userLastFilter.RUnlock()
		if !exists {
			// Если нет сохранённого фильтра, используем "any"
			lastFilter = filterInfo{filterType: "any", filterValue: ""}
		}

		switch {
		case data == "menu_levels":
			// Показываем кнопки с уровнями
			levels := []string{"A1", "A2", "B1", "B2", "C1"}
			var rows [][]telego.InlineKeyboardButton
			for _, lvl := range levels {
				rows = append(rows, tu.InlineKeyboardRow(
					tu.InlineKeyboardButton(lvl).WithCallbackData("level_"+lvl),
				))
			}
			rows = append(rows, tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("🔙 Назад").WithCallbackData("menu_main"),
			))
			keyboard := tu.InlineKeyboard(rows...)

			_, err := ctx.Bot().EditMessageText(ctx, &telego.EditMessageTextParams{
				ChatID:      tu.ID(chatID),
				MessageID:   query.Message.GetMessageID(),
				Text:        "Выбери уровень:",
				ReplyMarkup: keyboard,
			})
			return err

		case data == "menu_categories":
			// Показываем список категорий
			rows, err := getCategoriesKeyboard(ctx, chatID)
			if err != nil {
				return err
			}
			rows = append(rows, tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("🔙 Назад").WithCallbackData("menu_main"),
			))
			keyboard := tu.InlineKeyboard(rows...)

			_, err = ctx.Bot().EditMessageText(ctx, &telego.EditMessageTextParams{
				ChatID:      tu.ID(chatID),
				MessageID:   query.Message.GetMessageID(),
				Text:        "Выбери категорию:",
				ReplyMarkup: keyboard,
			})
			return err

		case data == "menu_main":
			// Главное меню
			keyboard := tu.InlineKeyboard(
				tu.InlineKeyboardRow(
					tu.InlineKeyboardButton("📚 По уровням").WithCallbackData("menu_levels"),
				),
				tu.InlineKeyboardRow(
					tu.InlineKeyboardButton("🏷 По категориям").WithCallbackData("menu_categories"),
				),
			)
			_, err := ctx.Bot().EditMessageText(ctx, &telego.EditMessageTextParams{
				ChatID:      tu.ID(chatID),
				MessageID:   query.Message.GetMessageID(),
				Text:        "Выбери, как хочешь получать слова:",
				ReplyMarkup: keyboard,
			})
			return err

		case data == "next":
			// Следующее слово с последним фильтром
			return sendWordForUser(ctx, bot, userID, chatID, lastFilter.filterType, lastFilter.filterValue)

		case strings.HasPrefix(data, "know_"):
			// Пользователь знает слово
			wordIDStr := strings.TrimPrefix(data, "know_")
			wordID, err := strconv.Atoi(wordIDStr)
			if err != nil {
				return nil
			}
			// Удаляем слово из user_words (или помечаем как изученное)
			var userDbID int
			err = db.Conn.QueryRow(ctx, "SELECT id FROM users WHERE telegram_id = $1", userID).Scan(&userDbID)
			if err != nil {
				return err
			}
			// Вариант: удаляем запись, чтобы слово больше не попадалось
			_, err = db.Conn.Exec(ctx, "DELETE FROM user_words WHERE user_id = $1 AND word_id = $2", userDbID, wordID)
			if err != nil {
				log.Printf("Failed to delete learned word: %v", err)
			}
			// Отправляем следующее слово
			return sendWordForUser(ctx, bot, userID, chatID, lastFilter.filterType, lastFilter.filterValue)

		case strings.HasPrefix(data, "dontknow_"):
			// Пользователь не знает слово — просто показываем следующее
			return sendWordForUser(ctx, bot, userID, chatID, lastFilter.filterType, lastFilter.filterValue)

		default:
			// Обработка выбора уровня или категории
			if strings.HasPrefix(data, "level_") {
				level := strings.TrimPrefix(data, "level_")
				// Сохраняем выбор пользователя
				userLastFilter.Lock()
				userLastFilter.m[userID] = filterInfo{filterType: "level", filterValue: level}
				userLastFilter.Unlock()
				return sendWordForUser(ctx, bot, userID, chatID, "level", level)
			}
			if strings.HasPrefix(data, "cat_") {
				category := strings.TrimPrefix(data, "cat_")
				userLastFilter.Lock()
				userLastFilter.m[userID] = filterInfo{filterType: "category", filterValue: category}
				userLastFilter.Unlock()
				return sendWordForUser(ctx, bot, userID, chatID, "category", category)
			}
		}
		return nil
	}, th.AnyCallbackQuery())

	// ---- Команда /next ----
	bh.Handle(func(ctx *th.Context, update telego.Update) error {
		chatID := update.Message.Chat.ID
		userID := update.Message.From.ID
		userLastFilter.RLock()
		lastFilter, exists := userLastFilter.m[userID]
		userLastFilter.RUnlock()
		if !exists {
			lastFilter = filterInfo{filterType: "any", filterValue: ""}
		}
		return sendWordForUser(ctx, bot, userID, chatID, lastFilter.filterType, lastFilter.filterValue)
	}, th.CommandEqual("next"))

	// ---- Команда /help ----
	bh.Handle(func(ctx *th.Context, update telego.Update) error {
		helpText := `Команды:
/start - главное меню
/next - следующее слово
/help - это сообщение`
		_, err := ctx.Bot().SendMessage(ctx, tu.Message(tu.ID(update.Message.Chat.ID), helpText))
		return err
	}, th.CommandEqual("help"))
}

// Вспомогательная функция для получения клавиатуры с категориями
func getCategoriesKeyboard(ctx context.Context, chatID int64) ([][]telego.InlineKeyboardButton, error) {
	rows, err := db.Conn.Query(ctx, "SELECT DISTINCT category FROM dictionary WHERE category IS NOT NULL ORDER BY category")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []string
	for rows.Next() {
		var cat string
		if err := rows.Scan(&cat); err != nil {
			return nil, err
		}
		categories = append(categories, cat)
	}

	var keyboardRows [][]telego.InlineKeyboardButton
	for _, cat := range categories {
		keyboardRows = append(keyboardRows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(cat).WithCallbackData("cat_"+cat),
		))
	}
	return keyboardRows, nil
}

// Функция для отправки слова пользователю
func sendWordForUser(ctx *th.Context, bot *telego.Bot, userID int64, chatID int64, filterType, filterValue string) error {
	var word, translation string
	var wordID int
	var query string
	var args []interface{}

	// Получаем внутренний id пользователя
	var userDbID int
	err := db.Conn.QueryRow(ctx, "SELECT id FROM users WHERE telegram_id = $1", userID).Scan(&userDbID)
	if err != nil {
		return err
	}

	switch filterType {
	case "level":
		query = `
            SELECT d.id, d.word, d.translation FROM dictionary d
            WHERE d.level = $1
              AND NOT EXISTS (SELECT 1 FROM user_words uw WHERE uw.user_id = $2 AND uw.word_id = d.id)
            ORDER BY RANDOM() LIMIT 1
        `
		args = []interface{}{filterValue, userDbID}
	case "category":
		query = `
            SELECT d.id, d.word, d.translation FROM dictionary d
            WHERE d.category = $1
              AND NOT EXISTS (SELECT 1 FROM user_words uw WHERE uw.user_id = $2 AND uw.word_id = d.id)
            ORDER BY RANDOM() LIMIT 1
        `
		args = []interface{}{filterValue, userDbID}
	default: // any
		query = `
            SELECT d.id, d.word, d.translation FROM dictionary d
            WHERE NOT EXISTS (SELECT 1 FROM user_words uw WHERE uw.user_id = $1 AND uw.word_id = d.id)
            ORDER BY RANDOM() LIMIT 1
        `
		args = []interface{}{userDbID}
	}

	err = db.Conn.QueryRow(ctx, query, args...).Scan(&wordID, &word, &translation)
	if err != nil {
		if err.Error() == "no rows in result set" {
			_, err = bot.SendMessage(ctx, tu.Message(tu.ID(chatID), "Поздравляю! Ты выучил все слова из этой категории/уровня!"))
			return err
		}
		return err
	}

	// Добавляем слово в user_words, если ещё не добавлено (на случай, если оно уже было удалено или не добавлено)
	_, err = db.Conn.Exec(ctx, "INSERT INTO user_words (user_id, word_id, status) VALUES ($1, $2, 'new') ON CONFLICT (user_id, word_id) DO NOTHING", userDbID, wordID)
	if err != nil {
		log.Printf("Failed to insert into user_words: %v", err)
	}

	// Кнопки для слова
	keyboard := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("✅ Знаю").WithCallbackData(fmt.Sprintf("know_%d", wordID)),
			tu.InlineKeyboardButton("❌ Не знаю").WithCallbackData(fmt.Sprintf("dontknow_%d", wordID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("➡️ Следующее").WithCallbackData("next"),
		),
	)

	msgText := fmt.Sprintf("*%s* — %s", word, translation)
	_, err = bot.SendMessage(ctx, tu.Message(tu.ID(chatID), msgText).WithParseMode("Markdown").WithReplyMarkup(keyboard))
	return err
}
