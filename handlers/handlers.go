package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/adamspd/QuizzApi/auth"
	"github.com/adamspd/QuizzApi/db"
	"github.com/adamspd/QuizzApi/models"
	"github.com/adamspd/QuizzApi/utils"
)

// API wrapper to hold all handlers
type API struct {
	authHandlers     *AuthHandlers
	questionHandlers *QuestionHandlers
	progressHandlers *ProgressHandlers
}

func NewAPI(database *db.DB, sessionStore *auth.SessionStore, emailService *auth.EmailService, emailConfig *models.EmailConfig) *API {
	return &API{
		authHandlers:     NewAuthHandlers(database, sessionStore, emailService, emailConfig),
		questionHandlers: NewQuestionHandlers(database, sessionStore),
		progressHandlers: NewProgressHandlers(database, sessionStore),
	}
}

func NewRouter(database *db.DB, sessionStore *auth.SessionStore, emailConfig *models.EmailConfig) http.Handler {
	emailService := auth.NewEmailService(emailConfig)
	api := NewAPI(database, sessionStore, emailService, emailConfig)

	mux := http.NewServeMux()

	// Health check (no auth required)
	mux.HandleFunc("/health", healthCheck)

	// Auth endpoints (handle their own auth as needed)
	mux.HandleFunc("/auth/", api.authHandlers.HandleAuth)

	// Public verification endpoint (no auth required)
	mux.HandleFunc("/verify-email", api.authHandlers.verifyEmail)

	// Question routes with auth
	mux.HandleFunc("/questions", authMiddlewareWithEmailCheck(api.questionHandlers.HandleQuestions, sessionStore, database, emailConfig))
	mux.HandleFunc("/questions/", func(w http.ResponseWriter, r *http.Request) {
		// Check if this is an approval request
		if strings.Contains(r.URL.Path, "/approve") {
			// Parse the question ID from URL
			path := strings.TrimPrefix(r.URL.Path, "/questions/")
			parts := strings.Split(path, "/")
			if len(parts) == 2 && parts[1] == "approve" {
				if id, err := strconv.Atoi(parts[0]); err == nil {
					// Require moderator or admin role for approval
					requireRole("moderator", "admin")(authMiddlewareWithEmailCheck(func(w http.ResponseWriter, r *http.Request) {
						api.questionHandlers.HandleQuestionApproval(w, r, id)
					}, sessionStore, database, emailConfig))(w, r)
					return
				}
			}
			http.Error(w, "Invalid approval path", http.StatusBadRequest)
		} else {
			// Regular question by ID handling
			path := strings.TrimPrefix(r.URL.Path, "/questions/")
			id, err := strconv.Atoi(path)
			if err != nil {
				utils.LogHTTP("Invalid question ID: %s", path)
				http.Error(w, "Invalid question ID", http.StatusBadRequest)
				return
			}
			authMiddlewareWithEmailCheck(func(w http.ResponseWriter, r *http.Request) {
				api.questionHandlers.HandleQuestionByID(w, r, id)
			}, sessionStore, database, emailConfig)(w, r)
		}
	})
	mux.HandleFunc("/questions/next", authMiddlewareWithEmailCheck(api.questionHandlers.GetNextQuestions, sessionStore, database, emailConfig))

	// Progress routes with auth
	mux.HandleFunc("/progress", authMiddlewareWithEmailCheck(api.progressHandlers.HandleProgress, sessionStore, database, emailConfig))
	mux.HandleFunc("/progress/stats", authMiddlewareWithEmailCheck(api.progressHandlers.GetProgressStats, sessionStore, database, emailConfig))

	// Import/Export routes (require auth)
	mux.HandleFunc("/import", authMiddlewareWithEmailCheck(api.questionHandlers.ImportQuestions, sessionStore, database, emailConfig))

	// User management routes (admin only)
	mux.HandleFunc("/users", requireRole("admin")(authMiddlewareWithEmailCheck(api.authHandlers.HandleUsers, sessionStore, database, emailConfig)))
	mux.HandleFunc("/users/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/users/")
		id, err := strconv.Atoi(path)
		if err != nil {
			utils.LogHTTP("Invalid user ID: %s", path)
			http.Error(w, "Invalid user ID", http.StatusBadRequest)
			return
		}
		requireRole("admin")(authMiddlewareWithEmailCheck(func(w http.ResponseWriter, r *http.Request) {
			api.authHandlers.HandleUserByID(w, r, id)
		}, sessionStore, database, emailConfig))(w, r)
	})

	return corsMiddleware(mux)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	utils.LogHTTP("Health check requested")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "ok"}`))
}
