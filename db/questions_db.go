package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/adamspd/QuizzApi/models"
	"github.com/adamspd/QuizzApi/utils"
)

// shuffleChoices randomizes the order of choices for multiple choice questions
func shuffleChoices(choices []string) []string {
	if len(choices) <= 1 {
		return choices
	}

	shuffled := make([]string, len(choices))
	copy(shuffled, choices)

	// Fisher-Yates shuffle
	for i := len(shuffled) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	return shuffled
}

func (db *DB) GetAllQuestionsForUser(userID int, userRole string) ([]models.Question, error) {
	utils.LogDB("Getting questions for user %d (role: %s)", userID, userRole)
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
		utils.LogError("GetAllQuestionsForUser query failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	var questions []models.Question
	for rows.Next() {
		var q models.Question
		var keywordsJSON, choicesJSON sql.NullString

		err := rows.Scan(&q.ID, &q.Category, &q.Question, &q.QuestionType, &choicesJSON, &q.Answer, &keywordsJSON,
			&q.Difficulty, &q.CreatedBy, &q.Status, &q.ApprovedBy, &q.ApprovedAt, &q.CreatedAt, &q.UpdatedAt,
			&q.CreatorUsername)
		if err != nil {
			utils.LogError("Failed to scan question row: %v", err)
			return nil, err
		}

		if keywordsJSON.Valid && keywordsJSON.String != "" {
			json.Unmarshal([]byte(keywordsJSON.String), &q.Keywords)
		}

		if choicesJSON.Valid && choicesJSON.String != "" {
			json.Unmarshal([]byte(choicesJSON.String), &q.Choices)
		}

		// Shuffle choices for multiple choice questions to prevent memorizing positions
		if q.QuestionType == "multiple_choice" || q.QuestionType == "multiple_select" {
			q.Choices = shuffleChoices(q.Choices)
		}

		questions = append(questions, q)
	}

	duration := time.Since(start)
	utils.LogDB("GetAllQuestionsForUser completed: %d questions in %v", len(questions), duration)
	return questions, nil
}

func (db *DB) GetQuestionByID(id int) (*models.Question, error) {
	utils.LogDB("Executing query: GetQuestionByID(%d)", id)
	start := time.Now()

	var q models.Question
	var keywordsJSON, choicesJSON sql.NullString

	err := db.QueryRow(`
        SELECT id, category, question, question_type, choices, answer, keywords, difficulty, created_at, updated_at 
        FROM questions WHERE id = ?
    `, id).Scan(&q.ID, &q.Category, &q.Question, &q.QuestionType, &choicesJSON, &q.Answer, &keywordsJSON,
		&q.Difficulty, &q.CreatedAt, &q.UpdatedAt)

	if err != nil {
		duration := time.Since(start)
		if err == sql.ErrNoRows {
			utils.LogDB("Question ID %d not found (%v)", id, duration)
		} else {
			utils.LogError("GetQuestionByID(%d) failed: %v (%v)", id, err, duration)
		}
		return nil, err
	}

	if keywordsJSON.Valid && keywordsJSON.String != "" {
		json.Unmarshal([]byte(keywordsJSON.String), &q.Keywords)
	}

	if choicesJSON.Valid && choicesJSON.String != "" {
		json.Unmarshal([]byte(choicesJSON.String), &q.Choices)
	}

	// Shuffle choices for multiple choice questions
	if q.QuestionType == "multiple_choice" || q.QuestionType == "multiple_select" {
		q.Choices = shuffleChoices(q.Choices)
	}

	duration := time.Since(start)
	utils.LogDB("GetQuestionByID(%d) completed in %v", id, duration)
	return &q, nil
}

func (db *DB) CreateQuestionWithAuth(req models.QuestionRequest, createdBy int, userRole string) (*models.Question, error) {
	utils.LogDB("Creating question by user %d (role: %s)", createdBy, userRole)
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
		utils.LogError("CreateQuestionWithAuth failed: %v (%v)", err, duration)
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		utils.LogError("Failed to get LastInsertId: %v", err)
		return nil, err
	}

	duration := time.Since(start)
	utils.LogDB("Question created with ID %d, status '%s' by user %d in %v", id, status, createdBy, duration)

	return db.GetQuestionByID(int(id))
}

