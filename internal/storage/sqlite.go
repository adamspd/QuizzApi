// internal/storage/sqlite.go

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
	ID           int       `json:"id"`
	Category     string    `json:"category"`
	Question     string    `json:"question"`
	QuestionType string    `json:"question_type"`
	Choices      []string  `json:"choices,omitempty"`
	Answer       string    `json:"answer"`
	Keywords     []string  `json:"keywords"`
	Difficulty   string    `json:"difficulty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type QuestionRequest struct {
	Category     string   `json:"category"`
	Question     string   `json:"question"`
	QuestionType string   `json:"question_type"`
	Choices      []string `json:"choices,omitempty"`
	Answer       string   `json:"answer"`
	Keywords     []string `json:"keywords"`
	Difficulty   string   `json:"difficulty"`
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
            question_type TEXT NOT NULL DEFAULT 'open_text',
            choices TEXT,
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

	// Add new columns to existing questions table if they don't exist
	log.Println("[DB] Checking for schema updates...")

	// Check if question_type column exists
	rows, err := db.Query("PRAGMA table_info(questions)")
	if err != nil {
		return fmt.Errorf("failed to check table info: %w", err)
	}
	defer rows.Close()

	hasQuestionType := false
	hasChoices := false

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk bool
		var defaultValue sql.NullString

		err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk)
		if err != nil {
			return fmt.Errorf("failed to scan table info: %w", err)
		}

		if name == "question_type" {
			hasQuestionType = true
		}
		if name == "choices" {
			hasChoices = true
		}
	}

	// Add missing columns
	if !hasQuestionType {
		log.Println("[DB] Adding question_type column...")
		if _, err := db.Exec("ALTER TABLE questions ADD COLUMN question_type TEXT DEFAULT 'open_text'"); err != nil {
			return fmt.Errorf("failed to add question_type column: %w", err)
		}
	}

	if !hasChoices {
		log.Println("[DB] Adding choices column...")
		if _, err := db.Exec("ALTER TABLE questions ADD COLUMN choices TEXT"); err != nil {
			return fmt.Errorf("failed to add choices column: %w", err)
		}
	}

	return nil
}

func (db *DB) GetAllQuestions() ([]Question, error) {
	log.Println("[DB] Executing query: GetAllQuestions")
	start := time.Now()

	rows, err := db.Query(`
        SELECT id, category, question, question_type, choices, answer, keywords, difficulty, created_at, updated_at 
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
		var keywordsJSON, choicesJSON sql.NullString

		err := rows.Scan(&q.ID, &q.Category, &q.Question, &q.QuestionType, &choicesJSON, &q.Answer, &keywordsJSON,
			&q.Difficulty, &q.CreatedAt, &q.UpdatedAt)
		if err != nil {
			log.Printf("[DB ERROR] Failed to scan question row: %v", err)
			return nil, err
		}

		// Parse keywords JSON
		if keywordsJSON.Valid && keywordsJSON.String != "" {
			json.Unmarshal([]byte(keywordsJSON.String), &q.Keywords)
		}

		// Parse choices JSON
		if choicesJSON.Valid && choicesJSON.String != "" {
			json.Unmarshal([]byte(choicesJSON.String), &q.Choices)
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
	var keywordsJSON, choicesJSON sql.NullString

	err := db.QueryRow(`
        SELECT id, category, question, question_type, choices, answer, keywords, difficulty, created_at, updated_at 
        FROM questions WHERE id = ?
    `, id).Scan(&q.ID, &q.Category, &q.Question, &q.QuestionType, &choicesJSON, &q.Answer, &keywordsJSON,
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
	if keywordsJSON.Valid && keywordsJSON.String != "" {
		json.Unmarshal([]byte(keywordsJSON.String), &q.Keywords)
	}

	// Parse choices JSON
	if choicesJSON.Valid && choicesJSON.String != "" {
		json.Unmarshal([]byte(choicesJSON.String), &q.Choices)
	}

	duration := time.Since(start)
	log.Printf("[DB] GetQuestionByID(%d) completed in %v", id, duration)
	return &q, nil
}

func (db *DB) CreateQuestion(req QuestionRequest) (*Question, error) {
	log.Printf("[DB] Creating question in category '%s', type '%s'", req.Category, req.QuestionType)
	start := time.Now()

	// Validate question type
	validTypes := []string{"open_text", "multiple_choice", "true_false", "multiple_select"}
	questionType := strings.ToLower(strings.TrimSpace(req.QuestionType))
	if questionType == "" {
		questionType = "open_text" // Default
	}

	isValidType := false
	for _, vt := range validTypes {
		if questionType == vt {
			isValidType = true
			break
		}
	}

	if !isValidType {
		return nil, fmt.Errorf("invalid question type '%s', must be one of: %v", req.QuestionType, validTypes)
	}

	keywordsJSON, _ := json.Marshal(req.Keywords)

	var choicesJSON []byte
	if len(req.Choices) > 0 {
		choicesJSON, _ = json.Marshal(req.Choices)
	}

	result, err := db.Exec(`
        INSERT INTO questions (category, question, question_type, choices, answer, keywords, difficulty)
        VALUES (?, ?, ?, ?, ?, ?, ?)
    `, req.Category, req.Question, questionType, string(choicesJSON), req.Answer, string(keywordsJSON), req.Difficulty)

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
	log.Printf("[DB] Question created with ID %d, type '%s' in %v", id, questionType, duration)

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

	// Validate question type
	validTypes := []string{"open_text", "multiple_choice", "true_false", "multiple_select"}
	questionType := strings.ToLower(strings.TrimSpace(req.QuestionType))
	if questionType == "" {
		questionType = current.QuestionType // Keep existing type if not specified
	}

	isValidType := false
	for _, vt := range validTypes {
		if questionType == vt {
			isValidType = true
			break
		}
	}

	if !isValidType {
		return nil, fmt.Errorf("invalid question type '%s', must be one of: %v", req.QuestionType, validTypes)
	}

	keywordsJSON, _ := json.Marshal(req.Keywords)

	var choicesJSON []byte
	if len(req.Choices) > 0 {
		choicesJSON, _ = json.Marshal(req.Choices)
	}

	// Update question
	result, err := db.Exec(`
        UPDATE questions 
        SET category = ?, question = ?, question_type = ?, choices = ?, answer = ?, keywords = ?, difficulty = ?, updated_at = CURRENT_TIMESTAMP
        WHERE id = ?
    `, req.Category, req.Question, questionType, string(choicesJSON), req.Answer, string(keywordsJSON), req.Difficulty, id)

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

	// Check answer based on question type
	isCorrect := checkAnswer(question, req.UserAnswer)

	log.Printf("[DB] Answer check for %s question: user='%s' vs correct='%s' -> %t",
		question.QuestionType, req.UserAnswer, question.Answer, isCorrect)

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

// Check answer based on question type
func checkAnswer(question *Question, userAnswer string) bool {
	switch question.QuestionType {
	case "open_text":
		return normalizeAnswer(userAnswer) == normalizeAnswer(question.Answer)

	case "multiple_choice", "true_false":
		// For multiple choice and true/false, exact match with correct answer
		return normalizeAnswer(userAnswer) == normalizeAnswer(question.Answer)

	case "multiple_select":
		// For multiple select, the correct answer is stored as JSON array
		var correctAnswers []string
		if err := json.Unmarshal([]byte(question.Answer), &correctAnswers); err != nil {
			log.Printf("[DB ERROR] Failed to parse multiple_select answer: %v", err)
			return false
		}

		// User answer should be comma-separated values
		userAnswers := strings.Split(userAnswer, ",")
		for i, answer := range userAnswers {
			userAnswers[i] = strings.TrimSpace(answer)
		}

		// Normalize both arrays for comparison
		normalizedCorrect := make([]string, len(correctAnswers))
		for i, answer := range correctAnswers {
			normalizedCorrect[i] = normalizeAnswer(answer)
		}

		normalizedUser := make([]string, len(userAnswers))
		for i, answer := range userAnswers {
			normalizedUser[i] = normalizeAnswer(answer)
		}

		// Check if arrays contain same elements (order doesn't matter)
		if len(normalizedCorrect) != len(normalizedUser) {
			return false
		}

		for _, correct := range normalizedCorrect {
			found := false
			for _, user := range normalizedUser {
				if correct == user {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}

		return true

	default:
		log.Printf("[DB ERROR] Unknown question type: %s", question.QuestionType)
		return false
	}
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
        SELECT DISTINCT q.id, q.category, q.question, q.question_type, q.choices, q.answer, q.keywords, q.difficulty, q.created_at, q.updated_at,
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
		var keywordsJSON, choicesJSON sql.NullString
		var lastCorrect bool
		var lastAnswered string
		var streak int

		err := rows.Scan(&q.ID, &q.Category, &q.Question, &q.QuestionType, &choicesJSON, &q.Answer, &keywordsJSON,
			&q.Difficulty, &q.CreatedAt, &q.UpdatedAt,
			&lastCorrect, &lastAnswered, &streak)
		if err != nil {
			log.Printf("[DB ERROR] Failed to scan next question row: %v", err)
			return nil, err
		}

		// Parse keywords JSON
		if keywordsJSON.Valid && keywordsJSON.String != "" {
			json.Unmarshal([]byte(keywordsJSON.String), &q.Keywords)
		}

		// Parse choices JSON
		if choicesJSON.Valid && choicesJSON.String != "" {
			json.Unmarshal([]byte(choicesJSON.String), &q.Choices)
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

// Import types and functions
type ImportRequest struct {
	Questions []QuestionImport `json:"questions"`
}

type QuestionImport struct {
	Category     string   `json:"category"`
	Question     string   `json:"question"`
	QuestionType string   `json:"question_type"`
	Choices      []string `json:"choices,omitempty"`
	Answer       string   `json:"answer"`
	Keywords     []string `json:"keywords"`
	Difficulty   string   `json:"difficulty"`
}

type ImportResult struct {
	TotalQuestions    int      `json:"total_questions"`
	ImportedQuestions int      `json:"imported_questions"`
	SkippedQuestions  int      `json:"skipped_questions"`
	Errors            []string `json:"errors"`
	TimeTaken         string   `json:"time_taken"`
}

// Import questions from JSON
func (db *DB) ImportQuestions(importReq ImportRequest) (*ImportResult, error) {
	log.Printf("[IMPORT] Starting import of %d questions", len(importReq.Questions))
	start := time.Now()

	result := &ImportResult{
		TotalQuestions: len(importReq.Questions),
		Errors:         make([]string, 0),
	}

	// Start transaction for better performance and consistency
	tx, err := db.Begin()
	if err != nil {
		log.Printf("[IMPORT ERROR] Failed to start transaction: %v", err)
		return nil, err
	}
	defer tx.Rollback() // Will be ignored if we commit successfully

	log.Println("[IMPORT] Transaction started")

	// Prepare statement for better performance
	stmt, err := tx.Prepare(`
		INSERT INTO questions (category, question, question_type, choices, answer, keywords, difficulty)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		log.Printf("[IMPORT ERROR] Failed to prepare statement: %v", err)
		return nil, err
	}
	defer stmt.Close()

	// Track duplicates
	existingQuestions := make(map[string]bool)
	rows, err := db.Query("SELECT question FROM questions")
	if err != nil {
		log.Printf("[IMPORT ERROR] Failed to fetch existing questions: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var existingQuestion string
		if err := rows.Scan(&existingQuestion); err != nil {
			log.Printf("[IMPORT ERROR] Failed to scan existing question: %v", err)
			continue
		}
		existingQuestions[strings.ToLower(strings.TrimSpace(existingQuestion))] = true
	}

	log.Printf("[IMPORT] Found %d existing questions to check for duplicates", len(existingQuestions))

	// Process each question
	for i, q := range importReq.Questions {
		log.Printf("[IMPORT] Processing question %d/%d: category='%s'", i+1, len(importReq.Questions), q.Category)

		// Validate required fields
		if strings.TrimSpace(q.Question) == "" {
			errMsg := fmt.Sprintf("Question %d: empty question text", i+1)
			log.Printf("[IMPORT SKIP] %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		if strings.TrimSpace(q.Answer) == "" {
			errMsg := fmt.Sprintf("Question %d: empty answer", i+1)
			log.Printf("[IMPORT SKIP] %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		if strings.TrimSpace(q.Category) == "" {
			errMsg := fmt.Sprintf("Question %d: empty category", i+1)
			log.Printf("[IMPORT SKIP] %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		// Validate question type and choices
		questionType := strings.ToLower(strings.TrimSpace(q.QuestionType))
		if questionType == "" {
			questionType = "open_text" // Default
			log.Printf("[IMPORT] Question %d: using default question type 'open_text'", i+1)
		}

		validTypes := []string{"open_text", "multiple_choice", "true_false", "multiple_select"}
		isValidType := false
		for _, vt := range validTypes {
			if questionType == vt {
				isValidType = true
				break
			}
		}

		if !isValidType {
			errMsg := fmt.Sprintf("Question %d: invalid question type '%s', must be one of: %v", i+1, q.QuestionType, validTypes)
			log.Printf("[IMPORT SKIP] %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		// Validate choices for multiple choice questions
		if (questionType == "multiple_choice" || questionType == "multiple_select") && len(q.Choices) < 2 {
			errMsg := fmt.Sprintf("Question %d: %s questions must have at least 2 choices", i+1, questionType)
			log.Printf("[IMPORT SKIP] %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		// Validate answer is in choices for multiple choice
		if questionType == "multiple_choice" {
			answerInChoices := false
			for _, choice := range q.Choices {
				if normalizeAnswer(choice) == normalizeAnswer(q.Answer) {
					answerInChoices = true
					break
				}
			}
			if !answerInChoices {
				errMsg := fmt.Sprintf("Question %d: answer '%s' not found in choices", i+1, q.Answer)
				log.Printf("[IMPORT SKIP] %s", errMsg)
				result.Errors = append(result.Errors, errMsg)
				result.SkippedQuestions++
				continue
			}
		}

		// For multiple_select, convert answer to JSON array if it's a comma-separated string
		answer := strings.TrimSpace(q.Answer)
		if questionType == "multiple_select" {
			// Check if answer is already JSON
			var testArray []string
			if err := json.Unmarshal([]byte(answer), &testArray); err != nil {
				// Not JSON, treat as comma-separated and convert
				answers := strings.Split(answer, ",")
				for i, a := range answers {
					answers[i] = strings.TrimSpace(a)
				}
				if answerJSON, err := json.Marshal(answers); err == nil {
					answer = string(answerJSON)
					log.Printf("[IMPORT] Question %d: converted comma-separated answer to JSON", i+1)
				}
			}
		}

		// Validate difficulty
		difficulty := strings.ToLower(strings.TrimSpace(q.Difficulty))
		if difficulty == "" {
			difficulty = "medium" // Default
			log.Printf("[IMPORT] Question %d: using default difficulty 'medium'", i+1)
		} else if difficulty != "easy" && difficulty != "medium" && difficulty != "hard" {
			errMsg := fmt.Sprintf("Question %d: invalid difficulty '%s', must be easy/medium/hard", i+1, q.Difficulty)
			log.Printf("[IMPORT SKIP] %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		// Check for duplicates
		questionKey := strings.ToLower(strings.TrimSpace(q.Question))
		if existingQuestions[questionKey] {
			errMsg := fmt.Sprintf("Question %d: duplicate question already exists", i+1)
			log.Printf("[IMPORT SKIP] %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		// Marshal keywords and choices
		keywordsJSON, err := json.Marshal(q.Keywords)
		if err != nil {
			errMsg := fmt.Sprintf("Question %d: failed to marshal keywords: %v", i+1, err)
			log.Printf("[IMPORT SKIP] %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		var choicesJSON []byte
		if len(q.Choices) > 0 {
			choicesJSON, err = json.Marshal(q.Choices)
			if err != nil {
				errMsg := fmt.Sprintf("Question %d: failed to marshal choices: %v", i+1, err)
				log.Printf("[IMPORT SKIP] %s", errMsg)
				result.Errors = append(result.Errors, errMsg)
				result.SkippedQuestions++
				continue
			}
		}

		// Insert question
		_, err = stmt.Exec(
			strings.TrimSpace(q.Category),
			strings.TrimSpace(q.Question),
			questionType,
			string(choicesJSON),
			answer,
			string(keywordsJSON),
			difficulty,
		)

		if err != nil {
			errMsg := fmt.Sprintf("Question %d: database insert failed: %v", i+1, err)
			log.Printf("[IMPORT ERROR] %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		// Mark as existing to prevent duplicates within this import
		existingQuestions[questionKey] = true
		result.ImportedQuestions++

		if (i+1)%10 == 0 || i+1 == len(importReq.Questions) {
			log.Printf("[IMPORT] Progress: %d/%d questions processed", i+1, len(importReq.Questions))
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		log.Printf("[IMPORT ERROR] Failed to commit transaction: %v", err)
		return nil, err
	}

	duration := time.Since(start)
	result.TimeTaken = duration.String()

	log.Printf("[IMPORT] Import completed: %d imported, %d skipped, %d errors in %v",
		result.ImportedQuestions, result.SkippedQuestions, len(result.Errors), duration)

	return result, nil
}

// Helper function to normalize answers for comparison
func normalizeAnswer(answer string) string {
	// Simple normalization - trim spaces and convert to lowercase
	// You can make this more sophisticated later (remove accents, handle synonyms, etc.)
	normalized := strings.ToLower(strings.TrimSpace(answer))
	log.Printf("[DB] Normalized answer: '%s' -> '%s'", answer, normalized)
	return normalized
}
