package auth

import (
	"sync"
	"time"

	"github.com/adamspd/QuizzApi/models"
	"github.com/adamspd/QuizzApi/utils"
)

type SessionStore struct {
	sessions map[string]*models.Session
	mutex    sync.RWMutex
}

func NewSessionStore() *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]*models.Session),
	}

	// Start a cleanup goroutine
	go store.cleanupExpiredSessions()

	return store
}

func (s *SessionStore) CreateSession(user *models.User) *models.Session {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	sessionID := utils.GenerateSessionID()
	session := &models.Session{
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

func (s *SessionStore) GetSession(sessionID string) (*models.Session, bool) {
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

func (s *SessionStore) DeleteSession(sessionID string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.sessions, sessionID)
}

func (s *SessionStore) DeleteUserSessions(userID int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for id, session := range s.sessions {
		if session.UserID == userID {
			delete(s.sessions, id)
		}
	}
}

func (s *SessionStore) cleanupExpiredSessions() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		s.mutex.Lock()
		now := time.Now()
		cleaned := 0
		for id, session := range s.sessions {
			if now.After(session.ExpiresAt) {
				delete(s.sessions, id)
				cleaned++
			}
		}
		if cleaned > 0 {
			utils.LogInfo("Cleaned up %d expired sessions", cleaned)
		}
		s.mutex.Unlock()
	}
}
