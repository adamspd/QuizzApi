package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/adamspd/QuizzApi/models"
	"github.com/adamspd/QuizzApi/utils"
)

func (db *DB) CreateUser(req models.UserRequest) (*models.User, error) {
	utils.LogDB("Creating user: %s (%s)", req.Username, req.Email)
	start := time.Now()

	// Validate the request
	if err := utils.ValidateUserRequest(&req, false); err != nil {
		return nil, err
	}

	// Hash the password
	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		utils.LogError("Failed to hash password: %v", err)
		return nil, err
	}

	// Set default role if not specified
	role := req.Role
	if role == "" {
		role = "user"
	}

	// Set default active status
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	result, err := db.Exec(`
		INSERT INTO users (username, email, password_hash, role, is_active)
		VALUES (?, ?, ?, ?, ?)
	`, req.Username, req.Email, hashedPassword, role, isActive)

	if err != nil {
		duration := time.Since(start)
		utils.LogError("CreateUser failed: %v (%v)", err, duration)
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		utils.LogError("Failed to get LastInsertId for user: %v", err)
		return nil, err
	}

	duration := time.Since(start)
	utils.LogDB("User created with ID %d in %v", id, duration)

	return db.GetUserByID(int(id))
}

func (db *DB) GetUserByID(id int) (*models.User, error) {
	utils.LogDB("Getting user by ID: %d", id)

	var user models.User
	err := db.QueryRow(`
		SELECT id, username, email, role, is_active, email_verified, email_verified_at, created_at, updated_at
		FROM users WHERE id = ?
	`, id).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.IsActive,
		&user.EmailVerified, &user.EmailVerifiedAt, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			utils.LogDB("User ID %d not found", id)
		} else {
			utils.LogError("GetUserByID(%d) failed: %v", id, err)
		}
		return nil, err
	}

	return &user, nil
}

func (db *DB) GetUserByUsername(username string) (*models.User, error) {
	utils.LogDB("Getting user by username: %s", username)

	var user models.User
	err := db.QueryRow(`
		SELECT id, username, email, role, is_active, email_verified, email_verified_at, created_at, updated_at
		FROM users WHERE username = ?
	`, username).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.IsActive,
		&user.EmailVerified, &user.EmailVerifiedAt, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			utils.LogDB("User %s not found", username)
		} else {
			utils.LogError("GetUserByUsername(%s) failed: %v", username, err)
		}
		return nil, err
	}

	return &user, nil
}

func (db *DB) GetUserByEmail(email string) (*models.User, error) {
	utils.LogDB("Getting user by email: %s", email)

	var user models.User
	err := db.QueryRow(`
		SELECT id, username, email, role, is_active, email_verified, email_verified_at, created_at, updated_at
		FROM users WHERE email = ?
	`, email).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.IsActive,
		&user.EmailVerified, &user.EmailVerifiedAt, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			utils.LogDB("User with email %s not found", email)
		} else {
			utils.LogError("GetUserByEmail(%s) failed: %v", email, err)
		}
		return nil, err
	}

	return &user, nil
}

func (db *DB) AuthenticateUser(username, password string) (*models.User, error) {
	utils.LogDB("Authenticating user: %s", username)

	var user models.User
	var passwordHash string

	err := db.QueryRow(`
		SELECT id, username, email, role, is_active, email_verified, email_verified_at, 
		       created_at, updated_at, password_hash
		FROM users WHERE username = ? AND is_active = 1
	`, username).Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.IsActive,
		&user.EmailVerified, &user.EmailVerifiedAt, &user.CreatedAt, &user.UpdatedAt, &passwordHash)

	if err != nil {
		if err == sql.ErrNoRows {
			utils.LogDB("Authentication failed: user %s not found or inactive", username)
		} else {
			utils.LogError("AuthenticateUser(%s) failed: %v", username, err)
		}
		return nil, fmt.Errorf("invalid credentials")
	}

	// Check password
	if !utils.CheckPassword(passwordHash, password) {
		utils.LogDB("Authentication failed: invalid password for user %s", username)
		return nil, fmt.Errorf("invalid credentials")
	}

	utils.LogDB("User %s authenticated successfully", username)
	return &user, nil
}

