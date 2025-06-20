package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
}

func InitDB(dbPath string) (*DB, error) {
	logStartup("Initializing database at: %s", dbPath)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		logError("Failed to open database: %v", err)
		return nil, err
	}

	if err := db.Ping(); err != nil {
		logError("Failed to ping database: %v", err)
		return nil, err
	}

	logStartup("Database connection established")

	if err := createTablesWithAuth(db); err != nil {
		logError("Failed to create tables: %v", err)
		return nil, err
	}

	logStartup("Database tables initialized successfully")
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

		// Progress table (already exists, just ensure foreign key)
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
		logDB("Creating table %d/%d", i+1, len(queries))
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_questions_status ON questions(status)",
		"CREATE INDEX IF NOT EXISTS idx_questions_created_by ON questions(created_by)",
		"CREATE INDEX IF NOT EXISTS idx_progress_user_id ON progress(user_id)",
		"CREATE INDEX IF NOT EXISTS idx_email_verifications_token ON email_verifications(token)",
		"CREATE INDEX IF NOT EXISTS idx_email_verifications_user_id ON email_verifications(user_id)",
	}

	for _, index := range indexes {
		if _, err := db.Exec(index); err != nil {
			logDB("Failed to create index (non-fatal): %v", err)
		}
	}

	return nil
}

func (db *DB) GetAllQuestionsForUser(userID int, userRole string) ([]Question, error) {
	logDB("Getting questions for user %d (role: %s)", userID, userRole)
	start := time.Now()

	var query string
	var args []interface{}

	if userRole == "admin" || userRole == "moderator" {
		// Admins and moderators see all questions
		query = `
			SELECT q.id, q.category, q.question, q.question_type, q.choices, q.answer, q.keywords, q.difficulty,
				   q.created_by, q.status, q.approved_by, q.approved_at, q.created_at, q.updated_at,
				   u.username as creator_username
			FROM questions q
			LEFT JOIN users u ON q.created_by = u.id
			ORDER BY q.created_at DESC
		`
	} else {
		// Regular users see approved questions + their own pending questions
		query = `
			SELECT q.id, q.category, q.question, q.question_type, q.choices, q.answer, q.keywords, q.difficulty,
				   q.created_by, q.status, q.approved_by, q.approved_at, q.created_at, q.updated_at,
				   u.username as creator_username
			FROM questions q
			LEFT JOIN users u ON q.created_by = u.id
			WHERE q.status = 'approved' OR (q.created_by = ? AND q.status = 'pending')
			ORDER BY q.created_at DESC
		`
		args = append(args, userID)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		logError("GetAllQuestionsForUser query failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	var questions []Question
	for rows.Next() {
		var q Question
		var keywordsJSON, choicesJSON sql.NullString

		err := rows.Scan(&q.ID, &q.Category, &q.Question, &q.QuestionType, &choicesJSON, &q.Answer, &keywordsJSON,
			&q.Difficulty, &q.CreatedBy, &q.Status, &q.ApprovedBy, &q.ApprovedAt, &q.CreatedAt, &q.UpdatedAt,
			&q.CreatorUsername)
		if err != nil {
			logError("Failed to scan question row: %v", err)
			return nil, err
		}

		if keywordsJSON.Valid && keywordsJSON.String != "" {
			json.Unmarshal([]byte(keywordsJSON.String), &q.Keywords)
		}

		if choicesJSON.Valid && choicesJSON.String != "" {
			json.Unmarshal([]byte(choicesJSON.String), &q.Choices)
		}

		questions = append(questions, q)
	}

	duration := time.Since(start)
	logDB("GetAllQuestionsForUser completed: %d questions in %v", len(questions), duration)
	return questions, nil
}

func (db *DB) GetQuestionByID(id int) (*Question, error) {
	logDB("Executing query: GetQuestionByID(%d)", id)
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
			logDB("Question ID %d not found (%v)", id, duration)
		} else {
			logError("GetQuestionByID(%d) failed: %v (%v)", id, err, duration)
		}
		return nil, err
	}

	if keywordsJSON.Valid && keywordsJSON.String != "" {
		json.Unmarshal([]byte(keywordsJSON.String), &q.Keywords)
	}

	if choicesJSON.Valid && choicesJSON.String != "" {
		json.Unmarshal([]byte(choicesJSON.String), &q.Choices)
	}

	duration := time.Since(start)
	logDB("GetQuestionByID(%d) completed in %v", id, duration)
	return &q, nil
}

