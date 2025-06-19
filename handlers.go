package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func NewRouter(db *DB) http.Handler {
	api := &API{db: db}

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", api.healthCheck)

	// Question routes
	mux.HandleFunc("/questions", api.handleQuestions)
	mux.HandleFunc("/questions/", api.handleQuestionByID)
	mux.HandleFunc("/questions/next", api.getNextQuestions)

	// Progress routes
	mux.HandleFunc("/progress", api.handleProgress)
	mux.HandleFunc("/progress/stats", api.getProgressStats)

	// Import/Export routes
	mux.HandleFunc("/import", api.importQuestions)

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
		api.getQuestions(w, r)
	case http.MethodPost:
		api.createQuestion(w, r)
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
		api.getQuestionByID(w, r, id)
	case http.MethodPut:
		api.updateQuestion(w, r, id)
	case http.MethodDelete:
		api.deleteQuestion(w, r, id)
	default:
		logHTTP("Method %s not allowed for /questions/%d", r.Method, id)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *API) getQuestions(w http.ResponseWriter, r *http.Request) {
	questions, err := api.db.GetAllQuestions()
	if err != nil {
		logError("Failed to fetch questions: %v", err)
		http.Error(w, "Failed to fetch questions", http.StatusInternalServerError)
		return
	}

	logHTTP("Returning %d questions", len(questions))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"questions": questions,
	})
}

func (api *API) createQuestion(w http.ResponseWriter, r *http.Request) {
	var req QuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logHTTP("Invalid JSON in create request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	question, err := api.db.CreateQuestion(req)
	if err != nil {
		logError("Failed to create question: %v", err)
		http.Error(w, "Failed to create question", http.StatusInternalServerError)
		return
	}

	logHTTP("Created question ID %d", question.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(question)
}

func (api *API) getQuestionByID(w http.ResponseWriter, r *http.Request, id int) {
	question, err := api.db.GetQuestionByID(id)
	if err != nil {
		logHTTP("Question ID %d not found: %v", id, err)
		http.Error(w, "Question not found", http.StatusNotFound)
		return
	}

	logHTTP("Returning question ID %d", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(question)
}

func (api *API) updateQuestion(w http.ResponseWriter, r *http.Request, id int) {
	var req QuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logHTTP("Invalid JSON in update request for ID %d: %v", id, err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	question, err := api.db.UpdateQuestion(id, req)
	if err != nil {
		logError("Failed to update question ID %d: %v", id, err)
		http.Error(w, "Failed to update question", http.StatusInternalServerError)
		return
	}

	logHTTP("Updated question ID %d", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(question)
}

func (api *API) deleteQuestion(w http.ResponseWriter, r *http.Request, id int) {
	err := api.db.DeleteQuestion(id)
	if err != nil {
		logError("Failed to delete question ID %d: %v", id, err)
		http.Error(w, "Failed to delete question", http.StatusInternalServerError)
		return
	}

	logHTTP("Deleted question ID %d", id)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) getNextQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logHTTP("Method %s not allowed for /questions/next", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// TODO: Get user ID from JWT when auth is implemented
	userID := 1

	count := 10
	if countStr := r.URL.Query().Get("count"); countStr != "" {
		if c, err := strconv.Atoi(countStr); err == nil && c > 0 && c <= 50 {
			count = c
		}
	}

	logHTTP("Getting next %d questions for user %d", count, userID)
	questions, err := api.db.GetNextQuestions(userID, count)
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
