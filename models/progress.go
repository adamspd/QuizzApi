package models

import "time"

// Progress represents user progress on questions
type Progress struct {
	ID               int       `json:"id"`
	UserID           int       `json:"user_id"`
	QuestionID       int       `json:"question_id"`
	UserAnswer       string    `json:"user_answer"`
	IsCorrect        bool      `json:"is_correct"`
	AnsweredAt       time.Time `json:"answered_at"`
	TimeTakenSeconds int       `json:"time_taken_seconds"`
}

// ProgressRequest for recording progress
type ProgressRequest struct {
	QuestionID       int    `json:"question_id"`
	UserAnswer       string `json:"user_answer"`
	TimeTakenSeconds int    `json:"time_taken_seconds"`
}

// Stats represents user statistics
type Stats struct {
	TotalQuestions int                     `json:"total_questions"`
	Answered       int                     `json:"answered"`
	Correct        int                     `json:"correct"`
	Accuracy       float64                 `json:"accuracy"`
	Streak         int                     `json:"streak"`
	Categories     map[string]CategoryStat `json:"categories"`
}

// CategoryStat represents stats for a specific category
type CategoryStat struct {
	Answered int `json:"answered"`
	Correct  int `json:"correct"`
}
