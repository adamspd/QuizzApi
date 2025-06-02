package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"french-citizenship-api/internal/storage"
)

type API struct {
	db *storage.DB
}

// Logging middleware to wrap handlers
func (api *API) logRequest(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Log incoming request
		log.Printf("[REQUEST] %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

		// Wrap ResponseWriter to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		handler(wrapped, r)

		// Log response
		duration := time.Since(start)
		log.Printf("[RESPONSE] %s %s -> %d (%v)", r.Method, r.URL.Path, wrapped.statusCode, duration)
	}
}

// ResponseWriter wrapper to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func NewRouter(db *storage.DB) http.Handler {
	api := &API{db: db}

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", api.logRequest(api.healthCheck))

	// Question routes
	mux.HandleFunc("/questions", api.logRequest(api.handleQuestions))
	mux.HandleFunc("/questions/", api.logRequest(api.handleQuestionByID))
	mux.HandleFunc("/questions/next", api.logRequest(api.getNextQuestions))

	// Progress routes
	mux.HandleFunc("/progress", api.logRequest(api.handleProgress))
	mux.HandleFunc("/progress/stats", api.logRequest(api.getProgressStats))

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
	log.Println("[HEALTH] Health check requested")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "ok"}`))
}

func (api *API) handleQuestions(w http.ResponseWriter, r *http.Request) {
	log.Printf("[QUESTIONS] Handling %s request", r.Method)
	switch r.Method {
	case http.MethodGet:
		api.getQuestions(w, r)
	case http.MethodPost:
		api.createQuestion(w, r)
	default:
		log.Printf("[ERROR] Method %s not allowed for /questions", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *API) handleQuestionByID(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path
	path := strings.TrimPrefix(r.URL.Path, "/questions/")
	id, err := strconv.Atoi(path)
	if err != nil {
		log.Printf("[ERROR] Invalid question ID: %s", path)
		http.Error(w, "Invalid question ID", http.StatusBadRequest)
		return
	}

	log.Printf("[QUESTIONS] Handling %s request for question ID: %d", r.Method, id)

	switch r.Method {
	case http.MethodGet:
		api.getQuestionByID(w, r, id)
	case http.MethodPut:
		api.updateQuestion(w, r, id)
	case http.MethodDelete:
		api.deleteQuestion(w, r, id)
	default:
		log.Printf("[ERROR] Method %s not allowed for /questions/%d", r.Method, id)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *API) getQuestions(w http.ResponseWriter, r *http.Request) {
	log.Println("[DB] Fetching all questions")
	questions, err := api.db.GetAllQuestions()
	if err != nil {
		log.Printf("[ERROR] Failed to fetch questions: %v", err)
		http.Error(w, "Failed to fetch questions", http.StatusInternalServerError)
		return
	}

	log.Printf("[DB] Retrieved %d questions", len(questions))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"questions": questions,
	})
}

func (api *API) createQuestion(w http.ResponseWriter, r *http.Request) {
	var req storage.QuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[ERROR] Invalid JSON in create question: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("[DB] Creating question in category: %s", req.Category)
	question, err := api.db.CreateQuestion(req)
	if err != nil {
		log.Printf("[ERROR] Failed to create question: %v", err)
		http.Error(w, "Failed to create question", http.StatusInternalServerError)
		return
	}

	log.Printf("[DB] Created question with ID: %d", question.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(question)
}

func (api *API) getQuestionByID(w http.ResponseWriter, r *http.Request, id int) {
	log.Printf("[DB] Fetching question ID: %d", id)
	question, err := api.db.GetQuestionByID(id)
	if err != nil {
		log.Printf("[ERROR] Question ID %d not found: %v", id, err)
		http.Error(w, "Question not found", http.StatusNotFound)
		return
	}

	log.Printf("[DB] Retrieved question ID: %d from category: %s", id, question.Category)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(question)
}

func (api *API) updateQuestion(w http.ResponseWriter, r *http.Request, id int) {
	var req storage.QuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[ERROR] Invalid JSON in update question ID %d: %v", id, err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("[DB] Updating question ID: %d", id)
	question, err := api.db.UpdateQuestion(id, req)
	if err != nil {
		log.Printf("[ERROR] Failed to update question ID %d: %v", id, err)
		http.Error(w, "Failed to update question", http.StatusInternalServerError)
		return
	}

	log.Printf("[DB] Updated question ID: %d", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(question)
}

func (api *API) deleteQuestion(w http.ResponseWriter, r *http.Request, id int) {
	log.Printf("[DB] Deleting question ID: %d", id)
	err := api.db.DeleteQuestion(id)
	if err != nil {
		log.Printf("[ERROR] Failed to delete question ID %d: %v", id, err)
		http.Error(w, "Failed to delete question", http.StatusInternalServerError)
		return
	}

	log.Printf("[DB] Deleted question ID: %d", id)
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) getNextQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		log.Printf("[ERROR] Method %s not allowed for /questions/next", r.Method)
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

	log.Printf("[DB] Fetching next %d questions for user ID: %d", count, userID)
	questions, err := api.db.GetNextQuestions(userID, count)
	if err != nil {
		log.Printf("[ERROR] Failed to fetch next questions for user ID %d: %v", userID, err)
		http.Error(w, "Failed to fetch next questions", http.StatusInternalServerError)
		return
	}

	log.Printf("[DB] Retrieved %d next questions for user ID: %d", len(questions), userID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"questions": questions,
		"count":     len(questions),
	})
}

func (api *API) handleProgress(w http.ResponseWriter, r *http.Request) {
	log.Printf("[PROGRESS] Handling %s request", r.Method)
	switch r.Method {
	case http.MethodPost:
		api.recordProgress(w, r)
	default:
		log.Printf("[ERROR] Method %s not allowed for /progress", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *API) recordProgress(w http.ResponseWriter, r *http.Request) {
	var req storage.ProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[ERROR] Invalid JSON in record progress: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.QuestionID == 0 || req.UserAnswer == "" {
		log.Printf("[ERROR] Missing required fields in progress request: QuestionID=%d, UserAnswer='%s'", req.QuestionID, req.UserAnswer)
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// TODO: Get user ID from JWT when auth is implemented
	userID := 1

	log.Printf("[DB] Recording progress for user ID: %d, question ID: %d", userID, req.QuestionID)
	progress, err := api.db.RecordProgress(userID, req)
	if err != nil {
		log.Printf("[ERROR] Failed to record progress for user ID %d, question ID %d: %v", userID, req.QuestionID, err)
		http.Error(w, "Failed to record progress", http.StatusInternalServerError)
		return
	}

	log.Printf("[DB] Recorded progress ID: %d (correct: %t)", progress.ID, progress.IsCorrect)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(progress)
}

func (api *API) getProgressStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		log.Printf("[ERROR] Method %s not allowed for /progress/stats", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// TODO: Get user ID from JWT when auth is implemented
	userID := 1

	log.Printf("[DB] Fetching stats for user ID: %d", userID)
	stats, err := api.db.GetUserStats(userID)
	if err != nil {
		log.Printf("[ERROR] Failed to fetch stats for user ID %d: %v", userID, err)
		http.Error(w, "Failed to fetch stats", http.StatusInternalServerError)
		return
	}

	log.Printf("[DB] Retrieved stats for user ID %d: %d/%d correct (%.1f%%)",
		userID, stats.Correct, stats.Answered, stats.Accuracy*100)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