func (db *DB) UpdateQuestionWithAuth(id int, req models.QuestionRequest, userID int, userRole string) (*models.Question, error) {
	utils.LogDB("Updating question ID %d by user %d (role: %s)", id, userID, userRole)
	start := time.Now()

	current, err := db.GetQuestionByID(id)
	if err != nil {
		return nil, err
	}

	// Check permissions using session logic
	session := &models.Session{UserID: userID, Role: userRole}
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
		utils.LogError("UpdateQuestionWithAuth(%d) failed: %v (%v)", id, err, duration)
		return nil, err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		utils.LogDB("UpdateQuestionWithAuth(%d): no rows affected", id)
	}

	// Clear progress if answer changed
	if current.Answer != req.Answer {
		utils.LogDB("Answer changed for question %d, clearing progress", id)
		deleteResult, err := db.Exec("DELETE FROM progress WHERE question_id = ?", id)
		if err != nil {
			utils.LogError("Failed to clear progress for question %d: %v", id, err)
			return nil, err
		}
		progressDeleted, _ := deleteResult.RowsAffected()
		utils.LogDB("Cleared %d progress entries for question %d", progressDeleted, id)
	}

	duration := time.Since(start)
	utils.LogDB("UpdateQuestionWithAuth(%d) completed in %v", id, duration)

	return db.GetQuestionByID(id)
}

func (db *DB) DeleteQuestion(id int) error {
	utils.LogDB("Deleting question ID %d", id)
	start := time.Now()

	progressResult, err := db.Exec("DELETE FROM progress WHERE question_id = ?", id)
	if err != nil {
		utils.LogError("Failed to delete progress for question %d: %v", id, err)
		return err
	}

	progressDeleted, _ := progressResult.RowsAffected()
	if progressDeleted > 0 {
		utils.LogDB("Deleted %d progress entries for question %d", progressDeleted, id)
	}

	questionResult, err := db.Exec("DELETE FROM questions WHERE id = ?", id)
	if err != nil {
		duration := time.Since(start)
		utils.LogError("Failed to delete question %d: %v (%v)", id, err, duration)
		return err
	}

	rowsAffected, _ := questionResult.RowsAffected()
	duration := time.Since(start)

	if rowsAffected == 0 {
		utils.LogDB("DeleteQuestion(%d): no rows affected (%v)", id, duration)
	} else {
		utils.LogDB("DeleteQuestion(%d) completed in %v", id, duration)
	}

	return nil
}

func (db *DB) GetNextQuestionsForUser(userID int, count int) ([]models.Question, error) {
	utils.LogDB("Getting next %d questions for user %d", count, userID)
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
		utils.LogError("GetNextQuestionsForUser failed: %v (%v)", err, duration)
		return nil, err
	}
	defer rows.Close()

	var questions []models.Question
	neverAnswered := 0
	incorrectAnswers := 0

	for rows.Next() {
		var q models.Question
		var keywordsJSON, choicesJSON sql.NullString
		var lastCorrect bool
		var lastAnswered string
		var streak int

		err := rows.Scan(&q.ID, &q.Category, &q.Question, &q.QuestionType, &choicesJSON, &q.Answer, &keywordsJSON,
			&q.Difficulty, &q.CreatedBy, &q.Status, &q.ApprovedBy, &q.ApprovedAt, &q.CreatedAt, &q.UpdatedAt,
			&lastCorrect, &lastAnswered, &streak)
		if err != nil {
			utils.LogError("Failed to scan next question row: %v", err)
			return nil, err
		}

		if keywordsJSON.Valid && keywordsJSON.String != "" {
			json.Unmarshal([]byte(keywordsJSON.String), &q.Keywords)
		}

		if choicesJSON.Valid && choicesJSON.String != "" {
			json.Unmarshal([]byte(choicesJSON.String), &q.Choices)
		}

		// Shuffle choices for multiple choice questions - this is the key fix!
		if q.QuestionType == "multiple_choice" || q.QuestionType == "multiple_select" {
			q.Choices = shuffleChoices(q.Choices)
		}

		if lastAnswered == "1970-01-01" {
			neverAnswered++
		} else if !lastCorrect {
			incorrectAnswers++
		}

		questions = append(questions, q)
	}

	duration := time.Since(start)
	utils.LogDB("GetNextQuestionsForUser completed: %d questions (%d never answered, %d incorrect) in %v",
		len(questions), neverAnswered, incorrectAnswers, duration)

	return questions, nil
}