func (db *DB) UpdateUser(id int, req models.UserRequest) (*models.User, error) {
	utils.LogDB("Updating user ID %d", id)
	start := time.Now()

	// Validate the request
	if err := utils.ValidateUserRequest(&req, true); err != nil {
		return nil, err
	}

	// Check if user exists
	currentUser, err := db.GetUserByID(id)
	if err != nil {
		return nil, err
	}

	// Build update query dynamically
	var setParts []string
	var args []interface{}

	if req.Username != "" && req.Username != currentUser.Username {
		setParts = append(setParts, "username = ?")
		args = append(args, req.Username)
	}

	if req.Email != "" && req.Email != currentUser.Email {
		setParts = append(setParts, "email = ?, email_verified = 0, email_verified_at = NULL")
		args = append(args, req.Email)
	}

	if req.Password != "" {
		hashedPassword, err := utils.HashPassword(req.Password)
		if err != nil {
			return nil, err
		}
		setParts = append(setParts, "password_hash = ?")
		args = append(args, hashedPassword)
	}

	if req.Role != "" && req.Role != currentUser.Role {
		setParts = append(setParts, "role = ?")
		args = append(args, req.Role)
	}

	if req.IsActive != nil && *req.IsActive != currentUser.IsActive {
		setParts = append(setParts, "is_active = ?")
		args = append(args, *req.IsActive)
	}

	if len(setParts) == 0 {
		utils.LogDB("UpdateUser(%d): no changes to apply", id)
		return currentUser, nil
	}

	setParts = append(setParts, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)

	query := fmt.Sprintf("UPDATE users SET %s WHERE id = ?", strings.Join(setParts, ", "))

	result, err := db.Exec(query, args...)
	if err != nil {
		duration := time.Since(start)
		utils.LogError("UpdateUser(%d) failed: %v (%v)", id, err, duration)
		return nil, err
	}

	rowsAffected, _ := result.RowsAffected()
	duration := time.Since(start)

	if rowsAffected == 0 {
		utils.LogDB("UpdateUser(%d): no rows affected (%v)", id, duration)
	} else {
		utils.LogDB("UpdateUser(%d) completed in %v", id, duration)
	}

	return db.GetUserByID(id)
}

