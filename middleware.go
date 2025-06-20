package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Context keys for storing user session
type contextKey string

const sessionContextKey contextKey = "session"

// generateSessionID creates a random session ID
func generateSessionID() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to time-based ID if crypto random fails
		logError("Failed to generate crypto random session ID: %v", err)
		return hex.EncodeToString([]byte(string(rune(time.Now().UnixNano()))))
	}
	return hex.EncodeToString(bytes)
}

// hashPassword creates a bcrypt hash of the password
func hashPassword(password string) (string, error) {
	if len(password) < 6 {
		return "", fmt.Errorf("password must be at least 6 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// checkPassword verifies a password against its hash
func checkPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// extractSessionFromRequest gets session ID from Authorization header or cookie
func extractSessionFromRequest(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	log.Printf("Authorization header: %s", auth)

	if strings.HasPrefix(auth, "Bearer ") {
		session := strings.TrimPrefix(auth, "Bearer ")
		log.Printf("Using Bearer session: %s", session)
		return session
	}

	cookie, err := r.Cookie("session_id")
	if err != nil {
		log.Printf("No session_id cookie found: %v", err)
		return ""
	}
	log.Printf("Found session_id cookie: %s", cookie.Value)
	return cookie.Value
}

// authMiddleware validates session and adds user context
func (api *API) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := extractSessionFromRequest(r)
		if sessionID == "" {
			http.Error(w, "Missing session token", http.StatusUnauthorized)
			return
		}

		session, exists := api.sessionStore.GetSession(sessionID)
		if !exists {
			http.Error(w, "Invalid or expired session", http.StatusUnauthorized)
			return
		}

		// Add session to request context
		ctx := context.WithValue(r.Context(), sessionContextKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// optionalAuthMiddleware validates session if present, but doesn't require it
func (api *API) optionalAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := extractSessionFromRequest(r)
		if sessionID != "" {
			if session, exists := api.sessionStore.GetSession(sessionID); exists {
				ctx := context.WithValue(r.Context(), sessionContextKey, session)
				r = r.WithContext(ctx)
			}
		}
		next.ServeHTTP(w, r)
	}
}

// requireRole middleware checks if user has required role
func requireRole(roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			session := getSessionFromContext(r.Context())
			if session == nil {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}

			hasRole := false
			for _, role := range roles {
				if session.Role == role {
					hasRole = true
					break
				}
			}

			if !hasRole {
				http.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		}
	}
}

// getSessionFromContext extracts session from request context
func getSessionFromContext(ctx context.Context) *Session {
	session, ok := ctx.Value(sessionContextKey).(*Session)
	if !ok {
		return nil
	}
	return session
}

// getCurrentUser gets current user from session context
func (api *API) getCurrentUser(r *http.Request) *Session {
	return getSessionFromContext(r.Context())
}

// validateUserRequest validates user creation/update requests
func validateUserRequest(req *UserRequest, isUpdate bool) error {
	if strings.TrimSpace(req.Username) == "" {
		return fmt.Errorf("username is required")
	}

	if strings.TrimSpace(req.Email) == "" {
		return fmt.Errorf("email is required")
	}

	// Password required for creation, optional for updates
	if !isUpdate && strings.TrimSpace(req.Password) == "" {
		return fmt.Errorf("password is required")
	}

	if req.Password != "" && len(req.Password) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}

	// Validate role
	validRoles := []string{"user", "moderator", "admin"}
	if req.Role != "" {
		roleValid := false
		for _, role := range validRoles {
			if req.Role == role {
				roleValid = true
				break
			}
		}
		if !roleValid {
			return fmt.Errorf("invalid role: %s", req.Role)
		}
	}

	return nil
}

// requireOwnershipOrRole checks if user owns resource or has required role
func requireOwnershipOrRole(resourceUserID int, roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			session := getSessionFromContext(r.Context())
			if session == nil {
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}

			// Check if user owns the resource
			if session.UserID == resourceUserID {
				next.ServeHTTP(w, r)
				return
			}

			// Check if user has required role
			hasRole := false
			for _, role := range roles {
				if session.Role == role {
					hasRole = true
					break
				}
			}

			if !hasRole {
				http.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		}
	}
}