func (db *DB) ImportQuestions(importReq models.ImportRequest) (*models.ImportResult, error) {
	utils.LogImport("Starting import of %d questions", len(importReq.Questions))
	start := time.Now()

	result := &models.ImportResult{
		TotalQuestions: len(importReq.Questions),
		Errors:         make([]string, 0),
	}

	tx, err := db.Begin()
	if err != nil {
		utils.LogError("Failed to start transaction: %v", err)
		return nil, err
	}
	defer tx.Rollback()

	utils.LogImport("Transaction started")

	stmt, err := tx.Prepare(`
		INSERT INTO questions (category, question, question_type, choices, answer, keywords, difficulty)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		utils.LogError("Failed to prepare statement: %v", err)
		return nil, err
	}
	defer stmt.Close()

	existingQuestions := make(map[string]bool)
	rows, err := db.Query("SELECT question FROM questions")
	if err != nil {
		utils.LogError("Failed to fetch existing questions: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var existingQuestion string
		if err := rows.Scan(&existingQuestion); err != nil {
			utils.LogError("Failed to scan existing question: %v", err)
			continue
		}
		existingQuestions[strings.ToLower(strings.TrimSpace(existingQuestion))] = true
	}

	utils.LogImport("Found %d existing questions to check for duplicates", len(existingQuestions))

	for i, q := range importReq.Questions {
		utils.LogImport("Processing question %d/%d: category='%s'", i+1, len(importReq.Questions), q.Category)

		if strings.TrimSpace(q.Question) == "" {
			errMsg := fmt.Sprintf("Question %d: empty question text", i+1)
			utils.LogImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		if strings.TrimSpace(q.Answer) == "" {
			errMsg := fmt.Sprintf("Question %d: empty answer", i+1)
			utils.LogImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		if strings.TrimSpace(q.Category) == "" {
			errMsg := fmt.Sprintf("Question %d: empty category", i+1)
			utils.LogImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		questionType := strings.ToLower(strings.TrimSpace(q.QuestionType))
		if questionType == "" {
			questionType = "open_text"
			utils.LogImport("Question %d: using default question type 'open_text'", i+1)
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
			utils.LogImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		if (questionType == "multiple_choice" || questionType == "multiple_select") && len(q.Choices) < 2 {
			errMsg := fmt.Sprintf("Question %d: %s questions must have at least 2 choices", i+1, questionType)
			utils.LogImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		if questionType == "multiple_choice" {
			answerInChoices := false
			for _, choice := range q.Choices {
				if utils.NormalizeAnswer(choice) == utils.NormalizeAnswer(q.Answer) {
					answerInChoices = true
					break
				}
			}
			if !answerInChoices {
				errMsg := fmt.Sprintf("Question %d: answer '%s' not found in choices", i+1, q.Answer)
				utils.LogImport("SKIP: %s", errMsg)
				result.Errors = append(result.Errors, errMsg)
				result.SkippedQuestions++
				continue
			}
		}

		answer := strings.TrimSpace(q.Answer)
		if questionType == "multiple_select" {
			// If it's already a proper JSON array, keep it
			if strings.HasPrefix(strings.TrimSpace(q.Answer), "[") {
				answer = strings.TrimSpace(q.Answer)
			} else {
				// Handle comma-separated fallback
				answers := strings.Split(q.Answer, ",")
				for i, a := range answers {
					answers[i] = strings.TrimSpace(a)
				}
				answerJSON, _ := json.Marshal(answers)
				answer = string(answerJSON)
			}
		} else {
			answer = strings.TrimSpace(q.Answer)
		}

		difficulty := strings.ToLower(strings.TrimSpace(q.Difficulty))
		if difficulty == "" {
			difficulty = "medium"
			utils.LogImport("Question %d: using default difficulty 'medium'", i+1)
		} else if difficulty != "easy" && difficulty != "medium" && difficulty != "hard" {
			errMsg := fmt.Sprintf("Question %d: invalid difficulty '%s', must be easy/medium/hard", i+1, q.Difficulty)
			utils.LogImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		questionKey := strings.ToLower(strings.TrimSpace(q.Question))
		if existingQuestions[questionKey] {
			errMsg := fmt.Sprintf("Question %d: duplicate question already exists", i+1)
			utils.LogImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		keywordsJSON, err := json.Marshal(q.Keywords)
		if err != nil {
			errMsg := fmt.Sprintf("Question %d: failed to marshal keywords: %v", i+1, err)
			utils.LogImport("SKIP: %s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		var choicesJSON []byte
		if len(q.Choices) > 0 {
			choicesJSON, err = json.Marshal(q.Choices)
			if err != nil {
				errMsg := fmt.Sprintf("Question %d: failed to marshal choices: %v", i+1, err)
				utils.LogImport("SKIP: %s", errMsg)
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
			utils.LogError("%s", errMsg)
			result.Errors = append(result.Errors, errMsg)
			result.SkippedQuestions++
			continue
		}

		existingQuestions[questionKey] = true
		result.ImportedQuestions++

		if (i+1)%10 == 0 || i+1 == len(importReq.Questions) {
			utils.LogImport("Progress: %d/%d questions processed", i+1, len(importReq.Questions))
		}
	}

	if err := tx.Commit(); err != nil {
		utils.LogError("Failed to commit transaction: %v", err)
		return nil, err
	}

	duration := time.Since(start)
	result.TimeTaken = duration.String()

	utils.LogImport("Import completed: %d imported, %d skipped, %d errors in %v",
		result.ImportedQuestions, result.SkippedQuestions, len(result.Errors), duration)

	return result, nil
}
