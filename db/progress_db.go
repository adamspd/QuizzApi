package db

import (
	"time"

	"github.com/adamspd/QuizzApi/models"
	"github.com/adamspd/QuizzApi/utils"
)

func (db *DB) RecordProgress(userID int, req models.ProgressRequest) (*models.Progress, error) {
	utils.LogDB("Recording progress: user %d, question %d", userID, req.QuestionID)
	start := time.Now()

	question, err := db.GetQuestionByID(req.QuestionID)
	if err != nil {
		utils.LogError("Failed to get question %d for progress check: %v", req.QuestionID, err)
		return nil, err
	}

	isCorrect := utils.CheckAnswer(question, req.UserAnswer)

	utils.LogDB("Answer check for %s question: user='%s' vs correct='%s' -> %t",
		question.QuestionType, req.UserAnswer, question.Answer, isCorrect)

	result, err := db.Exec(`
        INSERT INTO progress (user_id, question_id, user_answer, is_correct, time_taken_seconds)
        VALUES (?, ?, ?, ?, ?)
    `, userID, req.QuestionID, req.UserAnswer, isCorrect, req.TimeTakenSeconds)

	if err != nil {
		duration := time.Since(start)
		utils.LogError("RecordProgress failed: %v (%v)", err, duration)
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		utils.LogError("Failed to get progress LastInsertId: %v", err)
		return nil, err
	}

	duration := time.Since(start)
	utils.LogDB("Progress recorded with ID %d (correct: %t) in %v", id, isCorrect, duration)

	return db.GetProgressByID(int(id))
}

func (db *DB) GetProgressByID(id int) (*models.Progress, error) {
	utils.LogDB("Executing query: GetProgressByID(%d)", id)

	var p models.Progress

	err := db.QueryRow(`
        SELECT id, user_id, question_id, user_answer, is_correct, answered_at, time_taken_seconds
        FROM progress WHERE id = ?
    `, id).Scan(&p.ID, &p.UserID, &p.QuestionID, &p.UserAnswer, &p.IsCorrect, &p.AnsweredAt, &p.TimeTakenSeconds)

	if err != nil {
		utils.LogError("GetProgressByID(%d) failed: %v", id, err)
		return nil, err
	}

	return &p, nil
}

func (db *DB) GetUserStats(userID int) (*models.Stats, error) {
	utils.LogDB("Calculating stats for user %d", userID)
	start := time.Now()

	stats := &models.Stats{
		Categories: make(map[string]models.CategoryStat),
	}

	err := db.QueryRow("SELECT COUNT(*) FROM questions").Scan(&stats.TotalQuestions)
	if err != nil {
		utils.LogError("Failed to count total questions: %v", err)
		return nil, err
	}

	// Use COALESCE to handle NULL values when a user has no progress
	err = db.QueryRow(`
        SELECT COALESCE(COUNT(*), 0) as answered, 
               COALESCE(SUM(CASE WHEN is_correct THEN 1 ELSE 0 END), 0) as correct
        FROM progress WHERE user_id = ?
    `, userID).Scan(&stats.Answered, &stats.Correct)
	if err != nil {
		utils.LogError("Failed to get user progress stats: %v", err)
		return nil, err
	}

	if stats.Answered > 0 {
		stats.Accuracy = float64(stats.Correct) / float64(stats.Answered)
	}

	stats.Streak = db.getCurrentStreak(userID)

	// Use COALESCE here too for consistency
	rows, err := db.Query(`
        SELECT q.category, 
               COALESCE(COUNT(*), 0) as answered,
               COALESCE(SUM(CASE WHEN p.is_correct THEN 1 ELSE 0 END), 0) as correct
        FROM progress p
        JOIN questions q ON p.question_id = q.id
        WHERE p.user_id = ?
        GROUP BY q.category
    `, userID)
	if err != nil {
		utils.LogError("Failed to get category stats: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var category string
		var answered, correct int

		err := rows.Scan(&category, &answered, &correct)
		if err != nil {
			utils.LogError("Failed to scan category stats: %v", err)
			return nil, err
		}

		stats.Categories[category] = models.CategoryStat{
			Answered: answered,
			Correct:  correct,
		}
	}

	duration := time.Since(start)
	utils.LogDB("Stats calculated for user %d: %d/%d correct (%.1f%%), streak %d, %d categories (%v)",
		userID, stats.Correct, stats.Answered, stats.Accuracy*100, stats.Streak, len(stats.Categories), duration)

	return stats, nil
}

func (db *DB) getCurrentStreak(userID int) int {
	utils.LogDB("Calculating streak for user %d", userID)

	rows, err := db.Query(`
        SELECT is_correct FROM progress 
        WHERE user_id = ? 
        ORDER BY answered_at DESC
        LIMIT 50
    `, userID)
	if err != nil {
		utils.LogError("Failed to get streak data: %v", err)
		return 0
	}
	defer rows.Close()

	streak := 0
	for rows.Next() {
		var isCorrect bool
		if err := rows.Scan(&isCorrect); err != nil {
			utils.LogError("Failed to scan streak row: %v", err)
			break
		}

		if isCorrect {
			streak++
		} else {
			break
		}
	}

	utils.LogDB("Current streak for user %d: %d", userID, streak)
	return streak
}
