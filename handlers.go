package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func NewRouter(db *DB, sessionStore *SessionStore, emailConfig *EmailConfig) http.Handler {
	emailService := NewEmailService(emailConfig)
	api := &API{
		db:           db,
		sessionStore: sessionStore,
		emailService: emailService,
		emailConfig:  emailConfig,
	}

	mux := http.NewServeMux()

	// Health check (no auth required)
	mux.HandleFunc("/health", api.healthCheck)

	// Auth endpoints (handle their own auth as needed)
	mux.HandleFunc("/auth/", api.handleAuth)

	// Public verification endpoint (no auth required)
	mux.HandleFunc("/verify-email", api.verifyEmail)

	// Question routes with auth
	mux.HandleFunc("/questions", api.authMiddlewareWithEmailCheck(api.handleQuestions))
	mux.HandleFunc("/questions/", func(w http.ResponseWriter, r *http.Request) {
		// Check if this is an approval request
		if strings.Contains(r.URL.Path, "/approve") {
			// Require moderator or admin role for approval
			requireRole("moderator", "admin")(api.authMiddlewareWithEmailCheck(api.handleQuestionApproval))(w, r)
		} else {
			api.authMiddlewareWithEmailCheck(api.handleQuestionByID)(w, r)
		}
	})
	mux.HandleFunc("/questions/next", api.authMiddlewareWithEmailCheck(api.getNextQuestions))

	// Progress routes with auth
	mux.HandleFunc("/progress", api.authMiddlewareWithEmailCheck(api.handleProgress))
	mux.HandleFunc("/progress/stats", api.authMiddlewareWithEmailCheck(api.getProgressStats))

	// Import/Export routes (require auth)
	mux.HandleFunc("/import", api.authMiddlewareWithEmailCheck(api.importQuestions))

	// User management routes (admin only)
	mux.HandleFunc("/users", requireRole("admin")(api.authMiddlewareWithEmailCheck(api.handleUsers)))
	mux.HandleFunc("/users/", requireRole("admin")(api.authMiddlewareWithEmailCheck(api.handleUserByID)))

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

func (api *API) healthCheck(w http.ResponseWriter, r *http.Request) {
	logHTTP("Health check requested")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "ok"}`))
}

func (api *API) handleQuestions(w http.ResponseWriter, r *http.Request) {
	logHTTP("%s /questions", r.Method)
	switch r.Method {
	case http.MethodGet:
		api.getQuestionsWithAuth(w, r)
	case http.MethodPost:
		api.createQuestionWithAuth(w, r)
	default:
		logHTTP("Method %s not allowed for /questions", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *API) handleQuestionByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/questions/")
	id, err := strconv.Atoi(path)
	if err != nil {
		logHTTP("Invalid question ID: %s", path)
		http.Error(w, "Invalid question ID", http.StatusBadRequest)
		return
	}

	logHTTP("%s /questions/%d", r.Method, id)
	switch r.Method {
	case http.MethodGet:
		api.getQuestionByIDWithAuth(w, r, id)
	case http.MethodPut:
		api.updateQuestionWithAuth(w, r, id)
	case http.MethodDelete:
		api.deleteQuestionWithAuth(w, r, id)
	default:
		logHTTP("Method %s not allowed for /questions/%d", r.Method, id)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *API) getQuestionsWithAuth(w http.ResponseWriter, r *http.Request) {
	session := api.getCurrentUser(r)

	questions, err := api.db.GetAllQuestionsForUser(session.UserID, session.Role)
	if err != nil {
		logError("Failed to fetch questions: %v", err)
		http.Error(w, "Failed to fetch questions", http.StatusInternalServerError)
		return
	}

	logHTTP("Returning %d questions for user %s", len(questions), session.Username)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"questions": questions,
	})
}

func (api *API) createQuestionWithAuth(w http.ResponseWriter, r *http.Request) {
	session := api.getCurrentUser(r)

	var req QuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logHTTP("Invalid JSON in create request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Set question status based on user role
	if session.Role == "admin" {
		// Admins can create approved questions directly
		if req.Status == "" {
			req.Status = "approved"
		}
	} else {
		// Regular users and moderators create pending questions
		req.Status = "pending"
	}

	// Create question using updated function that accepts creator ID
	question, err := api.db.CreateQuestionWithAuth(req, session.UserID, session.Role)
	if err != nil {
		logError("Failed to create question: %v", err)
		http.Error(w, "Failed to create question", http.StatusInternalServerError)
		return
	}

	logHTTP("Created question ID %d by user %s (status: %s)", question.ID, session.Username, question.Status)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(question)
}

func (api *API) getQuestionByIDWithAuth(w http.ResponseWriter, r *http.Request, id int) {
	session := api.getCurrentUser(r)

	question, err := api.db.GetQuestionByID(id)
	if err != nil {
		logHTTP("Question ID %d not found: %v", id, err)
		http.Error(w, "Question not found", http.StatusNotFound)
		return
	}

	// Check permissions - users can only see approved questions or their own
	if session.Role != "admin" && session.Role != "moderator" {
		if question.Status != "approved" && question.CreatedBy != session.UserID {
			http.Error(w, "Question not found", http.StatusNotFound)
			return
		}
	}

	logHTTP("Returning question ID %d", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(question)
}

func (api *API) updateQuestionWithAuth(w http.ResponseWriter, r *http.Request, id int) {
	session := api.getCurrentUser(r)

	// Get existing question to check permissions
	question, err := api.db.GetQuestionByID(id)
	if err != nil {
		logHTTP("Question ID %d not found: %v", id, err)
		http.Error(w, "Question not found", http.StatusNotFound)
		return
	}

	// Check if user can edit this question
	if !session.CanEditQuestion(question) {
		http.Error(w, "Insufficient permissions to edit this question", http.StatusForbidden)
		return
	}

	var req QuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logHTTP("Invalid JSON in update request for ID %d: %v", id, err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Regular users can't change status, moderators/admins can
	if session.Role == "user" {
		req.Status = "" // Preserve existing status
	}

	updatedQuestion, err := api.db.UpdateQuestionWithAuth(id, req, session.UserID, session.Role)
	if err != nil {
		logError("Failed to update question ID %d: %v", id, err)
		http.Error(w, "Failed to update question", http.StatusInternalServerError)
		return
	}

	logHTTP("Updated question ID %d by user %s", id, session.Username)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedQuestion)
}

func (api *API) deleteQuestionWithAuth(w http.ResponseWriter, r *http.Request, id int) {
	session := api.getCurrentUser(r)

	// Get existing question to check permissions
	question, err := api.db.GetQuestionByID(id)
	if err != nil {
		logHTTP("Question ID %d not found: %v", id, err)
		http.Error(w, "Question not found", http.StatusNotFound)
		return
	}

	// Check if user can delete this question
	if !session.CanEditQuestion(question) {
		http.Error(w, "Insufficient permissions to delete this question", http.StatusForbidden)
		return
	}

	err = api.db.DeleteQuestion(id)
	if err != nil {
		logError("Failed to delete question ID %d: %v", id, err)
		http.Error(w, "Failed to delete question", http.StatusInternalServerError)
		return
	}

	logHTTP("Deleted question ID %d by user %s", id, session.Username)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) getNextQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logHTTP("Method %s not allowed for /questions/next", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session := api.getCurrentUser(r)

	count := 10
	if countStr := r.URL.Query().Get("count"); countStr != "" {
		if c, err := strconv.Atoi(countStr); err == nil && c > 0 && c <= 50 {
			count = c
		}
	}

	logHTTP("Getting next %d questions for user %s", count, session.Username)
	questions, err := api.db.GetNextQuestionsForUser(session.UserID, count)
	if err != nil {
		logError("Failed to fetch next questions: %v", err)
		http.Error(w, "Failed to fetch next questions", http.StatusInternalServerError)
		return
	}

	logHTTP("Returning %d next questions", len(questions))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"questions": questions,
		"count":     len(questions),
	})
}

func (api *API) handleProgress(w http.ResponseWriter, r *http.Request) {
	logHTTP("%s /progress", r.Method)
	switch r.Method {
	case http.MethodPost:
		api.recordProgress(w, r)
	default:
		logHTTP("Method %s not allowed for /progress", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *API) recordProgress(w http.ResponseWriter, r *http.Request) {
	var req ProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logHTTP("Invalid JSON in progress request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.QuestionID == 0 || req.UserAnswer == "" {
		logHTTP("Missing required fields in progress request")
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// TODO: Get user ID from JWT when auth is implemented
	userID := 1

	logHTTP("Recording progress for user %d, question %d", userID, req.QuestionID)
	progress, err := api.db.RecordProgress(userID, req)
	if err != nil {
		logError("Failed to record progress: %v", err)
		http.Error(w, "Failed to record progress", http.StatusInternalServerError)
		return
	}

	logHTTP("Progress recorded: ID %d, correct: %t", progress.ID, progress.IsCorrect)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(progress)
}

func (api *API) getProgressStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logHTTP("Method %s not allowed for /progress/stats", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// TODO: Get user ID from JWT when auth is implemented
	userID := 1

	logHTTP("Getting stats for user %d", userID)
	stats, err := api.db.GetUserStats(userID)
	if err != nil {
		logError("Failed to fetch stats: %v", err)
		http.Error(w, "Failed to fetch stats", http.StatusInternalServerError)
		return
	}

	logHTTP("Returning stats: %d/%d correct (%.1f%%)", stats.Correct, stats.Answered, stats.Accuracy*100)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (api *API) importQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		logHTTP("Method %s not allowed for /import", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logImport("Starting question import process")

	var importReq ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&importReq); err != nil {
		logError("Invalid JSON in import request: %v", err)
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	logImport("Received import request with %d questions", len(importReq.Questions))

	if len(importReq.Questions) == 0 {
		logImport("No questions provided in import request")
		http.Error(w, "No questions provided", http.StatusBadRequest)
		return
	}

	if len(importReq.Questions) > 1000 {
		logImport("Too many questions in import request: %d (max 1000)", len(importReq.Questions))
		http.Error(w, "Too many questions (max 1000 per import)", http.StatusBadRequest)
		return
	}

	result, err := api.db.ImportQuestions(importReq)
	if err != nil {
		logError("Import failed: %v", err)
		http.Error(w, "Import failed", http.StatusInternalServerError)
		return
	}

	logImport("Import completed: %d imported, %d skipped, %d errors",
		result.ImportedQuestions, result.SkippedQuestions, len(result.Errors))

	w.Header().Set("Content-Type", "application/json")
	if result.ImportedQuestions > 0 {
		w.WriteHeader(http.StatusCreated)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(result)
}

func (api *API) handleAuth(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/auth/")

	switch {
	case path == "register" && r.Method == http.MethodPost:
		api.register(w, r)
	case path == "login" && r.Method == http.MethodPost:
		api.login(w, r)
	case path == "logout" && r.Method == http.MethodPost:
		api.logout(w, r)
	case path == "me" && r.Method == http.MethodGet:
		api.getCurrentUserInfo(w, r)
	case path == "verify-email" && r.Method == http.MethodGet:
		api.verifyEmail(w, r)
	case path == "resend-verification" && r.Method == http.MethodPost:
		api.resendVerification(w, r)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// register creates a new user account
func (api *API) register(w http.ResponseWriter, r *http.Request) {
	logHTTP("POST /auth/register")

	var req UserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logHTTP("Invalid JSON in register request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Create the user
	user, err := api.db.CreateUser(req)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			if strings.Contains(err.Error(), "username") {
				http.Error(w, "Username already exists", http.StatusConflict)
			} else if strings.Contains(err.Error(), "email") {
				http.Error(w, "Email already exists", http.StatusConflict)
			} else {
				http.Error(w, "User already exists", http.StatusConflict)
			}
		} else {
			logError("Failed to create user: %v", err)
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
		}
		return
	}

	// Create email verification token
	verification, err := api.db.createEmailVerification(user.ID, user.Email)
	if err != nil {
		logError("Failed to create email verification: %v", err)
		// Don't fail registration, just log the error
	} else {
		// Send verification email
		if err := api.emailService.sendVerificationEmail(user, verification.Token); err != nil {
			logError("Failed to send verification email: %v", err)
			// Don't fail registration, just log the error
		}
	}

	// Create session for immediate login
	session := api.sessionStore.CreateSession(user)

	logHTTP("User registered successfully: %s (ID: %d)", user.Username, user.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user":    user,
		"session": session,
		"message": "Registration successful. Please check your email to verify your account.",
	})
}

// login authenticates a user and creates a session
func (api *API) login(w http.ResponseWriter, r *http.Request) {
	logHTTP("POST /auth/login")

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logHTTP("Invalid JSON in login request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Authenticate user
	user, err := api.db.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		logHTTP("Login failed for user: %s", req.Username)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Create session
	session := api.sessionStore.CreateSession(user)

	logHTTP("User logged in successfully: %s (ID: %d)", user.Username, user.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user":    user,
		"session": session,
		"message": "Login successful",
	})
}

// logout destroys the current session
func (api *API) logout(w http.ResponseWriter, r *http.Request) {
	logHTTP("POST /auth/logout")

	sessionID := extractSessionFromRequest(r)
	if sessionID != "" {
		api.sessionStore.DeleteSession(sessionID)
		logHTTP("Session %s destroyed", sessionID[:8]+"...")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Logout successful",
	})
}

// getCurrentUserInfo returns current user information
func (api *API) getCurrentUserInfo(w http.ResponseWriter, r *http.Request) {
	// Extract session manually since this endpoint handles its own auth
	sessionID := extractSessionFromRequest(r)
	if sessionID == "" {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	session, exists := api.sessionStore.GetSession(sessionID)
	if !exists {
		http.Error(w, "Invalid or expired session", http.StatusUnauthorized)
		return
	}

	user, err := api.db.GetUserByID(session.UserID)
	if err != nil {
		logError("Failed to get current user info: %v", err)
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}

	// Add grace period info for unverified users
	response := map[string]interface{}{
		"user": user,
	}

	if !user.EmailVerified {
		inGracePeriod, err := api.db.isUserInGracePeriod(user.ID, api.emailConfig.GracePeriod)
		if err == nil {
			response["email_verification"] = map[string]interface{}{
				"in_grace_period":    inGracePeriod,
				"grace_period_hours": int(api.emailConfig.GracePeriod.Hours()),
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// verifyEmail handles email verification
func (api *API) verifyEmail(w http.ResponseWriter, r *http.Request) {
	logHTTP("GET /auth/verify-email")

	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Verification token is required", http.StatusBadRequest)
		return
	}

	user, err := api.db.verifyEmailToken(token)
	if err != nil {
		logHTTP("Email verification failed: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	logHTTP("Email verified successfully for user: %s (ID: %d)", user.Username, user.ID)

	// Return HTML response for web browsers
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`
<!DOCTYPE html>
<html>
<head>
    <title>Email Verified</title>
    <style>
        body { font-family: Arial, sans-serif; text-align: center; padding: 50px; }
        .success { color: #28a745; }
        .container { max-width: 500px; margin: 0 auto; }
    </style>
</head>
<body>
    <div class="container">
        <h1 class="success">✓ Email Verified!</h1>
        <p>Your email address has been successfully verified.</p>
        <p>You can now use the French Citizenship Training app without restrictions.</p>
        <p><a href="/">Return to App</a></p>
    </div>
</body>
</html>
	`))
}

// resendVerification sends a new verification email
func (api *API) resendVerification(w http.ResponseWriter, r *http.Request) {
	logHTTP("POST /auth/resend-verification")

	// Extract session manually since this endpoint handles its own auth
	sessionID := extractSessionFromRequest(r)
	if sessionID == "" {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	session, exists := api.sessionStore.GetSession(sessionID)
	if !exists {
		http.Error(w, "Invalid or expired session", http.StatusUnauthorized)
		return
	}

	user, err := api.db.GetUserByID(session.UserID)
	if err != nil {
		logError("Failed to get user for verification resend: %v", err)
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}

	if user.EmailVerified {
		http.Error(w, "Email already verified", http.StatusBadRequest)
		return
	}

	// Create new verification token
	verification, err := api.db.createEmailVerification(user.ID, user.Email)
	if err != nil {
		logError("Failed to create email verification: %v", err)
		http.Error(w, "Failed to create verification token", http.StatusInternalServerError)
		return
	}

	// Send verification email
	if err := api.emailService.sendVerificationEmail(user, verification.Token); err != nil {
		logError("Failed to send verification email: %v", err)
		http.Error(w, "Failed to send verification email", http.StatusInternalServerError)
		return
	}

	logHTTP("Verification email resent to user: %s (ID: %d)", user.Username, user.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Verification email sent successfully",
	})
}

func (api *API) handleUsers(w http.ResponseWriter, r *http.Request) {
	logHTTP("%s /users", r.Method)
	switch r.Method {
	case http.MethodGet:
		api.getUsers(w, r)
	case http.MethodPost:
		api.createUserByAdmin(w, r)
	default:
		logHTTP("Method %s not allowed for /users", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *API) handleUserByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/users/")
	id, err := strconv.Atoi(path)
	if err != nil {
		logHTTP("Invalid user ID: %s", path)
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	logHTTP("%s /users/%d", r.Method, id)
	switch r.Method {
	case http.MethodGet:
		api.getUserByID(w, r, id)
	case http.MethodPut:
		api.updateUserByAdmin(w, r, id)
	case http.MethodDelete:
		api.deleteUserByAdmin(w, r, id)
	default:
		logHTTP("Method %s not allowed for /users/%d", r.Method, id)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *API) getUsers(w http.ResponseWriter, r *http.Request) {
	users, err := api.db.GetAllUsers()
	if err != nil {
		logError("Failed to fetch users: %v", err)
		http.Error(w, "Failed to fetch users", http.StatusInternalServerError)
		return
	}

	logHTTP("Returning %d users", len(users))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"users": users,
	})
}

func (api *API) getUserByID(w http.ResponseWriter, r *http.Request, id int) {
	session := api.getCurrentUser(r)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Users can only see their own info unless they're admin
	if session.UserID != id && session.Role != "admin" {
		http.Error(w, "Insufficient permissions", http.StatusForbidden)
		return
	}

	user, err := api.db.GetUserByID(id)
	if err != nil {
		logHTTP("User ID %d not found: %v", id, err)
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	logHTTP("Returning user ID %d", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (api *API) createUserByAdmin(w http.ResponseWriter, r *http.Request) {
	var req UserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logHTTP("Invalid JSON in create user request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	user, err := api.db.CreateUser(req)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			if strings.Contains(err.Error(), "username") {
				http.Error(w, "Username already exists", http.StatusConflict)
			} else if strings.Contains(err.Error(), "email") {
				http.Error(w, "Email already exists", http.StatusConflict)
			} else {
				http.Error(w, "User already exists", http.StatusConflict)
			}
		} else {
			logError("Failed to create user: %v", err)
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
		}
		return
	}

	logHTTP("Created user ID %d by admin", user.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

func (api *API) updateUserByAdmin(w http.ResponseWriter, r *http.Request, id int) {
	var req UserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logHTTP("Invalid JSON in update user request for ID %d: %v", id, err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	user, err := api.db.UpdateUser(id, req)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "User not found", http.StatusNotFound)
		} else {
			logError("Failed to update user ID %d: %v", id, err)
			http.Error(w, "Failed to update user", http.StatusInternalServerError)
		}
		return
	}

	// If user was deactivated, kill their sessions
	if req.IsActive != nil && !*req.IsActive {
		api.sessionStore.DeleteUserSessions(id)
		logHTTP("Deleted sessions for deactivated user ID %d", id)
	}

	logHTTP("Updated user ID %d by admin", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (api *API) deleteUserByAdmin(w http.ResponseWriter, r *http.Request, id int) {
	session := api.getCurrentUser(r)
	if session.UserID == id {
		http.Error(w, "Cannot delete your own account", http.StatusBadRequest)
		return
	}

	err := api.db.DeleteUser(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "User not found", http.StatusNotFound)
		} else {
			logError("Failed to delete user ID %d: %v", id, err)
			http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		}
		return
	}

	// Kill user's sessions
	api.sessionStore.DeleteUserSessions(id)

	logHTTP("Deleted user ID %d by admin", id)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) handleQuestionApproval(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/questions/")
	parts := strings.Split(path, "/")

	if len(parts) != 2 || parts[1] != "approve" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(parts[0])
	if err != nil {
		logHTTP("Invalid question ID: %s", parts[0])
		http.Error(w, "Invalid question ID", http.StatusBadRequest)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	api.approveQuestion(w, r, id)
}

func (api *API) approveQuestion(w http.ResponseWriter, r *http.Request, questionID int) {
	session := api.getCurrentUser(r)

	var req ApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logHTTP("Invalid JSON in approval request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Action != "approve" && req.Action != "reject" {
		http.Error(w, "Action must be 'approve' or 'reject'", http.StatusBadRequest)
		return
	}

	// Get the question
	question, err := api.db.GetQuestionByID(questionID)
	if err != nil {
		logHTTP("Question ID %d not found: %v", questionID, err)
		http.Error(w, "Question not found", http.StatusNotFound)
		return
	}

	if question.Status != "pending" {
		http.Error(w, "Question is not pending approval", http.StatusBadRequest)
		return
	}

	// Update question status
	var newStatus string
	if req.Action == "approve" {
		newStatus = "approved"
	} else {
		newStatus = "rejected"
	}

	_, err = api.db.Exec(`
		UPDATE questions 
		SET status = ?, approved_by = ?, approved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, newStatus, session.UserID, questionID)

	if err != nil {
		logError("Failed to update question approval status: %v", err)
		http.Error(w, "Failed to update question", http.StatusInternalServerError)
		return
	}

	logHTTP("Question ID %d %s by user %s", questionID, req.Action+"d", session.Username)

	// Return updated question
	updatedQuestion, _ := api.db.GetQuestionByID(questionID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedQuestion)
}