func (db *DB) CreateQuestionWithAuth(req QuestionRequest, createdBy int, userRole string) (*Question, error) {
	logDB("Creating question by user %d (role: %s)", createdBy, userRole)
	start := time.Now()

	validTypes := []string{"open_text", "multiple_choice", "true_false", "multiple_select"}
	questionType := strings.ToLower(strings.TrimSpace(req.QuestionType))
	if questionType == "" {
		questionType = "open_text"
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

	// Set status based on user role
	status := req.Status
	if status == "" {
		if userRole == "admin" {
			status = "approved"
		} else {
			status = "pending"
		}
	}

	keywordsJSON, _ := json.Marshal(req.Keywords)

	var choicesJSON []byte
	if len(req.Choices) > 0 {
		choicesJSON, _ = json.Marshal(req.Choices)
	}

	result, err := db.Exec(`
		INSERT INTO questions (category, question, question_type, choices, answer, keywords, difficulty, created_by, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, req.Category, req.Question, questionType, string(choicesJSON), req.Answer, string(keywordsJSON), req.Difficulty, createdBy, status)

	if err != nil {
		duration := time.Since(start)
		logError("CreateQuestionWithAuth failed: %v (%v)", err, duration)
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		logError("Failed to get LastInsertId: %v", err)
		return nil, err
	}

	duration := time.Since(start)
	logDB("Question created with ID %d, status '%s' by user %d in %v", id, status, createdBy, duration)

	return db.GetQuestionByID(int(id))
}

func (db *DB) UpdateQuestionWithAuth(id int, req QuestionRequest, userID int, userRole string) (*Question, error) {
	logDB("Updating question ID %d by user %d (role: %s)", id, userID, userRole)
	start := time.Now()

	current, err := db.GetQuestionByID(id)
	if err != nil {
		return nil, err
	}

	// Check permissions
	session := &Session{UserID: userID, Role: userRole}
	if !session.CanEditQuestion(current) {
		return nil, fmt.Errorf("insufficient permissions to edit this question")
	}

	validTypes := []string{"open_text", "multiple_choice", "true_false", "multiple_select"}
	questionType := strings.ToLower(strings.TrimSpace(req.QuestionType))
	if questionType == "" {
		questionType = current.QuestionType
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

	// Handle status changes
	status := current.Status
	if req.Status != "" && (userRole == "admin" || userRole == "moderator") {
		status = req.Status
	}

	keywordsJSON, _ := json.Marshal(req.Keywords)

	var choicesJSON []byte
	if len(req.Choices) > 0 {
		choicesJSON, _ = json.Marshal(req.Choices)
	}

	var approvedBy *int
	var approvedAt interface{}

	if status == "approved" && current.Status != "approved" {
		approvedBy = &userID
		approvedAt = "CURRENT_TIMESTAMP"
	} else if current.ApprovedBy != nil {
		approvedBy = current.ApprovedBy
		approvedAt = current.ApprovedAt
	}

	result, err := db.Exec(`
		UPDATE questions 
		SET category = ?, question = ?, question_type = ?, choices = ?, answer = ?, keywords = ?, 
		    difficulty = ?, status = ?, approved_by = ?, approved_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, req.Category, req.Question, questionType, string(choicesJSON), req.Answer, string(keywordsJSON),
		req.Difficulty, status, approvedBy, approvedAt, id)

	if err != nil {
		duration := time.Since(start)
		logError("UpdateQuestionWithAuth(%d) failed: %v (%v)", id, err, duration)
		return nil, err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		logDB("UpdateQuestionWithAuth(%d): no rows affected", id)
	}

	// Clear progress if answer changed
	if current.Answer != req.Answer {
		logDB("Answer changed for question %d, clearing progress", id)
		deleteResult, err := db.Exec("DELETE FROM progress WHERE question_id = ?", id)
		if err != nil {
			logError("Failed to clear progress for question %d: %v", id, err)
			return nil, err
		}
		progressDeleted, _ := deleteResult.RowsAffected()
		logDB("Cleared %d progress entries for question %d", progressDeleted, id)
	}

	duration := time.Since(start)
	logDB("UpdateQuestionWithAuth(%d) completed in %v", id, duration)

	return db.GetQuestionByID(id)
}

func (db *DB) DeleteQuestion(id int) error {
	logDB("Deleting question ID %d", id)
	start := time.Now()

	progressResult, err := db.Exec("DELETE FROM progress WHERE question_id = ?", id)
	if err != nil {
		logError("Failed to delete progress for question %d: %v", id, err)
		return err
	}

	progressDeleted, _ := progressResult.RowsAffected()
	if progressDeleted > 0 {
		logDB("Deleted %d progress entries for question %d", progressDeleted, id)
	}

	questionResult, err := db.Exec("DELETE FROM questions WHERE id = ?", id)
	if err != nil {
		duration := time.Since(start)
		logError("Failed to delete question %d: %v (%v)", id, err, duration)
		return err
	}

	rowsAffected, _ := questionResult.RowsAffected()
	duration := time.Since(start)

	if rowsAffected == 0 {
		logDB("DeleteQuestion(%d): no rows affected (%v)", id, duration)
	} else {
		logDB("DeleteQuestion(%d) completed in %v", id, duration)
	}

	return nil
}

func (db *DB) RecordProgress(userID int, req ProgressRequest) (*Progress, error) {
	logDB("Recording progress: user %d, question %d", userID, req.QuestionID)
	start := time.Now()

	question, err := db.GetQuestionByID(req.QuestionID)
	if err != nil {
		logError("Failed to get question %d for progress check: %v", req.QuestionID, err)
		return nil, err
	}

	isCorrect := checkAnswer(question, req.UserAnswer)

	logDB("Answer check for %s question: user='%s' vs correct='%s' -> %t",
		question.QuestionType, req.UserAnswer, question.Answer, isCorrect)

	result, err := db.Exec(`
        INSERT INTO progress (user_id, question_id, user_answer, is_correct, time_taken_seconds)
        VALUES (?, ?, ?, ?, ?)
    `, userID, req.QuestionID, req.UserAnswer, isCorrect, req.TimeTakenSeconds)

	if err != nil {
		duration := time.Since(start)
		logError("RecordProgress failed: %v (%v)", err, duration)
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		logError("Failed to get progress LastInsertId: %v", err)
		return nil, err
	}

	duration := time.Since(start)
	logDB("Progress recorded with ID %d (correct: %t) in %v", id, isCorrect, duration)

	return db.GetProgressByID(int(id))
}

func (db *DB) GetProgressByID(id int) (*Progress, error) {
	logDB("Executing query: GetProgressByID(%d)", id)

	var p Progress

	err := db.QueryRow(`
        SELECT id, user_id, question_id, user_answer, is_correct, answered_at, time_taken_seconds
        FROM progress WHERE id = ?
    `, id).Scan(&p.ID, &p.UserID, &p.QuestionID, &p.UserAnswer, &p.IsCorrect, &p.AnsweredAt, &p.TimeTakenSeconds)

	if err != nil {
		logError("GetProgressByID(%d) failed: %v", id, err)
		return nil, err
	}

	return &p, nil
}

func (db *DB) GetUserStats(userID int) (*Stats, error) {
	logDB("Calculating stats for user %d", userID)
	start := time.Now()

	stats := &Stats{
		Categories: make(map[string]CategoryStat),
	}

	err := db.QueryRow("SELECT COUNT(*) FROM questions").Scan(&stats.TotalQuestions)
	if err != nil {
		logError("Failed to count total questions: %v", err)
		return nil, err
	}

	err = db.QueryRow(`
        SELECT COUNT(*) as answered, SUM(CASE WHEN is_correct THEN 1 ELSE 0 END) as correct
        FROM progress WHERE user_id = ?
    `, userID).Scan(&stats.Answered, &stats.Correct)
	if err != nil {
		logError("Failed to get user progress stats: %v", err)
		return nil, err
	}

	if stats.Answered > 0 {
		stats.Accuracy = float64(stats.Correct) / float64(stats.Answered)
	}

	stats.Streak = db.getCurrentStreak(userID)

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
		logError("Failed to get category stats: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var category string
		var answered, correct int

		err := rows.Scan(&category, &answered, &correct)
		if err != nil {
			logError("Failed to scan category stats: %v", err)
			return nil, err
		}

		stats.Categories[category] = CategoryStat{
			Answered: answered,
			Correct:  correct,
		}
	}

	duration := time.Since(start)
	logDB("Stats calculated for user %d: %d/%d correct (%.1f%%), streak %d, %d categories (%v)",
		userID, stats.Correct, stats.Answered, stats.Accuracy*100, stats.Streak, len(stats.Categories), duration)

	return stats, nil
}

func (db *DB) getCurrentStreak(userID int) int {
	logDB("Calculating streak for user %d", userID)

	rows, err := db.Query(`
        SELECT is_correct FROM progress 
        WHERE user_id = ? 
        ORDER BY answered_at DESC
        LIMIT 50
    `, userID)
	if err != nil {
		logError("Failed to get streak data: %v", err)
		return 0
	}
	defer rows.Close()

	streak := 0
	for rows.Next() {
		var isCorrect bool
		if err := rows.Scan(&isCorrect); err != nil {
			logError("Failed to scan streak row: %v", err)
			break
		}

		if isCorrect {
			streak++
		} else {
			break
		}
	}

	logDB("Current streak for user %d: %d", userID, streak)
	return streak
}

func (db *DB) GetNextQuestionsForUser(userID int, count int) ([]Question, error) {
	logDB("Getting next %d questions for user %d", count, userID)
	start := time.Now()

	// Only show approved questions for practice
	query := `
		SELECT DISTINCT q.id, q.category, q.question, q.question_type, q.choices, q.answer, q.keywords, q.difficulty, 
			   q.created_by, q.status, q.approved_by, q.approved_at, q.created_at, q.updated_at,
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
		WHERE q.status = 'approved'
		ORDER BY 
			CASE WHEN last_progress.answered_at IS NULL THEN 0 ELSE 1 END,
			CASE WHEN last_progress.is_correct = 0 THEN 0 ELSE 1 END,
			last_progress.answered_at ASC
		LIMIT ?
	`

	rows, err := db.Query(query, userID, userID, count)
	if err != nil {
		duration := time.Since(start)
		logError("GetNextQuestionsForUser failed: %v (%v)", err, duration)
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
			&q.Difficulty, &q.CreatedBy, &q.Status, &q.ApprovedBy, &q.ApprovedAt, &q.CreatedAt, &q.UpdatedAt,
			&lastCorrect, &lastAnswered, &streak)
		if err != nil {
			logError("Failed to scan next question row: %v", err)
			return nil, err
		}

		if keywordsJSON.Valid && keywordsJSON.String != "" {
			json.Unmarshal([]byte(keywordsJSON.String), &q.Keywords)
		}

		if choicesJSON.Valid && choicesJSON.String != "" {
			json.Unmarshal([]byte(choicesJSON.String), &q.Choices)
		}

		if lastAnswered == "1970-01-01" {
			neverAnswered++
		} else if !lastCorrect {
			incorrectAnswers++
		}

		questions = append(questions, q)
	}

	duration := time.Since(start)
	logDB("GetNextQuestionsForUser completed: %d questions (%d never answered, %d incorrect) in %v",
		len(questions), neverAnswered, incorrectAnswers, duration)

	return questions, nil
}

