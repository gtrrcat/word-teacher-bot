package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/joho/godotenv"
	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"

	"github.com/gtrrcat/word-teacher-bot/internal/bot"
	"github.com/gtrrcat/word-teacher-bot/internal/db"
)

func main() {
	// Загружаем .env
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, relying on system env")
	}

	// Инициализация БД
	if err := db.InitDB(); err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer db.CloseDB()

	// Токен бота
	botToken := os.Getenv("BOT_TOKEN")
	if botToken == "" {
		log.Fatal("BOT_TOKEN not set")
	}

	// Создаём бота
	botInstance, err := telego.NewBot(botToken, telego.WithDefaultDebugLogger())
	if err != nil {
		log.Fatal(err)
	}

	// Контекст для graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer stop()

	// Проверяем подключение к Telegram
	botUser, err := botInstance.GetMe(ctx)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Bot started: @%s", botUser.Username)

	// Получаем обновления через long polling
	updates, err := botInstance.UpdatesViaLongPolling(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}

	// Создаём обработчик
	bh, err := th.NewBotHandler(botInstance, updates)
	if err != nil {
		log.Fatal(err)
	}
	defer bh.Stop()

	// Регистрируем наши обработчики из пакета bot
	bot.RegisterHandlers(bh, botInstance)

	log.Println("Bot is running. Press Ctrl+C to stop.")
	go bh.Start()
	<-ctx.Done()
	log.Println("Shutting down...")
}
