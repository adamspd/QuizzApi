// internal/api/router.go
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"french-citizenship-api/internal/storage"
)

type API struct {
	db *storage.DB
}

func NewRouter(db *storage.DB) http.Handler {
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

	// Wrap with CORS middleware
	return corsMiddleware(mux)
}

// CORS middleware to allow web requests
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow requests from any origin (for development)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (api *API) healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "ok"}`))
}

func (api *API) handleQuestions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		api.getQuestions(w, r)
	case http.MethodPost:
		api.createQuestion(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *API) handleQuestionByID(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path
	path := strings.TrimPrefix(r.URL.Path, "/questions/")
	id, err := strconv.Atoi(path)
	if err != nil {
		http.Error(w, "Invalid question ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		api.getQuestionByID(w, r, id)
	case http.MethodPut:
		api.updateQuestion(w, r, id)
	case http.MethodDelete:
		api.deleteQuestion(w, r, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *API) getQuestions(w http.ResponseWriter, r *http.Request) {
	questions, err := api.db.GetAllQuestions()
	if err != nil {
		http.Error(w, "Failed to fetch questions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"questions": questions,
	})
}

func (api *API) createQuestion(w http.ResponseWriter, r *http.Request) {
	var req storage.QuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	question, err := api.db.CreateQuestion(req)
	if err != nil {
		http.Error(w, "Failed to create question", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(question)
}

func (api *API) getQuestionByID(w http.ResponseWriter, r *http.Request, id int) {
	question, err := api.db.GetQuestionByID(id)
	if err != nil {
		http.Error(w, "Question not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(question)
}

func (api *API) updateQuestion(w http.ResponseWriter, r *http.Request, id int) {
	var req storage.QuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	question, err := api.db.UpdateQuestion(id, req)
	if err != nil {
		http.Error(w, "Failed to update question", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(question)
}

func (api *API) deleteQuestion(w http.ResponseWriter, r *http.Request, id int) {
	err := api.db.DeleteQuestion(id)
	if err != nil {
		http.Error(w, "Failed to delete question", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (api *API) getNextQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// TODO: Get user ID from JWT when auth is implemented
	userID := 1

	// Default to 10 questions, allow override via query param
	count := 10
	if countStr := r.URL.Query().Get("count"); countStr != "" {
		if c, err := strconv.Atoi(countStr); err == nil && c > 0 && c <= 50 {
			count = c
		}
	}

	questions, err := api.db.GetNextQuestions(userID, count)
	if err != nil {
		http.Error(w, "Failed to fetch next questions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"questions": questions,
		"count":     len(questions),
	})
}

func (api *API) handleProgress(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		api.recordProgress(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *API) recordProgress(w http.ResponseWriter, r *http.Request) {
	var req storage.ProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.QuestionID == 0 || req.UserAnswer == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// TODO: Get user ID from JWT when auth is implemented
	userID := 1

	progress, err := api.db.RecordProgress(userID, req)
	if err != nil {
		http.Error(w, "Failed to record progress", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(progress)
}

func (api *API) getProgressStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// TODO: Get user ID from JWT when auth is implemented
	userID := 1

	stats, err := api.db.GetUserStats(userID)
	if err != nil {
		http.Error(w, "Failed to fetch stats", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (api *API) importQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		log.Printf("[ERROR] Method %s not allowed for /import", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	log.Println("[IMPORT] Starting question import process")

	// Parse JSON request
	var importReq storage.ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&importReq); err != nil {
		log.Printf("[IMPORT ERROR] Invalid JSON in import request: %v", err)
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	log.Printf("[IMPORT] Received import request with %d questions", len(importReq.Questions))

	// Validate request
	if len(importReq.Questions) == 0 {
		log.Println("[IMPORT ERROR] No questions provided in import request")
		http.Error(w, "No questions provided", http.StatusBadRequest)
		return
	}

	if len(importReq.Questions) > 1000 {
		log.Printf("[IMPORT ERROR] Too many questions in import request: %d (max 1000)", len(importReq.Questions))
		http.Error(w, "Too many questions (max 1000 per import)", http.StatusBadRequest)
		return
	}

	// Perform import
	result, err := api.db.ImportQuestions(importReq)
	if err != nil {
		log.Printf("[IMPORT ERROR] Import failed: %v", err)
		http.Error(w, "Import failed", http.StatusInternalServerError)
		return
	}

	// Log summary
	log.Printf("[IMPORT SUCCESS] Import completed: %d imported, %d skipped, %d errors",
		result.ImportedQuestions, result.SkippedQuestions, len(result.Errors))

	// Return result
	w.Header().Set("Content-Type", "application/json")
	if result.ImportedQuestions > 0 {
		w.WriteHeader(http.StatusCreated)
	} else {
		w.WriteHeader(http.StatusOK) // No new questions imported
	}
	json.NewEncoder(w).Encode(result)
}
