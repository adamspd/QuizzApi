package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/adamspd/QuizzApi/auth"
	"github.com/adamspd/QuizzApi/db"
	"github.com/adamspd/QuizzApi/models"
	"github.com/adamspd/QuizzApi/utils"
)

// Context keys for storing user session
type contextKey string

const sessionContextKey contextKey = "session"

// extractSessionFromRequest gets session ID from Authorization header or cookie
func extractSessionFromRequest(r *http.Request) string {
	_auth := r.Header.Get("Authorization")

	if strings.HasPrefix(_auth, "Bearer ") {
		return strings.TrimPrefix(_auth, "Bearer ")
	}

	cookie, err := r.Cookie("session_id")
	if err != nil {
		return ""
	}
	return cookie.Value
}

// authMiddleware validates session and adds user context
func authMiddleware(sessionStore *auth.SessionStore) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			sessionID := extractSessionFromRequest(r)
			if sessionID == "" {
				http.Error(w, "Missing session token", http.StatusUnauthorized)
				return
			}

			session, exists := sessionStore.GetSession(sessionID)
			if !exists {
				http.Error(w, "Invalid or expired session", http.StatusUnauthorized)
				return
			}

			// Add session to request context
			ctx := context.WithValue(r.Context(), sessionContextKey, session)
			next.ServeHTTP(w, r.WithContext(ctx))
		}
	}
}

// authMiddlewareWithEmailCheck validates session and checks email verification during grace period
func authMiddlewareWithEmailCheck(next http.HandlerFunc, sessionStore *auth.SessionStore, database *db.DB, emailConfig *models.EmailConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := extractSessionFromRequest(r)
		if sessionID == "" {
			http.Error(w, "Missing session token", http.StatusUnauthorized)
			return
		}

		session, exists := sessionStore.GetSession(sessionID)
		if !exists {
			http.Error(w, "Invalid or expired session", http.StatusUnauthorized)
			return
		}

		// Check if user needs email verification
		user, err := database.GetUserByID(session.UserID)
		if err != nil {
			utils.LogError("Failed to get user for email verification check: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if !user.EmailVerified {
			inGracePeriod, err := database.IsUserInGracePeriod(user.ID, emailConfig.GracePeriod)
			if err != nil {
				utils.LogError("Failed to check grace period: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			if !inGracePeriod {
				utils.LogInfo("User %s (%d) blocked - email not verified and grace period expired", user.Username, user.ID)
				http.Error(w, "Email verification required. Please check your email and verify your account.", http.StatusForbidden)
				return
			}
		}

		// Add session to request context
		ctx := context.WithValue(r.Context(), sessionContextKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// optionalAuthMiddleware validates session if present, but doesn't require it
func optionalAuthMiddleware(sessionStore *auth.SessionStore) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			sessionID := extractSessionFromRequest(r)
			if sessionID != "" {
				if session, exists := sessionStore.GetSession(sessionID); exists {
					ctx := context.WithValue(r.Context(), sessionContextKey, session)
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		}
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

// getSessionFromContext extracts session from request context
func getSessionFromContext(ctx context.Context) *models.Session {
	session, ok := ctx.Value(sessionContextKey).(*models.Session)
	if !ok {
		return nil
	}
	return session
}

// rateLimitMiddleware - basic rate limiting (optional, for future use)
func rateLimitMiddleware(requestsPerMinute int) func(http.HandlerFunc) http.HandlerFunc {
	// Simple in-memory rate limiter - you'd want Redis or similar for production
	clients := make(map[string][]time.Time)

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			clientIP := r.RemoteAddr
			now := time.Now()

			// Clean old requests (older than 1 minute)
			var validRequests []time.Time
			if requests, exists := clients[clientIP]; exists {
				for _, reqTime := range requests {
					if now.Sub(reqTime) < time.Minute {
						validRequests = append(validRequests, reqTime)
					}
				}
			}

			// Check rate limit
			if len(validRequests) >= requestsPerMinute {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			// Add current request
			validRequests = append(validRequests, now)
			clients[clientIP] = validRequests

			next.ServeHTTP(w, r)
		}
	}
}

// loggingMiddleware logs HTTP requests
func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer that captures status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)
		utils.LogHTTP("%s %s %d %v", r.Method, r.URL.Path, wrapped.statusCode, duration)
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
