package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"time"
)

// EmailVerification represents a pending email verification
type EmailVerification struct {
	ID        int        `json:"id"`
	UserID    int        `json:"user_id"`
	Email     string     `json:"email"`
	Token     string     `json:"token"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

// EmailConfig holds SMTP configuration
type EmailConfig struct {
	SMTPHost    string
	SMTPPort    int
	Username    string
	Password    string
	FromAddress string
	FromName    string
	BaseURL     string // For verification links
	GracePeriod time.Duration
}

// LoadEmailConfig loads email configuration from environment
func LoadEmailConfig() *EmailConfig {
	gracePeriodHours, _ := strconv.Atoi(getEnvOrDefault("EMAIL_GRACE_PERIOD_HOURS", "2"))

	return &EmailConfig{
		SMTPHost:    getEnvOrDefault("SMTP_HOST", "mail.adamspierredavid.com"),
		SMTPPort:    getEnvInt("SMTP_PORT", 465),
		Username:    getEnvOrDefault("SMTP_USERNAME", ""),
		Password:    getEnvOrDefault("SMTP_PASSWORD", ""),
		FromAddress: getEnvOrDefault("FROM_EMAIL", "noreply@adamspierredavid.com"),
		FromName:    getEnvOrDefault("FROM_NAME", "French Citizenship Training"),
		BaseURL:     getEnvOrDefault("BASE_URL", "http://localhost:8043"),
		GracePeriod: time.Duration(gracePeriodHours) * time.Hour,
	}
}

// Helper functions for environment variables
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// generateVerificationToken creates a secure random token
func generateVerificationToken() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback - should never happen but better safe than sorry
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	return hex.EncodeToString(bytes)
}

// createEmailVerification creates a new verification token for a user
func (db *DB) createEmailVerification(userID int, email string) (*EmailVerification, error) {
	logDB("Creating email verification for user %d", userID)

	// Delete any existing unverified tokens for this user
	_, err := db.Exec("DELETE FROM email_verifications WHERE user_id = ? AND used_at IS NULL", userID)
	if err != nil {
		logError("Failed to clean up old verification tokens: %v", err)
		return nil, err
	}

	token := generateVerificationToken()
	expiresAt := time.Now().Add(24 * time.Hour) // Tokens valid for 24 hours

	result, err := db.Exec(`
		INSERT INTO email_verifications (user_id, email, token, created_at, expires_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?)
	`, userID, email, token, expiresAt)

	if err != nil {
		logError("Failed to create email verification: %v", err)
		return nil, err
	}

	id, _ := result.LastInsertId()

	verification := &EmailVerification{
		ID:        int(id),
		UserID:    userID,
		Email:     email,
		Token:     token,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}

	logDB("Email verification created with token: %s", token[:8]+"...")
	return verification, nil
}

// verifyEmailToken verifies a token and marks user as verified
func (db *DB) verifyEmailToken(token string) (*User, error) {
	logDB("Verifying email token: %s", token[:8]+"...")

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Get verification record
	var verification EmailVerification
	err = tx.QueryRow(`
		SELECT id, user_id, email, created_at, expires_at, used_at
		FROM email_verifications 
		WHERE token = ? AND used_at IS NULL
	`, token).Scan(&verification.ID, &verification.UserID, &verification.Email,
		&verification.CreatedAt, &verification.ExpiresAt, &verification.UsedAt)

	if err != nil {
		logDB("Email verification token not found or already used: %s", token[:8]+"...")
		return nil, fmt.Errorf("invalid or expired verification token")
	}

	// Check if token is expired
	if time.Now().After(verification.ExpiresAt) {
		logDB("Email verification token expired: %s", token[:8]+"...")
		return nil, fmt.Errorf("verification token has expired")
	}

	// Mark token as used
	_, err = tx.Exec(`
		UPDATE email_verifications 
		SET used_at = CURRENT_TIMESTAMP 
		WHERE id = ?
	`, verification.ID)
	if err != nil {
		return nil, err
	}

	// Mark user as verified
	_, err = tx.Exec(`
		UPDATE users 
		SET email_verified = 1, email_verified_at = CURRENT_TIMESTAMP 
		WHERE id = ?
	`, verification.UserID)
	if err != nil {
		return nil, err
	}

	// Get updated user
	var user User
	err = tx.QueryRow(`
		SELECT id, username, email, role, is_active, email_verified, created_at, updated_at
		FROM users WHERE id = ?
	`, verification.UserID).Scan(&user.ID, &user.Username, &user.Email, &user.Role,
		&user.IsActive, &user.EmailVerified, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	logDB("Email verified for user %d (%s)", user.ID, user.Username)
	return &user, nil
}

// isUserInGracePeriod checks if user is still within the verification grace period
func (db *DB) isUserInGracePeriod(userID int, gracePeriod time.Duration) (bool, error) {
	var createdAt time.Time
	err := db.QueryRow("SELECT created_at FROM users WHERE id = ?", userID).Scan(&createdAt)
	if err != nil {
		return false, err
	}

	graceEndsAt := createdAt.Add(gracePeriod)
	return time.Now().Before(graceEndsAt), nil
}

// EmailService handles email sending
type EmailService struct {
	config *EmailConfig
}

// NewEmailService creates a new email service
func NewEmailService(config *EmailConfig) *EmailService {
	return &EmailService{config: config}
}

// sendVerificationEmail sends an email verification link
func (es *EmailService) sendVerificationEmail(user *User, token string) error {
	if es.config.Username == "" || es.config.Password == "" {
		logInfo("SMTP not configured, logging verification token instead")
		logInfo("=== EMAIL VERIFICATION ===")
		logInfo("To: %s", user.Email)
		logInfo("Verification URL: %s/verify-email?token=%s", es.config.BaseURL, token)
		logInfo("==========================")
		return nil
	}

	verificationURL := fmt.Sprintf("%s/verify-email?token=%s", es.config.BaseURL, url.QueryEscape(token))

	subject := "Verify your email address"
	body := fmt.Sprintf(`Hello %s,

Thank you for registering for French Citizenship Training!

Please click the link below to verify your email address:
%s

You have 2 hours to use the application before verification is required.
After that, your account will be temporarily disabled until you verify your email.

This verification link will expire in 24 hours.

If you didn't create this account, please ignore this email.

Best regards,
French Citizenship Training Team`, user.Username, verificationURL)

	return es.sendEmail(user.Email, subject, body)
}

// sendEmail sends an email using SMTP
func (es *EmailService) sendEmail(to, subject, body string) error {
	logInfo("Sending email to %s: %s", to, subject)

	// Prepare message
	message := fmt.Sprintf("From: %s <%s>\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s\r\n", es.config.FromName, es.config.FromAddress, to, subject, body)

	// SMTP authentication
	auth := smtp.PlainAuth("", es.config.Username, es.config.Password, es.config.SMTPHost)

	// Send email
	addr := fmt.Sprintf("%s:%d", es.config.SMTPHost, es.config.SMTPPort)
	err := smtp.SendMail(addr, auth, es.config.FromAddress, []string{to}, []byte(message))

	if err != nil {
		logError("Failed to send email to %s: %v", to, err)
		return err
	}

	logInfo("Email sent successfully to %s", to)
	return nil
}

// Enhanced auth middleware that checks email verification during grace period
func (api *API) authMiddlewareWithEmailCheck(next http.HandlerFunc) http.HandlerFunc {
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

		// Check if user needs email verification
		user, err := api.db.GetUserByID(session.UserID)
		if err != nil {
			logError("Failed to get user for email verification check: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if !user.EmailVerified {
			inGracePeriod, err := api.db.isUserInGracePeriod(user.ID, api.emailConfig.GracePeriod)
			if err != nil {
				logError("Failed to check grace period: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			if !inGracePeriod {
				logInfo("User %s (%d) blocked - email not verified and grace period expired", user.Username, user.ID)
				http.Error(w, "Email verification required. Please check your email and verify your account.", http.StatusForbidden)
				return
			}
		}

		// Add session to request context
		ctx := context.WithValue(r.Context(), sessionContextKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}
