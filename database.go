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

	if err := createTables(db); err != nil {
		logError("Failed to create tables: %v", err)
		return nil, err
	}

	logStartup("Database tables initialized successfully")
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
		logDB("Creating table %d/3", i+1)
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	logDB("Checking for schema updates...")

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

	if !hasQuestionType {
		logDB("Adding question_type column...")
		if _, err := db.Exec("ALTER TABLE questions ADD COLUMN question_type TEXT DEFAULT 'open_text'"); err != nil {
			return fmt.Errorf("failed to add question_type column: %w", err)
		}
	}

	if !hasChoices {
		logDB("Adding choices column...")
		if _, err := db.Exec("ALTER TABLE questions ADD COLUMN choices TEXT"); err != nil {
			return fmt.Errorf("failed to add choices column: %w", err)
		}
	}

	return nil
}

func (db *DB) GetAllQuestions() ([]Question, error) {
	logDB("Executing query: GetAllQuestions")
	start := time.Now()

	rows, err := db.Query(`
        SELECT id, category, question, question_type, choices, answer, keywords, difficulty, created_at, updated_at 
        FROM questions ORDER BY created_at DESC
    `)
	if err != nil {
		logError("GetAllQuestions query failed: %v", err)
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
	logDB("GetAllQuestions completed: %d questions in %v", len(questions), duration)
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

func (db *DB) CreateQuestion(req QuestionRequest) (*Question, error) {
	logDB("Creating question in category '%s', type '%s'", req.Category, req.QuestionType)
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
		logError("CreateQuestion failed: %v (%v)", err, duration)
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		logError("Failed to get LastInsertId: %v", err)
		return nil, err
	}

	duration := time.Since(start)
	logDB("Question created with ID %d, type '%s' in %v", id, questionType, duration)

	return db.GetQuestionByID(int(id))
}

func (db *DB) UpdateQuestion(id int, req QuestionRequest) (*Question, error) {
	logDB("Updating question ID %d", id)
	start := time.Now()

	current, err := db.GetQuestionByID(id)
	if err != nil {
		return nil, err
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

	keywordsJSON, _ := json.Marshal(req.Keywords)

	var choicesJSON []byte
	if len(req.Choices) > 0 {
		choicesJSON, _ = json.Marshal(req.Choices)
	}

	result, err := db.Exec(`
        UPDATE questions 
        SET category = ?, question = ?, question_type = ?, choices = ?, answer = ?, keywords = ?, difficulty = ?, updated_at = CURRENT_TIMESTAMP
        WHERE id = ?
    `, req.Category, req.Question, questionType, string(choicesJSON), req.Answer, string(keywordsJSON), req.Difficulty, id)

	if err != nil {
		duration := time.Since(start)
		logError("UpdateQuestion(%d) failed: %v (%v)", id, err, duration)
		return nil, err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		logDB("UpdateQuestion(%d): no rows affected", id)
	}

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
	logDB("UpdateQuestion(%d) completed in %v", id, duration)

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

func (db *DB) GetNextQuestions(userID int, count int) ([]Question, error) {
	logDB("Getting next %d questions for user %d", count, userID)
	start := time.Now()

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
            CASE WHEN last_progress.answered_at IS NULL THEN 0 ELSE 1 END,
            CASE WHEN last_progress.is_correct = 0 THEN 0 ELSE 1 END,
            last_progress.answered_at ASC
        LIMIT ?
    `

	rows, err := db.Query(query, userID, userID, count)
	if err != nil {
		duration := time.Since(start)
		logError("GetNextQuestions failed: %v (%v)", err, duration)
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
	logDB("GetNextQuestions completed: %d questions (%d never answered, %d incorrect) in %v",
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
