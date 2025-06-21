package models

import "time"

// Question represents a question in the system
type Question struct {
	ID              int        `json:"id"`
	Category        string     `json:"category"`
	Question        string     `json:"question"`
	QuestionType    string     `json:"question_type"`
	Choices         []string   `json:"choices,omitempty"`
	Answer          string     `json:"answer"`
	Keywords        []string   `json:"keywords"`
	Difficulty      string     `json:"difficulty"`
	CreatedBy       int        `json:"created_by"`
	Status          string     `json:"status"`
	ApprovedBy      *int       `json:"approved_by,omitempty"`
	ApprovedAt      *time.Time `json:"approved_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CreatorUsername string     `json:"creator_username,omitempty"`
}

// QuestionRequest for creating/updating questions
type QuestionRequest struct {
	Category     string   `json:"category"`
	Question     string   `json:"question"`
	QuestionType string   `json:"question_type"`
	Choices      []string `json:"choices,omitempty"`
	Answer       string   `json:"answer"`
	Keywords     []string `json:"keywords"`
	Difficulty   string   `json:"difficulty"`
	Status       string   `json:"status,omitempty"`
}

// ApprovalRequest for question approval actions
type ApprovalRequest struct {
	Action string `json:"action"` // "approve" or "reject"
	Reason string `json:"reason,omitempty"`
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
