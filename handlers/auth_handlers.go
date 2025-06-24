package handlers

import (
	"encoding/json"
	"github.com/adamspd/QuizzApi/jobs"
	"net/http"
	"strings"

	"github.com/adamspd/QuizzApi/auth"
	"github.com/adamspd/QuizzApi/db"
	"github.com/adamspd/QuizzApi/models"
	"github.com/adamspd/QuizzApi/utils"
)

type AuthHandlers struct {
	db           *db.DB
	sessionStore *auth.SessionStore
	emailService *auth.EmailService
	emailConfig  *models.EmailConfig
	jobManager   *jobs.JobManager // Assuming you have a job manager for email jobs
}

func NewAuthHandlers(database *db.DB, sessionStore *auth.SessionStore, emailService *auth.EmailService, emailConfig *models.EmailConfig, jobManager *jobs.JobManager) *AuthHandlers {
	return &AuthHandlers{
		db:           database,
		sessionStore: sessionStore,
		emailService: emailService,
		emailConfig:  emailConfig,
		jobManager:   jobManager,
	}
}

func (ah *AuthHandlers) HandleAuth(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/auth/")

	switch {
	case path == "register" && r.Method == http.MethodPost:
		ah.register(w, r)
	case path == "login" && r.Method == http.MethodPost:
		ah.login(w, r)
	case path == "logout" && r.Method == http.MethodPost:
		ah.logout(w, r)
	case path == "me" && r.Method == http.MethodGet:
		ah.getCurrentUserInfo(w, r)
	case path == "verify-email" && r.Method == http.MethodGet:
		ah.verifyEmail(w, r)
	case path == "resend-verification" && r.Method == http.MethodPost:
		ah.resendVerification(w, r)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func (ah *AuthHandlers) register(w http.ResponseWriter, r *http.Request) {
	utils.LogHTTP("POST /auth/register")

	var req models.UserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogHTTP("Invalid JSON in register request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Create the user
	user, err := ah.db.CreateUser(req)
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
			utils.LogError("Failed to create user: %v", err)
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
		}
		return
	}

	// Create email verification token
	verification, err := ah.db.CreateEmailVerification(user.ID, user.Email)
	if err != nil {
		utils.LogError("Failed to create email verification: %v", err)
	} else {
		// Build email content
		subject, body := ah.emailService.BuildVerificationEmail(user, verification.Token)

		// Queue verification email
		if err := ah.jobManager.QueueVerificationEmail(user.Email, subject, body, user.ID, verification.Token); err != nil {
			utils.LogError("Failed to queue verification email: %v", err)
		}
	}

	// Create session for immediate login
	session := ah.sessionStore.CreateSession(user)

	utils.LogHTTP("User registered successfully: %s (ID: %d)", user.Username, user.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user":    user,
		"session": session,
		"message": "Registration successful. Please check your email to verify your account.",
	})
}

func (ah *AuthHandlers) login(w http.ResponseWriter, r *http.Request) {
	utils.LogHTTP("POST /auth/login")

	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogHTTP("Invalid JSON in login request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Authenticate user
	user, err := ah.db.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		utils.LogHTTP("Login failed for user: %s", req.Username)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Create session
	session := ah.sessionStore.CreateSession(user)

	utils.LogHTTP("User logged in successfully: %s (ID: %d)", user.Username, user.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user":    user,
		"session": session,
		"message": "Login successful",
	})
}

func (ah *AuthHandlers) logout(w http.ResponseWriter, r *http.Request) {
	utils.LogHTTP("POST /auth/logout")

	sessionID := extractSessionFromRequest(r)
	if sessionID != "" {
		ah.sessionStore.DeleteSession(sessionID)
		utils.LogHTTP("Session %s destroyed", sessionID[:8]+"...")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Logout successful",
	})
}