func (db *DB) DeleteUser(id int) error {
	utils.LogDB("Deleting user ID %d", id)
	start := time.Now()

	result, err := db.Exec("UPDATE users SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	if err != nil {
		duration := time.Since(start)
		utils.LogError("Failed to delete user %d: %v (%v)", id, err, duration)
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	duration := time.Since(start)

	if rowsAffected == 0 {
		utils.LogDB("DeleteUser(%d): no rows affected (%v)", id, duration)
		return fmt.Errorf("user not found")
	} else {
		utils.LogDB("DeleteUser(%d) completed in %v", id, duration)
	}

	return nil
}

func (db *DB) DeleteUserPermanently(id int) error {
	utils.LogDB("Permanently deleting user ID %d", id)
	start := time.Now()

	// Start a transaction to delete user and related data
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete related data first (foreign key constraints)
	// Delete user preferences
	_, err = tx.Exec("DELETE FROM user_preferences WHERE user_id = ?", id)
	if err != nil {
		utils.LogError("Failed to delete user preferences for user %d: %v", id, err)
		return err
	}

	// Delete progress records
	_, err = tx.Exec("DELETE FROM progress WHERE user_id = ?", id)
	if err != nil {
		utils.LogError("Failed to delete progress records for user %d: %v", id, err)
		return err
	}

	// Delete email verifications
	_, err = tx.Exec("DELETE FROM email_verifications WHERE user_id = ?", id)
	if err != nil {
		utils.LogError("Failed to delete email verifications for user %d: %v", id, err)
		return err
	}

	// Finally delete the user
	result, err := tx.Exec("DELETE FROM users WHERE id = ?", id)
	if err != nil {
		utils.LogError("Failed to permanently delete user %d: %v", id, err)
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	if err = tx.Commit(); err != nil {
		utils.LogError("Failed to commit user deletion transaction: %v", err)
		return err
	}

	duration := time.Since(start)
	utils.LogDB("User %d permanently deleted in %v", id, duration)
	return nil
}

func (db *DB) GetAllUsers() ([]models.User, error) {
	utils.LogDB("Getting all users")
	start := time.Now()

	rows, err := db.Query(`
		SELECT id, username, email, role, is_active, email_verified, email_verified_at, created_at, updated_at
		FROM users ORDER BY created_at DESC
	`)
	if err != nil {
		utils.LogError("GetAllUsers query failed: %v", err)
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(&user.ID, &user.Username, &user.Email, &user.Role, &user.IsActive,
			&user.EmailVerified, &user.EmailVerifiedAt, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			utils.LogError("Failed to scan user row: %v", err)
			return nil, err
		}
		users = append(users, user)
	}

	duration := time.Since(start)
	utils.LogDB("GetAllUsers completed: %d users in %v", len(users), duration)
	return users, nil
}

// Email verification functions
func (db *DB) CreateEmailVerification(userID int, email string) (*models.EmailVerification, error) {
	utils.LogDB("Creating email verification for user %d", userID)

	// Delete any existing unverified tokens for this user
	_, err := db.Exec("DELETE FROM email_verifications WHERE user_id = ? AND used_at IS NULL", userID)
	if err != nil {
		utils.LogError("Failed to clean up old verification tokens: %v", err)
		return nil, err
	}

	token := utils.GenerateVerificationToken()
	expiresAt := time.Now().Add(24 * time.Hour) // Tokens valid for 24 hours

	result, err := db.Exec(`
		INSERT INTO email_verifications (user_id, email, token, created_at, expires_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?)
	`, userID, email, token, expiresAt)

	if err != nil {
		utils.LogError("Failed to create email verification: %v", err)
		return nil, err
	}

	id, _ := result.LastInsertId()

	verification := &models.EmailVerification{
		ID:        int(id),
		UserID:    userID,
		Email:     email,
		Token:     token,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}

	utils.LogDB("Email verification created with token: %s", token[:8]+"...")
	return verification, nil
}

func (db *DB) VerifyEmailToken(token string) (*models.User, error) {
	utils.LogDB("Verifying email token: %s", token[:8]+"...")

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Get verification record
	var verification models.EmailVerification
	err = tx.QueryRow(`
		SELECT id, user_id, email, created_at, expires_at, used_at
		FROM email_verifications 
		WHERE token = ? AND used_at IS NULL
	`, token).Scan(&verification.ID, &verification.UserID, &verification.Email,
		&verification.CreatedAt, &verification.ExpiresAt, &verification.UsedAt)

	if err != nil {
		utils.LogDB("Email verification token not found or already used: %s", token[:8]+"...")
		return nil, fmt.Errorf("invalid or expired verification token")
	}

	// Check if token is expired
	if time.Now().After(verification.ExpiresAt) {
		utils.LogDB("Email verification token expired: %s", token[:8]+"...")
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
	var user models.User
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

	utils.LogDB("Email verified for user %d (%s)", user.ID, user.Username)
	return &user, nil
}

func (db *DB) IsUserInGracePeriod(userID int, gracePeriod time.Duration) (bool, error) {
	var createdAt time.Time
	err := db.QueryRow("SELECT created_at FROM users WHERE id = ?", userID).Scan(&createdAt)
	if err != nil {
		return false, err
	}

	graceEndsAt := createdAt.Add(gracePeriod)
	return time.Now().Before(graceEndsAt), nil
}
