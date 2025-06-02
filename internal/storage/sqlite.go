package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
}

type Question struct {
	ID         int       `json:"id"`
	Category   string    `json:"category"`
	Question   string    `json:"question"`
	Answer     string    `json:"answer"`
	Keywords   []string  `json:"keywords"`
	Difficulty string    `json:"difficulty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type QuestionRequest struct {
	Category   string   `json:"category"`
	Question   string   `json:"question"`
	Answer     string   `json:"answer"`
	Keywords   []string `json:"keywords"`
	Difficulty string   `json:"difficulty"`
}

func InitDB(dbPath string) (*DB, error) {
	log.Printf("[DB] Initializing database at: %s", dbPath)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Printf("[DB ERROR] Failed to open database: %v", err)
		return nil, err
	}

	if err := db.Ping(); err != nil {
		log.Printf("[DB ERROR] Failed to ping database: %v", err)
		return nil, err
	}

	log.Println("[DB] Database connection established")

	// Create tables
	if err := createTables(db); err != nil {
		log.Printf("[DB ERROR] Failed to create tables: %v", err)
		return nil, err
	}

	log.Println("[DB] Database tables initialized successfully")
	return &DB{db}, nil
}

func createTables(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            username TEXT UNIQUE NOT NULL,
            password_hash TEXT NOT NULL,
            role TEXT NOT NULL DEFAULT 'user',
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )`,

		`CREATE TABLE IF NOT EXISTS questions (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            category TEXT NOT NULL,
            question TEXT NOT NULL,
            answer TEXT NOT NULL,
            keywords TEXT,
            difficulty TEXT NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )`,

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
		log.Printf("[DB] Creating table %d/3", i+1)
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return nil
}