func (ah *AuthHandlers) getCurrentUserInfo(w http.ResponseWriter, r *http.Request) {
	// Extract session manually since this endpoint handles its own auth
	sessionID := extractSessionFromRequest(r)
	if sessionID == "" {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	session, exists := ah.sessionStore.GetSession(sessionID)
	if !exists {
		http.Error(w, "Invalid or expired session", http.StatusUnauthorized)
		return
	}

	user, err := ah.db.GetUserByID(session.UserID)
	if err != nil {
		utils.LogError("Failed to get current user info: %v", err)
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}

	// Add grace period info for unverified users
	response := map[string]interface{}{
		"user": user,
	}

	if !user.EmailVerified {
		inGracePeriod, err := ah.db.IsUserInGracePeriod(user.ID, ah.emailConfig.GracePeriod)
		if err == nil {
			response["email_verification"] = map[string]interface{}{
				"in_grace_period":    inGracePeriod,
				"grace_period_hours": int(ah.emailConfig.GracePeriod.Hours()),
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (ah *AuthHandlers) verifyEmail(w http.ResponseWriter, r *http.Request) {
	utils.LogHTTP("GET /auth/verify-email")

	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Verification token is required", http.StatusBadRequest)
		return
	}

	user, err := ah.db.VerifyEmailToken(token)
	if err != nil {
		utils.LogHTTP("Email verification failed: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.LogHTTP("Email verified successfully for user: %s (ID: %d)", user.Username, user.ID)

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

func (ah *AuthHandlers) resendVerification(w http.ResponseWriter, r *http.Request) {
	utils.LogHTTP("POST /auth/resend-verification")

	// Extract session manually since this endpoint handles its own auth
	sessionID := extractSessionFromRequest(r)
	if sessionID == "" {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	session, exists := ah.sessionStore.GetSession(sessionID)
	if !exists {
		http.Error(w, "Invalid or expired session", http.StatusUnauthorized)
		return
	}

	user, err := ah.db.GetUserByID(session.UserID)
	if err != nil {
		utils.LogError("Failed to get user for verification resend: %v", err)
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}

	if user.EmailVerified {
		http.Error(w, "Email already verified", http.StatusBadRequest)
		return
	}

	// Create new verification token
	verification, err := ah.db.CreateEmailVerification(user.ID, user.Email)
	if err != nil {
		utils.LogError("Failed to create email verification: %v", err)
		http.Error(w, "Failed to create verification token", http.StatusInternalServerError)
		return
	}

	// Build email content
	subject, body := ah.emailService.BuildVerificationEmail(user, verification.Token)

	// Queue verification email
	if err := ah.jobManager.QueueVerificationEmail(user.Email, subject, body, user.ID, verification.Token); err != nil {
		utils.LogError("Failed to queue verification email: %v", err)
		http.Error(w, "Failed to queue verification email", http.StatusInternalServerError)
		return
	}

	utils.LogHTTP("Verification email resent to user: %s (ID: %d)", user.Username, user.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Verification email sent successfully",
	})
}

func (ah *AuthHandlers) HandleUsers(w http.ResponseWriter, r *http.Request) {
	utils.LogHTTP("%s /users", r.Method)
	switch r.Method {
	case http.MethodGet:
		ah.getUsers(w, r)
	case http.MethodPost:
		ah.createUserByAdmin(w, r)
	default:
		utils.LogHTTP("Method %s not allowed for /users", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ah *AuthHandlers) HandleUserByID(w http.ResponseWriter, r *http.Request, id int) {
	utils.LogHTTP("%s /users/%d", r.Method, id)
	switch r.Method {
	case http.MethodGet:
		ah.getUserByID(w, r, id)
	case http.MethodPut:
		ah.updateUserByAdmin(w, r, id)
	case http.MethodDelete:
		ah.deleteUserByAdmin(w, r, id)
	default:
		utils.LogHTTP("Method %s not allowed for /users/%d", r.Method, id)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ah *AuthHandlers) getUsers(w http.ResponseWriter, r *http.Request) {
	users, err := ah.db.GetAllUsers()
	if err != nil {
		utils.LogError("Failed to fetch users: %v", err)
		http.Error(w, "Failed to fetch users", http.StatusInternalServerError)
		return
	}

	utils.LogHTTP("Returning %d users", len(users))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"users": users,
	})
}

func (ah *AuthHandlers) getUserByID(w http.ResponseWriter, r *http.Request, id int) {
	session := getSessionFromRequest(r, ah.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Users can only see their own info unless they're admin
	if session.UserID != id && session.Role != "admin" {
		http.Error(w, "Insufficient permissions", http.StatusForbidden)
		return
	}

	user, err := ah.db.GetUserByID(id)
	if err != nil {
		utils.LogHTTP("User ID %d not found: %v", id, err)
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	utils.LogHTTP("Returning user ID %d", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (ah *AuthHandlers) createUserByAdmin(w http.ResponseWriter, r *http.Request) {
	var req models.UserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogHTTP("Invalid JSON in create user request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	user, err := ah.db.CreateUser(req)
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
			utils.LogError("Failed to create user: %v", err)
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
		}
		return
	}

	utils.LogHTTP("Created user ID %d by admin", user.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

func (ah *AuthHandlers) updateUserByAdmin(w http.ResponseWriter, r *http.Request, id int) {
	var req models.UserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogHTTP("Invalid JSON in update user request for ID %d: %v", id, err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	user, err := ah.db.UpdateUser(id, req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "User not found", http.StatusNotFound)
		} else {
			utils.LogError("Failed to update user ID %d: %v", id, err)
			http.Error(w, "Failed to update user", http.StatusInternalServerError)
		}
		return
	}

	// If user was deactivated, kill their sessions
	if req.IsActive != nil && !*req.IsActive {
		ah.sessionStore.DeleteUserSessions(id)
		utils.LogHTTP("Deleted sessions for deactivated user ID %d", id)
	}

	utils.LogHTTP("Updated user ID %d by admin", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (ah *AuthHandlers) deleteUserByAdmin(w http.ResponseWriter, r *http.Request, id int) {
	session := getSessionFromRequest(r, ah.sessionStore)
	if session.UserID == id {
		http.Error(w, "Cannot delete your own account", http.StatusBadRequest)
		return
	}

	err := ah.db.DeleteUser(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "User not found", http.StatusNotFound)
		} else {
			utils.LogError("Failed to delete user ID %d: %v", id, err)
			http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		}
		return
	}

	// Kill user's sessions
	ah.sessionStore.DeleteUserSessions(id)

	utils.LogHTTP("Deleted user ID %d by admin", id)
	w.WriteHeader(http.StatusNoContent)
}

func getSessionFromRequest(r *http.Request, sessionStore *auth.SessionStore) *models.Session {
	sessionID := extractSessionFromRequest(r)
	if sessionID == "" {
		return nil
	}

	session, exists := sessionStore.GetSession(sessionID)
	if !exists {
		return nil
	}

	return session
}
