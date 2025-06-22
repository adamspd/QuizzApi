package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/adamspd/QuizzApi/models"
	"github.com/adamspd/QuizzApi/utils"
)

func (db *DB) GetUserPreferences(userID int) (*models.UserPreferences, error) {
	utils.LogDB("Getting preferences for user %d", userID)

	var prefs models.UserPreferences
	var categoryJSON sql.NullString

	err := db.QueryRow(`
		SELECT user_id, practice_session_length, difficulty_preference, category_preference,
		       review_mode, auto_advance_timing_open, auto_advance_timing_choice,
		       question_randomization, skip_answered_questions, focus_weak_areas,
		       theme_mode, stats_visibility, interface_language, updated_at
		FROM user_preferences WHERE user_id = ?
	`, userID).Scan(
		&prefs.UserID, &prefs.PracticeSessionLength, &prefs.DifficultyPreference, &categoryJSON,
		&prefs.ReviewMode, &prefs.AutoAdvanceTimingOpen, &prefs.AutoAdvanceTimingChoice,
		&prefs.QuestionRandomization, &prefs.SkipAnsweredQuestions, &prefs.FocusWeakAreas,
		&prefs.ThemeMode, &prefs.StatsVisibility, &prefs.InterfaceLanguage, &prefs.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		// Create default preferences for new user
		utils.LogDB("No preferences found for user %d, creating defaults", userID)
		return db.CreateDefaultPreferences(userID)
	}

	if err != nil {
		utils.LogError("Failed to get preferences for user %d: %v", userID, err)
		return nil, err
	}

	// Parse category JSON properly
	if categoryJSON.Valid && categoryJSON.String != "" {
		var categories []string
		if err := json.Unmarshal([]byte(categoryJSON.String), &categories); err != nil {
			utils.LogError("Failed to parse category JSON for user %d: %v", userID, err)
			prefs.CategoryPreference = nil
		} else {
			prefs.CategoryPreference = categories
		}
	} else {
		prefs.CategoryPreference = nil // Empty means all categories
	}

	return &prefs, nil
}

func (db *DB) CreateDefaultPreferences(userID int) (*models.UserPreferences, error) {
	utils.LogDB("Creating default preferences for user %d", userID)

	defaults := models.GetDefaultPreferences(userID)

	_, err := db.Exec(`
		INSERT INTO user_preferences (
			user_id, practice_session_length, difficulty_preference, category_preference,
			review_mode, auto_advance_timing_open, auto_advance_timing_choice,
			question_randomization, skip_answered_questions, focus_weak_areas,
			theme_mode, stats_visibility, interface_language, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, userID, defaults.PracticeSessionLength, defaults.DifficultyPreference, nil, // NULL for all categories
		defaults.ReviewMode, defaults.AutoAdvanceTimingOpen, defaults.AutoAdvanceTimingChoice,
		defaults.QuestionRandomization, defaults.SkipAnsweredQuestions, defaults.FocusWeakAreas,
		defaults.ThemeMode, defaults.StatsVisibility, defaults.InterfaceLanguage)

	if err != nil {
		utils.LogError("Failed to create default preferences for user %d: %v", userID, err)
		return nil, err
	}

	return defaults, nil
}

func (db *DB) UpdateUserPreferences(userID int, req models.UserPreferencesRequest) (*models.UserPreferences, error) {
	utils.LogDB("Updating preferences for user %d", userID)
	start := time.Now()

	// Get current preferences
	current, err := db.GetUserPreferences(userID)
	if err != nil {
		return nil, err
	}

	// Build update query dynamically
	var setParts []string
	var args []interface{}

	if req.PracticeSessionLength != nil {
		setParts = append(setParts, "practice_session_length = ?")
		args = append(args, *req.PracticeSessionLength)
	}

	if req.DifficultyPreference != nil {
		setParts = append(setParts, "difficulty_preference = ?")
		args = append(args, *req.DifficultyPreference)
	}

	// Handle category preference properly
	if req.CategoryPreference != nil {
		if len(*req.CategoryPreference) == 0 {
			// Empty array means all categories (NULL in database)
			setParts = append(setParts, "category_preference = NULL")
		} else {
			// Convert to JSON string for database storage
			categoryJSON, err := json.Marshal(req.CategoryPreference)
			if err != nil {
				utils.LogError("Failed to marshal categories: %v", err)
				return nil, fmt.Errorf("invalid category preference")
			}
			setParts = append(setParts, "category_preference = ?")
			args = append(args, string(categoryJSON))
		}
	}

	if req.ReviewMode != nil {
		setParts = append(setParts, "review_mode = ?")
		args = append(args, *req.ReviewMode)
	}

	if req.AutoAdvanceTimingOpen != nil {
		setParts = append(setParts, "auto_advance_timing_open = ?")
		args = append(args, *req.AutoAdvanceTimingOpen)
	}

	if req.AutoAdvanceTimingChoice != nil {
		setParts = append(setParts, "auto_advance_timing_choice = ?")
		args = append(args, *req.AutoAdvanceTimingChoice)
	}

	if req.QuestionRandomization != nil {
		setParts = append(setParts, "question_randomization = ?")
		args = append(args, *req.QuestionRandomization)
	}

	if req.SkipAnsweredQuestions != nil {
		setParts = append(setParts, "skip_answered_questions = ?")
		args = append(args, *req.SkipAnsweredQuestions)
	}

	if req.FocusWeakAreas != nil {
		setParts = append(setParts, "focus_weak_areas = ?")
		args = append(args, *req.FocusWeakAreas)
	}

	if req.ThemeMode != nil {
		setParts = append(setParts, "theme_mode = ?")
		args = append(args, *req.ThemeMode)
	}

	if req.StatsVisibility != nil {
		setParts = append(setParts, "stats_visibility = ?")
		args = append(args, *req.StatsVisibility)
	}

	if req.InterfaceLanguage != nil {
		setParts = append(setParts, "interface_language = ?")
		args = append(args, *req.InterfaceLanguage)
	}

	if len(setParts) == 0 {
		utils.LogDB("No preferences to update for user %d", userID)
		return current, nil
	}

	// Add updated_at and user_id to query
	setParts = append(setParts, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, userID)

	query := fmt.Sprintf("UPDATE user_preferences SET %s WHERE user_id = ?", strings.Join(setParts, ", "))

	result, err := db.Exec(query, args...)
	if err != nil {
		duration := time.Since(start)
		utils.LogError("Failed to update preferences for user %d: %v (%v)", userID, err, duration)
		return nil, err
	}

	rowsAffected, _ := result.RowsAffected()
	duration := time.Since(start)
	utils.LogDB("Updated preferences for user %d: %d rows affected (%v)", userID, rowsAffected, duration)

	// Return updated preferences
	return db.GetUserPreferences(userID)
}
