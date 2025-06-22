package models

import "time"

// UserPreferences represents user preferences stored
type UserPreferences struct {
	UserID                  int       `json:"user_id"`
	PracticeSessionLength   int       `json:"practice_session_length"`
	DifficultyPreference    string    `json:"difficulty_preference"`
	CategoryPreference      []string  `json:"category_preference,omitempty"` // JSON array or NULL
	ReviewMode              string    `json:"review_mode"`
	AutoAdvanceTimingOpen   int       `json:"auto_advance_timing_open"`   // milliseconds
	AutoAdvanceTimingChoice int       `json:"auto_advance_timing_choice"` // milliseconds
	QuestionRandomization   bool      `json:"question_randomization"`
	SkipAnsweredQuestions   bool      `json:"skip_answered_questions"`
	FocusWeakAreas          bool      `json:"focus_weak_areas"`
	ThemeMode               string    `json:"theme_mode"`
	StatsVisibility         bool      `json:"stats_visibility"`
	InterfaceLanguage       string    `json:"interface_language"`
	UpdatedAt               time.Time `json:"updated_at"`
}

// UserPreferencesRequest for updating preferences
type UserPreferencesRequest struct {
	PracticeSessionLength   *int      `json:"practice_session_length,omitempty"`
	DifficultyPreference    *string   `json:"difficulty_preference,omitempty"`
	CategoryPreference      *[]string `json:"category_preference,omitempty"`
	ReviewMode              *string   `json:"review_mode,omitempty"`
	AutoAdvanceTimingOpen   *int      `json:"auto_advance_timing_open,omitempty"`
	AutoAdvanceTimingChoice *int      `json:"auto_advance_timing_choice,omitempty"`
	QuestionRandomization   *bool     `json:"question_randomization,omitempty"`
	SkipAnsweredQuestions   *bool     `json:"skip_answered_questions,omitempty"`
	FocusWeakAreas          *bool     `json:"focus_weak_areas,omitempty"`
	ThemeMode               *string   `json:"theme_mode,omitempty"`
	StatsVisibility         *bool     `json:"stats_visibility,omitempty"`
	InterfaceLanguage       *string   `json:"interface_language,omitempty"`
}

// GetDefaultPreferences returns default user preferences
func GetDefaultPreferences(userID int) *UserPreferences {
	return &UserPreferences{
		UserID:                  userID,
		PracticeSessionLength:   10,
		DifficultyPreference:    "adaptive",
		CategoryPreference:      nil, // All categories
		ReviewMode:              "immediate",
		AutoAdvanceTimingOpen:   60000, // 60 seconds for open questions
		AutoAdvanceTimingChoice: 30000, // 30 seconds for choice questions
		QuestionRandomization:   false, // Keep smart prioritization
		SkipAnsweredQuestions:   false, // Let smart algorithm handle it
		FocusWeakAreas:          true,  // Use existing smart logic
		ThemeMode:               "system",
		StatsVisibility:         true,
		InterfaceLanguage:       "fr",
		UpdatedAt:               time.Now(),
	}
}
