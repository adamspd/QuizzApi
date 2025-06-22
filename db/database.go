package db

import (
	"database/sql"
	"fmt"

	"github.com/adamspd/QuizzApi/utils"
	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
}

func InitDB(dbPath string) (*DB, error) {
	utils.LogStartup("Initializing database at: %s", dbPath)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		utils.LogError("Failed to open database: %v", err)
		return nil, err
	}

	if err := db.Ping(); err != nil {
		utils.LogError("Failed to ping database: %v", err)
		return nil, err
	}

	utils.LogStartup("Database connection established")

	if err := createTablesWithAuth(db); err != nil {
		utils.LogError("Failed to create tables: %v", err)
		return nil, err
	}

	utils.LogStartup("Database tables initialized successfully")
	return &DB{db}, nil
}

func createTablesWithAuth(db *sql.DB) error {
	queries := []string{
		// Users table
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			email TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'moderator', 'admin')),
			is_active BOOLEAN NOT NULL DEFAULT 1,
			email_verified BOOLEAN NOT NULL DEFAULT 0,
			email_verified_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Email verification tokens table
		`CREATE TABLE IF NOT EXISTS email_verifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			email TEXT NOT NULL,
			token TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			used_at DATETIME,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS user_preferences (
			user_id INTEGER PRIMARY KEY,
			practice_session_length INTEGER NOT NULL DEFAULT 10,
			difficulty_preference TEXT NOT NULL DEFAULT 'adaptive' CHECK (difficulty_preference IN ('easy', 'medium', 'hard', 'adaptive', 'mixed')),
			category_preference TEXT, -- JSON array or NULL for all categories
			review_mode TEXT NOT NULL DEFAULT 'immediate' CHECK (review_mode IN ('immediate', 'end_of_session')),
			auto_advance_timing_open INTEGER NOT NULL DEFAULT 60000,
			auto_advance_timing_choice INTEGER NOT NULL DEFAULT 30000,
			question_randomization BOOLEAN NOT NULL DEFAULT 0,
			skip_answered_questions BOOLEAN NOT NULL DEFAULT 0,
			focus_weak_areas BOOLEAN NOT NULL DEFAULT 1,
			theme_mode TEXT NOT NULL DEFAULT 'system' CHECK (theme_mode IN ('light', 'dark', 'system')),
			stats_visibility BOOLEAN NOT NULL DEFAULT 1,
			interface_language TEXT NOT NULL DEFAULT 'fr',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,

		// Updated questions table
		`CREATE TABLE IF NOT EXISTS questions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			category TEXT NOT NULL,
			question TEXT NOT NULL,
			question_type TEXT NOT NULL DEFAULT 'open_text',
			choices TEXT,
			answer TEXT NOT NULL,
			keywords TEXT,
			difficulty TEXT NOT NULL,
			created_by INTEGER NOT NULL DEFAULT 1,
			status TEXT NOT NULL DEFAULT 'approved' CHECK (status IN ('pending', 'approved', 'rejected')),
			approved_by INTEGER,
			approved_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (created_by) REFERENCES users(id),
			FOREIGN KEY (approved_by) REFERENCES users(id)
		)`,

		// Progress table
		`CREATE TABLE IF NOT EXISTS progress (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			question_id INTEGER NOT NULL,
			user_answer TEXT NOT NULL,
			is_correct BOOLEAN NOT NULL,
			answered_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			time_taken_seconds INTEGER,
			FOREIGN KEY (user_id) REFERENCES users(id),
			FOREIGN KEY (question_id) REFERENCES questions(id)
		)`,
	}

	for i, query := range queries {
		utils.LogDB("Creating table %d/%d", i+1, len(queries))
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Create indexes for performance
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_questions_status ON questions(status)",
		"CREATE INDEX IF NOT EXISTS idx_questions_created_by ON questions(created_by)",
		"CREATE INDEX IF NOT EXISTS idx_progress_user_id ON progress(user_id)",
		"CREATE INDEX IF NOT EXISTS idx_email_verifications_token ON email_verifications(token)",
		"CREATE INDEX IF NOT EXISTS idx_email_verifications_user_id ON email_verifications(user_id)",
		"CREATE INDEX IF NOT EXISTS idx_user_preferences_user_id ON user_preferences(user_id)",
	}

	for _, index := range indexes {
		if _, err := db.Exec(index); err != nil {
			utils.LogDB("Failed to create index (non-fatal): %v", err)
		}
	}

	return nil
}
