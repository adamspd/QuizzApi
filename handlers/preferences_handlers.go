package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/adamspd/QuizzApi/auth"
	"github.com/adamspd/QuizzApi/db"
	"github.com/adamspd/QuizzApi/models"
	"github.com/adamspd/QuizzApi/utils"
)

type PreferencesHandlers struct {
	db           *db.DB
	sessionStore *auth.SessionStore
}

func NewPreferencesHandlers(database *db.DB, sessionStore *auth.SessionStore) *PreferencesHandlers {
	return &PreferencesHandlers{
		db:           database,
		sessionStore: sessionStore,
	}
}

func (ph *PreferencesHandlers) HandlePreferences(w http.ResponseWriter, r *http.Request) {
	utils.LogHTTP("%s /preferences", r.Method)

	session := getSessionFromRequest(r, ph.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		ph.getPreferences(w, r, session.UserID)
	case http.MethodPut:
		ph.updatePreferences(w, r, session.UserID)
	default:
		utils.LogHTTP("Method %s not allowed for /preferences", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ph *PreferencesHandlers) getPreferences(w http.ResponseWriter, r *http.Request, userID int) {
	utils.LogHTTP("Getting preferences for user %d", userID)

	preferences, err := ph.db.GetUserPreferences(userID)
	if err != nil {
		utils.LogError("Failed to get preferences for user %d: %v", userID, err)
		http.Error(w, "Failed to get preferences", http.StatusInternalServerError)
		return
	}

	utils.LogHTTP("Returning preferences for user %d", userID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(preferences)
}

func (ph *PreferencesHandlers) updatePreferences(w http.ResponseWriter, r *http.Request, userID int) {
	utils.LogHTTP("Updating preferences for user %d", userID)

	var req models.UserPreferencesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogHTTP("Invalid JSON in preferences update request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate preference values
	if err := ph.validatePreferencesRequest(&req); err != nil {
		utils.LogHTTP("Invalid preference values: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	preferences, err := ph.db.UpdateUserPreferences(userID, req)
	if err != nil {
		utils.LogError("Failed to update preferences for user %d: %v", userID, err)
		http.Error(w, "Failed to update preferences", http.StatusInternalServerError)
		return
	}

	utils.LogHTTP("Updated preferences for user %d", userID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(preferences)
}

func (ph *PreferencesHandlers) validatePreferencesRequest(req *models.UserPreferencesRequest) error {
	if req.PracticeSessionLength != nil && (*req.PracticeSessionLength < 1 || *req.PracticeSessionLength > 50) {
		return fmt.Errorf("practice_session_length must be between 1 and 50")
	}

	if req.DifficultyPreference != nil {
		validDifficulties := []string{"easy", "medium", "hard", "adaptive", "mixed"}
		if !contains(validDifficulties, *req.DifficultyPreference) {
			return fmt.Errorf("difficulty_preference must be one of: %v", validDifficulties)
		}
	}

	if req.ReviewMode != nil {
		validModes := []string{"immediate", "end_of_session"}
		if !contains(validModes, *req.ReviewMode) {
			return fmt.Errorf("review_mode must be one of: %v", validModes)
		}
	}

	if req.AutoAdvanceTimingOpen != nil && (*req.AutoAdvanceTimingOpen < 5000 || *req.AutoAdvanceTimingOpen > 300000) {
		return fmt.Errorf("auto_advance_timing_open must be between 5000 and 300000 milliseconds")
	}

	if req.AutoAdvanceTimingChoice != nil && (*req.AutoAdvanceTimingChoice < 3000 || *req.AutoAdvanceTimingChoice > 180000) {
		return fmt.Errorf("auto_advance_timing_choice must be between 3000 and 180000 milliseconds")
	}

	if req.ThemeMode != nil {
		validThemes := []string{"light", "dark", "system"}
		if !contains(validThemes, *req.ThemeMode) {
			return fmt.Errorf("theme_mode must be one of: %v", validThemes)
		}
	}

	if req.CategoryPreference != nil && len(*req.CategoryPreference) > 0 {
		// Validate that all categories exist (optional - you could skip this)
		validCategories := []string{"symboles", "personnalités", "politique", "histoire", "laïcité", "valeurs", "société", "citoyenneté", "patrimoine", "culture", "géographie", "europe", "sciences"}
		for _, category := range *req.CategoryPreference {
			if !contains(validCategories, category) {
				return fmt.Errorf("invalid category: %s", category)
			}
		}
	}

	return nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
