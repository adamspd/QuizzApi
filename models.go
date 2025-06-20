package main

import (
	"sync"
	"time"
)

// User represents a user in the system
type User struct {
	ID              int        `json:"id"`
	Username        string     `json:"username"`
	Email           string     `json:"email"`
	Role            string     `json:"role"`
	IsActive        bool       `json:"is_active"`
	EmailVerified   bool       `json:"email_verified"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// UserRequest for creating/updating users
type UserRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
	IsActive *bool  `json:"is_active,omitempty"`
}

// LoginRequest for authentication
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Session represents an active user session
type Session struct {
	ID        string    `json:"session_id"`
	UserID    int       `json:"user_id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SessionStore manages in-memory sessions
type SessionStore struct {
	sessions map[string]*Session
	mutex    sync.RWMutex
}

// NewSessionStore creates a new session store
func NewSessionStore() *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]*Session),
	}

	// Start a cleanup goroutine
	go store.cleanupExpiredSessions()

	return store
}

// CreateSession creates a new session for a user
func (s *SessionStore) CreateSession(user *User) *Session {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	sessionID := generateSessionID()
	session := &Session{
		ID:        sessionID,
		UserID:    user.ID,
		Username:  user.Username,
		Role:      user.Role,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(72 * time.Hour), // 72-hour sessions
	}

	s.sessions[sessionID] = session
	return session
}

// GetSession retrieves a session by ID
func (s *SessionStore) GetSession(sessionID string) (*Session, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return nil, false
	}

	// Check if expired
	if time.Now().After(session.ExpiresAt) {
		delete(s.sessions, sessionID)
		return nil, false
	}

	return session, true
}

// DeleteSession removes a session
func (s *SessionStore) DeleteSession(sessionID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.sessions, sessionID)
}

// DeleteUserSessions removes all sessions for a user
func (s *SessionStore) DeleteUserSessions(userID int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for id, session := range s.sessions {
		if session.UserID == userID {
			delete(s.sessions, id)
		}
	}
}

// cleanupExpiredSessions runs periodically to clean up expired sessions
func (s *SessionStore) cleanupExpiredSessions() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		s.mutex.Lock()
		now := time.Now()
		for id, session := range s.sessions {
			if now.After(session.ExpiresAt) {
				delete(s.sessions, id)
			}
		}
		s.mutex.Unlock()
	}
}

type Question struct {
	ID           int        `json:"id"`
	Category     string     `json:"category"`
	Question     string     `json:"question"`
	QuestionType string     `json:"question_type"`
	Choices      []string   `json:"choices,omitempty"`
	Answer       string     `json:"answer"`
	Keywords     []string   `json:"keywords"`
	Difficulty   string     `json:"difficulty"`
	CreatedBy    int        `json:"created_by"`
	Status       string     `json:"status"`
	ApprovedBy   *int       `json:"approved_by,omitempty"`
	ApprovedAt   *time.Time `json:"approved_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	// Include creator info for admin/moderator views
	CreatorUsername string `json:"creator_username,omitempty"`
}

// QuestionRequest updated to handle status changes
type QuestionRequest struct {
	Category     string   `json:"category"`
	Question     string   `json:"question"`
	QuestionType string   `json:"question_type"`
	Choices      []string `json:"choices,omitempty"`
	Answer       string   `json:"answer"`
	Keywords     []string `json:"keywords"`
	Difficulty   string   `json:"difficulty"`
	Status       string   `json:"status,omitempty"` // For moderator/admin use
}

// ApprovalRequest for question approval actions
type ApprovalRequest struct {
	Action string `json:"action"` // "approve" or "reject"
	Reason string `json:"reason,omitempty"`
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

// ImportRequest Import types
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
	db           *DB
	sessionStore *SessionStore
	emailService *EmailService
	emailConfig  *EmailConfig
}

func (session *Session) CanApproveQuestions() bool {
	return session.Role == "moderator" || session.Role == "admin"
}

func (session *Session) CanManageUsers() bool {
	return session.Role == "admin"
}

func (session *Session) CanEditQuestion(question *Question) bool {
	// Admins and moderators can edit any question
	if session.Role == "admin" || session.Role == "moderator" {
		return true
	}
	// Users can only edit their own pending questions
	return session.UserID == question.CreatedBy && question.Status == "pending"
}