func (db *DB) ImportQuestions(importReq ImportRequest) (*ImportResult, error) {
	logImport("Starting import of %d questions", len(importReq.Questions))
	start := time.Now()

	result := &ImportResult{
		TotalQuestions: len(importReq.Questions),
		Errors:         make([]string, 0),
	}

	tx, err := db.Begin()
	if err != nil {
		logError("Failed to start transaction: %v", err)
		return nil, err
	}
	defer tx.Rollback()

	logImport("Transaction started")

	stmt, err := tx.Prepare(`
		INSERT INTO questions (category, question, question_type, choices, answer, keywords, difficulty)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		logError("Failed to prepare statement: %v", err)
		return nil, err
	}
	defer stmt.Close()

	existingQuestions := make(map[string]bool)
	rows, err := db.Query("SELECT question FROM questions")
	if err != nil {
		logError("Failed to fetch existing questions: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var existingQuestion string
		if err := rows.Scan(&existingQuestion); err != nil {
			logError("Failed to scan existing question: %v", err)
			continue
		}
		existingQuestions[strings.ToLower(strings.TrimSpace(existingQuestion))] = true
	}

	logImport("Found %d existing questions to check for duplicates", len(existingQuestions))

	for i, q := range importReq.Questions {
		logImport("Processing question %d/%d: category='%s'", i+1, len(importReq.Questions), q.Category)

		if strings.TrimSpace(q.Question) == "" {
			errMsg := fmt.Sprintf("Question %d: empty question text", i+1)
			logImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		if strings.TrimSpace(q.Answer) == "" {
			errMsg := fmt.Sprintf("Question %d: empty answer", i+1)
			logImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		if strings.TrimSpace(q.Category) == "" {
			errMsg := fmt.Sprintf("Question %d: empty category", i+1)
			logImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		questionType := strings.ToLower(strings.TrimSpace(q.QuestionType))
		if questionType == "" {
			questionType = "open_text"
			logImport("Question %d: using default question type 'open_text'", i+1)
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
			logImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		if (questionType == "multiple_choice" || questionType == "multiple_select") && len(q.Choices) < 2 {
			errMsg := fmt.Sprintf("Question %d: %s questions must have at least 2 choices", i+1, questionType)
			logImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

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
				logImport("SKIP: %s", errMsg)
				result.Errors = append(result.Errors, errMsg)
				result.SkippedQuestions++
				continue
			}
		}

		answer := strings.TrimSpace(q.Answer)
		if questionType == "multiple_select" {
			var testArray []string
			if err := json.Unmarshal([]byte(answer), &testArray); err != nil {
				answers := strings.Split(answer, ",")
				for i, a := range answers {
					answers[i] = strings.TrimSpace(a)
				}
				if answerJSON, err := json.Marshal(answers); err == nil {
					answer = string(answerJSON)
					logImport("Question %d: converted comma-separated answer to JSON", i+1)
				}
			}
		}

		difficulty := strings.ToLower(strings.TrimSpace(q.Difficulty))
		if difficulty == "" {
			difficulty = "medium"
			logImport("Question %d: using default difficulty 'medium'", i+1)
		} else if difficulty != "easy" && difficulty != "medium" && difficulty != "hard" {
			errMsg := fmt.Sprintf("Question %d: invalid difficulty '%s', must be easy/medium/hard", i+1, q.Difficulty)
			logImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		questionKey := strings.ToLower(strings.TrimSpace(q.Question))
		if existingQuestions[questionKey] {
			errMsg := fmt.Sprintf("Question %d: duplicate question already exists", i+1)
			logImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		keywordsJSON, err := json.Marshal(q.Keywords)
		if err != nil {
			errMsg := fmt.Sprintf("Question %d: failed to marshal keywords: %v", i+1, err)
			logImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		var choicesJSON []byte
		if len(q.Choices) > 0 {
			choicesJSON, err = json.Marshal(q.Choices)
			if err != nil {
				errMsg := fmt.Sprintf("Question %d: failed to marshal choices: %v", i+1, err)
				logImport("SKIP: %s", errMsg)
				result.Errors = append(result.Errors, errMsg)
				result.SkippedQuestions++
				continue
			}
		}

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
			logError("%s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		existingQuestions[questionKey] = true
		result.ImportedQuestions++

		if (i+1)%10 == 0 || i+1 == len(importReq.Questions) {
			logImport("Progress: %d/%d questions processed", i+1, len(importReq.Questions))
		}
	}

	if err := tx.Commit(); err != nil {
		logError("Failed to commit transaction: %v", err)
		return nil, err
	}

	duration := time.Since(start)
	result.TimeTaken = duration.String()

	logImport("Import completed: %d imported, %d skipped, %d errors in %v",
		result.ImportedQuestions, result.SkippedQuestions, len(result.Errors), duration)

	return result, nil
}

func (db *DB) CreateUser(req UserRequest) (*User, error) {
	logDB("Creating user: %s (%s)", req.Username, req.Email)
	start := time.Now()

	// Validate the request
	if err := validateUserRequest(&req, false); err != nil {
		return nil, err
	}

	// Hash the password
	hashedPassword, err := hashPassword(req.Password)
	if err != nil {
		logError("Failed to hash password: %v", err)
		return nil, err
	}

	// Set default role if not specified
	role := req.Role
	if role == "" {
		role = "user"
	}

	// Set default active status
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	result, err := db.Exec(`
		INSERT INTO users (username, email, password_hash, role, is_active)
		VALUES (?, ?, ?, ?, ?)
	`, req.Username, req.Email, hashedPassword, role, isActive)

	if err != nil {
		duration := time.Since(start)
		logError("CreateUser failed: %v (%v)", err, duration)
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		logError("Failed to get LastInsertId for user: %v", err)
		return nil, err
	}

	duration := time.Since(start)
	logDB("User created with ID %d in %v", id, duration)

	return db.GetUserByID(int(id))
}

func (db *DB) GetUserByID(id int) (*User, error) {
	logDB("Getting user by ID: %d", id)

	var user User
	err := db.QueryRow(`
		SELECT id, username, email, role, is_active, email_verified, email_verified_at, created_at, updated_at
		FROM users WHERE id = ?
	`, id).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.IsActive,
		&user.EmailVerified, &user.EmailVerifiedAt, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			logDB("User ID %d not found", id)
		} else {
			logError("GetUserByID(%d) failed: %v", id, err)
		}
		return nil, err
	}

	return &user, nil
}

func (db *DB) GetUserByUsername(username string) (*User, error) {
	logDB("Getting user by username: %s", username)

	var user User
	err := db.QueryRow(`
		SELECT id, username, email, role, is_active, email_verified, email_verified_at, created_at, updated_at
		FROM users WHERE username = ?
	`, username).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.IsActive,
		&user.EmailVerified, &user.EmailVerifiedAt, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			logDB("User %s not found", username)
		} else {
			logError("GetUserByUsername(%s) failed: %v", username, err)
		}
		return nil, err
	}

	return &user, nil
}

func (db *DB) GetUserByEmail(email string) (*User, error) {
	logDB("Getting user by email: %s", email)

	var user User
	err := db.QueryRow(`
		SELECT id, username, email, role, is_active, email_verified, email_verified_at, created_at, updated_at
		FROM users WHERE email = ?
	`, email).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.IsActive,
		&user.EmailVerified, &user.EmailVerifiedAt, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			logDB("User with email %s not found", email)
		} else {
			logError("GetUserByEmail(%s) failed: %v", email, err)
		}
		return nil, err
	}

	return &user, nil
}

func (db *DB) AuthenticateUser(username, password string) (*User, error) {
	logDB("Authenticating user: %s", username)

	var user User
	var passwordHash string

	err := db.QueryRow(`
		SELECT id, username, email, role, is_active, email_verified, email_verified_at, 
		       created_at, updated_at, password_hash
		FROM users WHERE username = ? AND is_active = 1
	`, username).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.IsActive,
		&user.EmailVerified, &user.EmailVerifiedAt, &user.CreatedAt, &user.UpdatedAt, &passwordHash)

	if err != nil {
		if err == sql.ErrNoRows {
			logDB("Authentication failed: user %s not found or inactive", username)
		} else {
			logError("AuthenticateUser(%s) failed: %v", username, err)
		}
		return nil, fmt.Errorf("invalid credentials")
	}

	// Check password
	if !checkPassword(passwordHash, password) {
		logDB("Authentication failed: invalid password for user %s", username)
		return nil, fmt.Errorf("invalid credentials")
	}

	logDB("User %s authenticated successfully", username)
	return &user, nil
}

func (db *DB) UpdateUser(id int, req UserRequest) (*User, error) {
	logDB("Updating user ID %d", id)
	start := time.Now()

	// Validate the request
	if err := validateUserRequest(&req, true); err != nil {
		return nil, err
	}

	// Check if user exists
	currentUser, err := db.GetUserByID(id)
	if err != nil {
		return nil, err
	}

	// Build update query dynamically
	var setParts []string
	var args []interface{}

	if req.Username != "" && req.Username != currentUser.Username {
		setParts = append(setParts, "username = ?")
		args = append(args, req.Username)
	}

	if req.Email != "" && req.Email != currentUser.Email {
		setParts = append(setParts, "email = ?, email_verified = 0, email_verified_at = NULL")
		args = append(args, req.Email)
	}

	if req.Password != "" {
		hashedPassword, err := hashPassword(req.Password)
		if err != nil {
			return nil, err
		}
		setParts = append(setParts, "password_hash = ?")
		args = append(args, hashedPassword)
	}

	if req.Role != "" && req.Role != currentUser.Role {
		setParts = append(setParts, "role = ?")
		args = append(args, req.Role)
	}

	if req.IsActive != nil && *req.IsActive != currentUser.IsActive {
		setParts = append(setParts, "is_active = ?")
		args = append(args, *req.IsActive)
	}

	if len(setParts) == 0 {
		logDB("UpdateUser(%d): no changes to apply", id)
		return currentUser, nil
	}

	setParts = append(setParts, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)

	query := fmt.Sprintf("UPDATE users SET %s WHERE id = ?", strings.Join(setParts, ", "))

	result, err := db.Exec(query, args...)
	if err != nil {
		duration := time.Since(start)
		logError("UpdateUser(%d) failed: %v (%v)", id, err, duration)
		return nil, err
	}

	rowsAffected, _ := result.RowsAffected()
	duration := time.Since(start)

	if rowsAffected == 0 {
		logDB("UpdateUser(%d): no rows affected (%v)", id, duration)
	} else {
		logDB("UpdateUser(%d) completed in %v", id, duration)
	}

	return db.GetUserByID(id)
}

func (db *DB) DeleteUser(id int) error {
	logDB("Deleting user ID %d", id)
	start := time.Now()

	result, err := db.Exec("UPDATE users SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	if err != nil {
		duration := time.Since(start)
		logError("Failed to delete user %d: %v (%v)", id, err, duration)
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	duration := time.Since(start)

	if rowsAffected == 0 {
		logDB("DeleteUser(%d): no rows affected (%v)", id, duration)
		return fmt.Errorf("user not found")
	} else {
		logDB("DeleteUser(%d) completed in %v", id, duration)
	}

	return nil
}

func (db *DB) GetAllUsers() ([]User, error) {
	logDB("Getting all users")
	start := time.Now()

	rows, err := db.Query(`
		SELECT id, username, email, role, is_active, email_verified, email_verified_at, created_at, updated_at
		FROM users ORDER BY created_at DESC
	`)
	if err != nil {
		logError("GetAllUsers query failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		err := rows.Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.IsActive,
			&user.EmailVerified, &user.EmailVerifiedAt, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			logError("Failed to scan user row: %v", err)
			return nil, err
		}
		users = append(users, user)
	}

	duration := time.Since(start)
	logDB("GetAllUsers completed: %d users in %v", len(users), duration)
	return users, nil
}
