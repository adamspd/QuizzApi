package main

import "time"

// Core data structures
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

// Import types
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

// API wrapper
type API struct {
	db *DB
}
