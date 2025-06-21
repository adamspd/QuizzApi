package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/adamspd/QuizzApi/auth"
	"github.com/adamspd/QuizzApi/db"
	"github.com/adamspd/QuizzApi/models"
	"github.com/adamspd/QuizzApi/utils"
)

type ProgressHandlers struct {
	db           *db.DB
	sessionStore *auth.SessionStore
}

func NewProgressHandlers(database *db.DB, sessionStore *auth.SessionStore) *ProgressHandlers {
	return &ProgressHandlers{
		db:           database,
		sessionStore: sessionStore,
	}
}

func (ph *ProgressHandlers) HandleProgress(w http.ResponseWriter, r *http.Request) {
	utils.LogHTTP("%s /progress", r.Method)
	switch r.Method {
	case http.MethodPost:
		ph.recordProgress(w, r)
	default:
		utils.LogHTTP("Method %s not allowed for /progress", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ph *ProgressHandlers) recordProgress(w http.ResponseWriter, r *http.Request) {
	session := getSessionFromRequest(r, ph.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req models.ProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogHTTP("Invalid JSON in progress request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.QuestionID == 0 || req.UserAnswer == "" {
		utils.LogHTTP("Missing required fields in progress request")
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	utils.LogHTTP("Recording progress for user %d, question %d", session.UserID, req.QuestionID)
	progress, err := ph.db.RecordProgress(session.UserID, req)
	if err != nil {
		utils.LogError("Failed to record progress: %v", err)
		http.Error(w, "Failed to record progress", http.StatusInternalServerError)
		return
	}

	utils.LogHTTP("Progress recorded: ID %d, correct: %t", progress.ID, progress.IsCorrect)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(progress)
}

func (ph *ProgressHandlers) GetProgressStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.LogHTTP("Method %s not allowed for /progress/stats", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session := getSessionFromRequest(r, ph.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	utils.LogHTTP("Getting stats for user %d", session.UserID)
	stats, err := ph.db.GetUserStats(session.UserID)
	if err != nil {
		utils.LogError("Failed to fetch stats for user %d: %v", session.UserID, err)
		http.Error(w, "Failed to fetch stats", http.StatusInternalServerError)
		return
	}

	utils.LogHTTP("Returning stats for user %d: %d/%d correct (%.1f%%)", session.UserID, stats.Correct, stats.Answered, stats.Accuracy*100)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