func (db *DB) GetAllQuestions() ([]Question, error) {
	log.Println("[DB] Executing query: GetAllQuestions")
	start := time.Now()

	rows, err := db.Query(`
        SELECT id, category, question, answer, keywords, difficulty, created_at, updated_at 
        FROM questions ORDER BY created_at DESC
    `)
	if err != nil {
		log.Printf("[DB ERROR] GetAllQuestions query failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	var questions []Question
	for rows.Next() {
		var q Question
		var keywordsJSON string

		err := rows.Scan(&q.ID, &q.Category, &q.Question, &q.Answer, &keywordsJSON,
			&q.Difficulty, &q.CreatedAt, &q.UpdatedAt)
		if err != nil {
			log.Printf("[DB ERROR] Failed to scan question row: %v", err)
			return nil, err
		}

		// Parse keywords JSON
		if keywordsJSON != "" {
			json.Unmarshal([]byte(keywordsJSON), &q.Keywords)
		}

		questions = append(questions, q)
	}

	duration := time.Since(start)
	log.Printf("[DB] GetAllQuestions completed: %d questions in %v", len(questions), duration)
	return questions, nil
}

func (db *DB) GetQuestionByID(id int) (*Question, error) {
	log.Printf("[DB] Executing query: GetQuestionByID(%d)", id)
	start := time.Now()

	var q Question
	var keywordsJSON string

	err := db.QueryRow(`
        SELECT id, category, question, answer, keywords, difficulty, created_at, updated_at 
        FROM questions WHERE id = ?
    `, id).Scan(&q.ID, &q.Category, &q.Question, &q.Answer, &keywordsJSON,
		&q.Difficulty, &q.CreatedAt, &q.UpdatedAt)

	if err != nil {
		duration := time.Since(start)
		if err == sql.ErrNoRows {
			log.Printf("[DB] Question ID %d not found (%v)", id, duration)
		} else {
			log.Printf("[DB ERROR] GetQuestionByID(%d) failed: %v (%v)", id, err, duration)
		}
		return nil, err
	}

	// Parse keywords JSON
	if keywordsJSON != "" {
		json.Unmarshal([]byte(keywordsJSON), &q.Keywords)
	}

	duration := time.Since(start)
	log.Printf("[DB] GetQuestionByID(%d) completed in %v", id, duration)
	return &q, nil
}

func (db *DB) CreateQuestion(req QuestionRequest) (*Question, error) {
	log.Printf("[DB] Creating question in category '%s'", req.Category)
	start := time.Now()

	keywordsJSON, _ := json.Marshal(req.Keywords)

	result, err := db.Exec(`
        INSERT INTO questions (category, question, answer, keywords, difficulty)
        VALUES (?, ?, ?, ?, ?)
    `, req.Category, req.Question, req.Answer, string(keywordsJSON), req.Difficulty)

	if err != nil {
		duration := time.Since(start)
		log.Printf("[DB ERROR] CreateQuestion failed: %v (%v)", err, duration)
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		log.Printf("[DB ERROR] Failed to get LastInsertId: %v", err)
		return nil, err
	}

	duration := time.Since(start)
	log.Printf("[DB] Question created with ID %d in %v", id, duration)

	return db.GetQuestionByID(int(id))
}

func (db *DB) UpdateQuestion(id int, req QuestionRequest) (*Question, error) {
	log.Printf("[DB] Updating question ID %d", id)
	start := time.Now()

	// Get current question to check if answer changed
	current, err := db.GetQuestionByID(id)
	if err != nil {
		return nil, err
	}

	keywordsJSON, _ := json.Marshal(req.Keywords)

	// Update question
	result, err := db.Exec(`
        UPDATE questions 
        SET category = ?, question = ?, answer = ?, keywords = ?, difficulty = ?, updated_at = CURRENT_TIMESTAMP
        WHERE id = ?
    `, req.Category, req.Question, req.Answer, string(keywordsJSON), req.Difficulty, id)

	if err != nil {
		duration := time.Since(start)
		log.Printf("[DB ERROR] UpdateQuestion(%d) failed: %v (%v)", id, err, duration)
		return nil, err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		log.Printf("[DB] UpdateQuestion(%d): no rows affected", id)
	}

	// If answer changed, delete progress for this question
	if current.Answer != req.Answer {
		log.Printf("[DB] Answer changed for question %d, clearing progress", id)
		deleteResult, err := db.Exec("DELETE FROM progress WHERE question_id = ?", id)
		if err != nil {
			log.Printf("[DB ERROR] Failed to clear progress for question %d: %v", id, err)
			return nil, err
		}
		progressDeleted, _ := deleteResult.RowsAffected()
		log.Printf("[DB] Cleared %d progress entries for question %d", progressDeleted, id)
	}

	duration := time.Since(start)
	log.Printf("[DB] UpdateQuestion(%d) completed in %v", id, duration)

	return db.GetQuestionByID(id)
}

func (db *DB) DeleteQuestion(id int) error {
	log.Printf("[DB] Deleting question ID %d", id)
	start := time.Now()

	// Delete progress first (foreign key constraint)
	progressResult, err := db.Exec("DELETE FROM progress WHERE question_id = ?", id)
	if err != nil {
		log.Printf("[DB ERROR] Failed to delete progress for question %d: %v", id, err)
		return err
	}

	progressDeleted, _ := progressResult.RowsAffected()
	if progressDeleted > 0 {
		log.Printf("[DB] Deleted %d progress entries for question %d", progressDeleted, id)
	}

	// Delete question
	questionResult, err := db.Exec("DELETE FROM questions WHERE id = ?", id)
	if err != nil {
		duration := time.Since(start)
		log.Printf("[DB ERROR] Failed to delete question %d: %v (%v)", id, err, duration)
		return err
	}

	rowsAffected, _ := questionResult.RowsAffected()
	duration := time.Since(start)

	if rowsAffected == 0 {
		log.Printf("[DB] DeleteQuestion(%d): no rows affected (%v)", id, duration)
	} else {
		log.Printf("[DB] DeleteQuestion(%d) completed in %v", id, duration)
	}

	return nil
}

// Progress tracking types
type Progress struct {
	ID               int       `json:"id"`
	UserID           int       `json:"user_id"`
	QuestionID       int       `json:"question_id"`
	UserAnswer       string    `json:"user_answer"`
	IsCorrect        bool      `json:"is_correct"`
	AnsweredAt       time.Time `json:"answered_at"`
	TimeTakenSeconds int       `json:"time_taken_seconds"`
}

type ProgressRequest struct {
	QuestionID       int    `json:"question_id"`
	UserAnswer       string `json:"user_answer"`
	TimeTakenSeconds int    `json:"time_taken_seconds"`
}

type Stats struct {
	TotalQuestions int                     `json:"total_questions"`
	Answered       int                     `json:"answered"`
	Correct        int                     `json:"correct"`
	Accuracy       float64                 `json:"accuracy"`
	Streak         int                     `json:"streak"`
	Categories     map[string]CategoryStat `json:"categories"`
}

type CategoryStat struct {
	Answered int `json:"answered"`
	Correct  int `json:"correct"`
}

// Record a progress entry
func (db *DB) RecordProgress(userID int, req ProgressRequest) (*Progress, error) {
	log.Printf("[DB] Recording progress: user %d, question %d", userID, req.QuestionID)
	start := time.Now()

	// Get the question to check correct answer
	question, err := db.GetQuestionByID(req.QuestionID)
	if err != nil {
		log.Printf("[DB ERROR] Failed to get question %d for progress check: %v", req.QuestionID, err)
		return nil, err
	}

	// Simple answer checking - normalize both answers for comparison
	isCorrect := normalizeAnswer(req.UserAnswer) == normalizeAnswer(question.Answer)

	log.Printf("[DB] Answer check: user='%s' vs correct='%s' -> %t",
		req.UserAnswer, question.Answer, isCorrect)

	result, err := db.Exec(`
        INSERT INTO progress (user_id, question_id, user_answer, is_correct, time_taken_seconds)
        VALUES (?, ?, ?, ?, ?)
    `, userID, req.QuestionID, req.UserAnswer, isCorrect, req.TimeTakenSeconds)

	if err != nil {
		duration := time.Since(start)
		log.Printf("[DB ERROR] RecordProgress failed: %v (%v)", err, duration)
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		log.Printf("[DB ERROR] Failed to get progress LastInsertId: %v", err)
		return nil, err
	}

	duration := time.Since(start)
	log.Printf("[DB] Progress recorded with ID %d (correct: %t) in %v", id, isCorrect, duration)

	return db.GetProgressByID(int(id))
}

func (db *DB) GetProgressByID(id int) (*Progress, error) {
	log.Printf("[DB] Executing query: GetProgressByID(%d)", id)

	var p Progress

	err := db.QueryRow(`
        SELECT id, user_id, question_id, user_answer, is_correct, answered_at, time_taken_seconds
        FROM progress WHERE id = ?
    `, id).Scan(&p.ID, &p.UserID, &p.QuestionID, &p.UserAnswer, &p.IsCorrect, &p.AnsweredAt, &p.TimeTakenSeconds)

	if err != nil {
		log.Printf("[DB ERROR] GetProgressByID(%d) failed: %v", id, err)
		return nil, err
	}

	return &p, nil
}

// Get user statistics
func (db *DB) GetUserStats(userID int) (*Stats, error) {
	log.Printf("[DB] Calculating stats for user %d", userID)
	start := time.Now()

	stats := &Stats{
		Categories: make(map[string]CategoryStat),
	}

	// Get total questions count
	err := db.QueryRow("SELECT COUNT(*) FROM questions").Scan(&stats.TotalQuestions)
	if err != nil {
		log.Printf("[DB ERROR] Failed to count total questions: %v", err)
		return nil, err
	}

	// Get user's progress stats
	err = db.QueryRow(`
        SELECT COUNT(*) as answered, SUM(CASE WHEN is_correct THEN 1 ELSE 0 END) as correct
        FROM progress WHERE user_id = ?
    `, userID).Scan(&stats.Answered, &stats.Correct)
	if err != nil {
		log.Printf("[DB ERROR] Failed to get user progress stats: %v", err)
		return nil, err
	}

	// Calculate accuracy
	if stats.Answered > 0 {
		stats.Accuracy = float64(stats.Correct) / float64(stats.Answered)
	}

	// Get current streak
	stats.Streak = db.getCurrentStreak(userID)

	// Get category breakdown
	rows, err := db.Query(`
        SELECT q.category, 
               COUNT(*) as answered,
               SUM(CASE WHEN p.is_correct THEN 1 ELSE 0 END) as correct
        FROM progress p
        JOIN questions q ON p.question_id = q.id
        WHERE p.user_id = ?
        GROUP BY q.category
    `, userID)
	if err != nil {
		log.Printf("[DB ERROR] Failed to get category stats: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var category string
		var answered, correct int

		err := rows.Scan(&category, &answered, &correct)
		if err != nil {
			log.Printf("[DB ERROR] Failed to scan category stats: %v", err)
			return nil, err
		}

		stats.Categories[category] = CategoryStat{
			Answered: answered,
			Correct:  correct,
		}
	}

	duration := time.Since(start)
	log.Printf("[DB] Stats calculated for user %d: %d/%d correct (%.1f%%), streak %d, %d categories (%v)",
		userID, stats.Correct, stats.Answered, stats.Accuracy*100, stats.Streak, len(stats.Categories), duration)

	return stats, nil
}

// Get current correct streak
func (db *DB) getCurrentStreak(userID int) int {
	log.Printf("[DB] Calculating streak for user %d", userID)

	rows, err := db.Query(`
        SELECT is_correct FROM progress 
        WHERE user_id = ? 
        ORDER BY answered_at DESC
        LIMIT 50
    `, userID)
	if err != nil {
		log.Printf("[DB ERROR] Failed to get streak data: %v", err)
		return 0
	}
	defer rows.Close()

	streak := 0
	for rows.Next() {
		var isCorrect bool
		if err := rows.Scan(&isCorrect); err != nil {
			log.Printf("[DB ERROR] Failed to scan streak row: %v", err)
			break
		}

		if isCorrect {
			streak++
		} else {
			break
		}
	}

	log.Printf("[DB] Current streak for user %d: %d", userID, streak)

	return streak
}

// Get questions for next practice session
func (db *DB) GetNextQuestions(userID int, count int) ([]Question, error) {
	log.Printf("[DB] Getting next %d questions for user %d", count, userID)
	start := time.Now()

	// Priority order:
	// 1. Never answered questions
	// 2. Questions answered incorrectly recently
	// 3. Questions due for review based on spaced repetition

	query := `
        SELECT DISTINCT q.id, q.category, q.question, q.answer, q.keywords, q.difficulty, q.created_at, q.updated_at,
               COALESCE(last_progress.is_correct, 0) as last_correct,
               COALESCE(last_progress.answered_at, '1970-01-01') as last_answered,
               COALESCE(correct_streak.streak, 0) as streak
        FROM questions q
        LEFT JOIN (
            SELECT question_id, is_correct, answered_at,
                   ROW_NUMBER() OVER (PARTITION BY question_id ORDER BY answered_at DESC) as rn
            FROM progress WHERE user_id = ?
        ) last_progress ON q.id = last_progress.question_id AND last_progress.rn = 1
        LEFT JOIN (
            SELECT question_id, COUNT(*) as streak
            FROM (
                SELECT question_id, is_correct,
                       ROW_NUMBER() OVER (PARTITION BY question_id ORDER BY answered_at DESC) as rn
                FROM progress WHERE user_id = ?
            ) recent_progress
            WHERE rn <= 10 AND is_correct = 1
            GROUP BY question_id
        ) correct_streak ON q.id = correct_streak.question_id
        ORDER BY 
            CASE WHEN last_progress.answered_at IS NULL THEN 0 ELSE 1 END,  -- Never answered first
            CASE WHEN last_progress.is_correct = 0 THEN 0 ELSE 1 END,       -- Wrong answers next
            last_progress.answered_at ASC                                    -- Oldest attempts next
        LIMIT ?
    `

	rows, err := db.Query(query, userID, userID, count)
	if err != nil {
		duration := time.Since(start)
		log.Printf("[DB ERROR] GetNextQuestions failed: %v (%v)", err, duration)
		return nil, err
	}
	defer rows.Close()

	var questions []Question
	neverAnswered := 0
	incorrectAnswers := 0

	for rows.Next() {
		var q Question
		var keywordsJSON string
		var lastCorrect bool
		var lastAnswered string
		var streak int

		err := rows.Scan(&q.ID, &q.Category, &q.Question, &q.Answer, &keywordsJSON,
			&q.Difficulty, &q.CreatedAt, &q.UpdatedAt,
			&lastCorrect, &lastAnswered, &streak)
		if err != nil {
			log.Printf("[DB ERROR] Failed to scan next question row: %v", err)
			return nil, err
		}

		// Parse keywords JSON
		if keywordsJSON != "" {
			json.Unmarshal([]byte(keywordsJSON), &q.Keywords)
		}

		// Count question types for logging
		if lastAnswered == "1970-01-01" {
			neverAnswered++
		} else if !lastCorrect {
			incorrectAnswers++
		}

		questions = append(questions, q)
	}

	duration := time.Since(start)
	log.Printf("[DB] GetNextQuestions completed: %d questions (%d never answered, %d incorrect) in %v",
		len(questions), neverAnswered, incorrectAnswers, duration)

	return questions, nil
}

// Helper function to normalize answers for comparison
func normalizeAnswer(answer string) string {
	// Simple normalization - trim spaces and convert to lowercase
	// You can make this more sophisticated later (remove accents, handle synonyms, etc.)
	normalized := strings.ToLower(strings.TrimSpace(answer))
	log.Printf("[DB] Normalized answer: '%s' -> '%s'", answer, normalized)
	return normalized
}
