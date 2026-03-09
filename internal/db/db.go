package db

import (
	"context"
	"log"
	"os"

	"github.com/jackc/pgx/v5"
)

var Conn *pgx.Conn

func InitDB() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL not set in .env file")
	}

	var err error
	Conn, err = pgx.Connect(context.Background(), databaseURL)
	if err != nil {
		return err
	}

	// Создаём таблицы (новую структуру)
	createTables := `
    -- Таблица пользователей (была, оставляем)
    CREATE TABLE IF NOT EXISTS users (
        id SERIAL PRIMARY KEY,
        telegram_id BIGINT UNIQUE NOT NULL,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );

    -- Таблица словаря (общая для всех)
    CREATE TABLE IF NOT EXISTS dictionary (
        id SERIAL PRIMARY KEY,
        word TEXT NOT NULL,
        translation TEXT NOT NULL,
        level TEXT,
        category TEXT,
        example TEXT,
        notes TEXT,
        UNIQUE(word, translation)
    );

    -- Таблица прогресса пользователя
    CREATE TABLE IF NOT EXISTS user_words (
        user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
        word_id INTEGER NOT NULL REFERENCES dictionary(id) ON DELETE CASCADE,
        status TEXT DEFAULT 'new',
        correct_count INTEGER DEFAULT 0,
        next_review TIMESTAMP,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (user_id, word_id)
    );

    -- Удаляем старую таблицу words, если она есть (чтобы не мешала)
    DROP TABLE IF EXISTS words;
    `
	_, err = Conn.Exec(context.Background(), createTables)
	if err != nil {
		return err
	}

	// Проверяем, есть ли слова в словаре, если нет — добавляем начальные
	var count int
	err = Conn.QueryRow(context.Background(), "SELECT COUNT(*) FROM dictionary").Scan(&count)
	if err != nil {
		return err
	}
	// Список всех желаемых слов (20 штук)
	initialWords := []struct {
		word, translation, level, category string
	}{
		{"hello", "привет", "A1", "общее"},
		{"goodbye", "до свидания", "A1", "общее"},
		{"cat", "кошка", "A1", "животные"},
		{"dog", "собака", "A1", "животные"},
		{"apple", "яблоко", "A1", "еда"},
		{"bread", "хлеб", "A1", "еда"},
		{"car", "машина", "A1", "транспорт"},
		{"house", "дом", "A1", "жильё"},
		{"book", "книга", "A1", "обучение"},
		{"pen", "ручка", "A1", "обучение"},
		{"water", "вода", "A1", "еда"},
		{"tea", "чай", "A1", "еда"},
		{"coffee", "кофе", "A1", "еда"},
		{"mother", "мама", "A1", "семья"},
		{"father", "папа", "A1", "семья"},
		{"brother", "брат", "A1", "семья"},
		{"sister", "сестра", "A1", "семья"},
		{"friend", "друг", "A1", "люди"},
		{"city", "город", "A1", "места"},
		{"street", "улица", "A1", "места"},
	}

	for _, w := range initialWords {
		// INSERT с игнорированием конфликта по уникальной паре (word, translation)
		_, err = Conn.Exec(context.Background(),
			`INSERT INTO dictionary (word, translation, level, category) 
         VALUES ($1, $2, $3, $4) 
         ON CONFLICT (word, translation) DO NOTHING`,
			w.word, w.translation, w.level, w.category)
		if err != nil {
			log.Printf("Failed to insert word %s: %v", w.word, err)
		}
	}
	log.Println("Dictionary sync completed")

	log.Println("Database connected and new tables created")
	return nil
}

func CloseDB() {
	if Conn != nil {
		Conn.Close(context.Background())
	}
}
