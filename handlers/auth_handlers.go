package handlers

import (
	"encoding/json"
	"github.com/adamspd/QuizzApi/jobs"
	"net/http"
	"strconv"
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
	case strings.HasPrefix(path, "resend-verification/") && r.Method == http.MethodPost:
		ah.resendVerificationForUser(w, r)
	case strings.HasPrefix(path, "force-password-change/") && r.Method == http.MethodPost:
		ah.forcePasswordChange(w, r)
	case path == "delete-self" && r.Method == http.MethodPost:
		ah.deleteSelf(w, r)
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

func (ah *AuthHandlers) forcePasswordChange(w http.ResponseWriter, r *http.Request) {
	session := getSessionFromRequest(r, ah.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Only admin and moderator can force password changes
	if session.Role != "admin" && session.Role != "moderator" {
		http.Error(w, "Insufficient permissions", http.StatusForbidden)
		return
	}

	// Extract user ID from path
	path := strings.TrimPrefix(r.URL.Path, "/auth/force-password-change/")
	userID, err := strconv.Atoi(path)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	var req struct {
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.NewPassword == "" {
		http.Error(w, "New password is required", http.StatusBadRequest)
		return
	}

	user, err := ah.db.GetUserByID(userID)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Moderators cannot change admin passwords
	if session.Role == "moderator" && user.Role == "admin" {
		http.Error(w, "Insufficient permissions", http.StatusForbidden)
		return
	}

	// Admin cannot change their own password this way (security measure)
	if session.UserID == userID {
		http.Error(w, "Cannot force change your own password", http.StatusBadRequest)
		return
	}

	// Update the password
	userReq := models.UserRequest{
		Password: req.NewPassword,
	}

	_, err = ah.db.UpdateUser(userID, userReq)
	if err != nil {
		utils.LogError("Failed to update user password: %v", err)
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
		return
	}

	// Kill all sessions for this user (force re-login)
	ah.sessionStore.DeleteUserSessions(userID)

	utils.LogHTTP("Password changed for user %s (ID: %d) by %s", user.Username, user.ID, session.Username)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Password changed successfully. User will need to log in again.",
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
	utils.LogHTTP("GET /verify-email")

	token := r.URL.Query().Get("token")
	if token == "" {
		http.ServeFile(w, r, "static/verify-error.html")
		return
	}

	user, err := ah.db.VerifyEmailToken(token)
	if err != nil {
		utils.LogHTTP("Email verification failed: %v", err)
		http.ServeFile(w, r, "static/verify-error.html")
		return
	}

	utils.LogHTTP("Email verified successfully for user: %s (ID: %d)", user.Username, user.ID)
	http.ServeFile(w, r, "static/verify-email.html")
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

	// Create a new verification token
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

func (ah *AuthHandlers) resendVerificationForUser(w http.ResponseWriter, r *http.Request) {
	session := getSessionFromRequest(r, ah.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Only admin and moderator can resend verification for others
	if session.Role != "admin" && session.Role != "moderator" {
		http.Error(w, "Insufficient permissions", http.StatusForbidden)
		return
	}

	// Extract user ID from path
	path := strings.TrimPrefix(r.URL.Path, "/auth/resend-verification/")
	userID, err := strconv.Atoi(path)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	user, err := ah.db.GetUserByID(userID)
	if err != nil {
		utils.LogError("Failed to get user for verification resend: %v", err)
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Moderators cannot resend verification for admin users
	if session.Role == "moderator" && user.Role == "admin" {
		http.Error(w, "Insufficient permissions", http.StatusForbidden)
		return
	}

	if user.EmailVerified {
		http.Error(w, "Email already verified", http.StatusBadRequest)
		return
	}

	// Create a new verification token
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

	utils.LogHTTP("Verification email resent for user %s (ID: %d) by %s", user.Username, user.ID, session.Username)

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
	session := getSessionFromRequest(r, ah.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	users, err := ah.db.GetAllUsers()
	if err != nil {
		utils.LogError("Failed to fetch users: %v", err)
		http.Error(w, "Failed to fetch users", http.StatusInternalServerError)
		return
	}

	// If moderator, filter out admin users
	if session.Role == "moderator" {
		var filteredUsers []models.User
		for _, user := range users {
			if user.Role != "admin" {
				filteredUsers = append(filteredUsers, user)
			}
		}
		users = filteredUsers
	}

	utils.LogHTTP("Returning %d users to %s", len(users), session.Role)
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

	user, err := ah.db.GetUserByID(id)
	if err != nil {
		utils.LogHTTP("User ID %d not found: %v", id, err)
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Check permissions
	if session.UserID == id {
		// Users can always see their own info
	} else if session.Role == "admin" {
		// Admins can see anyone
	} else if session.Role == "moderator" && user.Role != "admin" {
		// Moderators can see non-admin users
	} else {
		// All other cases are forbidden
		http.Error(w, "Insufficient permissions", http.StatusForbidden)
		return
	}

	utils.LogHTTP("Returning user ID %d to %s", id, session.Username)
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
	session := getSessionFromRequest(r, ah.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Get the user being edited
	targetUser, err := ah.db.GetUserByID(id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	var req models.UserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogHTTP("Invalid JSON in update user request for ID %d: %v", id, err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Non-admins cannot change role or active status
	if session.Role != "admin" {
		if req.Role != "" && req.Role != targetUser.Role {
			http.Error(w, "Insufficient permissions to change user role", http.StatusForbidden)
			return
		}
		if req.IsActive != nil && *req.IsActive != targetUser.IsActive {
			http.Error(w, "Insufficient permissions to change user active status", http.StatusForbidden)
			return
		}
	}

	// Moderators cannot edit admin users (except themselves for basic info)
	if session.Role == "moderator" && targetUser.Role == "admin" && session.UserID != id {
		http.Error(w, "Moderators cannot edit admin users", http.StatusForbidden)
		return
	}

	utils.LogHTTP("Updating user with request: %+v", req)

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

	// If user was deactivated by admin, kill their sessions
	if req.IsActive != nil && !*req.IsActive && session.UserID != id {
		ah.sessionStore.DeleteUserSessions(id)
		utils.LogHTTP("Deleted sessions for deactivated user ID %d", id)
	}

	utils.LogHTTP("Updated user ID %d by %s", id, session.Username)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (ah *AuthHandlers) deleteUserByAdmin(w http.ResponseWriter, r *http.Request, id int) {
	session := getSessionFromRequest(r, ah.sessionStore)
	if session.UserID == id {
		http.Error(w, "Cannot delete your own account", http.StatusBadRequest)
		return
	}

	// Check if the user exists and get their info
	user, err := ah.db.GetUserByID(id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Moderators cannot delete admin users
	if session.Role == "moderator" && user.Role == "admin" {
		http.Error(w, "Insufficient permissions", http.StatusForbidden)
		return
	}

	// Only admins can permanently delete users
	if session.Role != "admin" {
		// Moderators can only deactivate
		err := ah.db.DeleteUser(id) // This just deactivates
		if err != nil {
			utils.LogError("Failed to deactivate user ID %d: %v", id, err)
			http.Error(w, "Failed to deactivate user", http.StatusInternalServerError)
			return
		}

		// Kill user's sessions
		ah.sessionStore.DeleteUserSessions(id)

		utils.LogHTTP("User ID %d deactivated by moderator %s", id, session.Username)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// For admins, check if they want permanent deletion or just deactivation
	permanent := r.URL.Query().Get("permanent")

	if permanent == "true" {
		// Permanent deletion
		err := ah.db.DeleteUserPermanently(id)
		if err != nil {
			utils.LogError("Failed to permanently delete user ID %d: %v", id, err)
			http.Error(w, "Failed to permanently delete user", http.StatusInternalServerError)
			return
		}
		utils.LogHTTP("User ID %d permanently deleted by admin %s", id, session.Username)
	} else {
		// Just deactivate
		err := ah.db.DeleteUser(id)
		if err != nil {
			utils.LogError("Failed to deactivate user ID %d: %v", id, err)
			http.Error(w, "Failed to deactivate user", http.StatusInternalServerError)
			return
		}
		utils.LogHTTP("User ID %d deactivated by admin %s", id, session.Username)
	}

	// Kill user's sessions in both cases
	ah.sessionStore.DeleteUserSessions(id)

	w.WriteHeader(http.StatusNoContent)
}

func (ah *AuthHandlers) deleteSelf(w http.ResponseWriter, r *http.Request) {
	session := getSessionFromRequest(r, ah.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		Permanent bool `json:"permanent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// If no "body" or invalid JSON, default to deactivation
		req.Permanent = false
	}

	// TODO: Add immediate permanent deletion for users when questions/author relationship is handled
	// For now, both deactivation and permanent deletion requests result in deactivation
	// to preserve data integrity with questions table

	err := ah.db.DeleteUser(session.UserID) // This just deactivates
	if err != nil {
		utils.LogError("Failed to deactivate user ID %d (self-deletion): %v", session.UserID, err)
		http.Error(w, "Failed to deactivate account", http.StatusInternalServerError)
		return
	}

	// Kill all sessions for this user
	ah.sessionStore.DeleteUserSessions(session.UserID)

	if req.Permanent {
		utils.LogHTTP("User ID %d requested permanent self-deletion (deactivated for now)", session.UserID)
	} else {
		utils.LogHTTP("User ID %d requested self-deactivation", session.UserID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Account deactivated successfully",
	})
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
