package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/adamspd/QuizzApi/auth"
	"github.com/adamspd/QuizzApi/db"
	"github.com/adamspd/QuizzApi/models"
	"github.com/adamspd/QuizzApi/utils"
)

type QuestionHandlers struct {
	db           *db.DB
	sessionStore *auth.SessionStore
}

func NewQuestionHandlers(database *db.DB, sessionStore *auth.SessionStore) *QuestionHandlers {
	return &QuestionHandlers{
		db:           database,
		sessionStore: sessionStore,
	}
}

func (qh *QuestionHandlers) HandleQuestions(w http.ResponseWriter, r *http.Request) {
	utils.LogHTTP("%s /questions", r.Method)
	switch r.Method {
	case http.MethodGet:
		qh.getQuestionsWithAuth(w, r)
	case http.MethodPost:
		qh.createQuestionWithAuth(w, r)
	default:
		utils.LogHTTP("Method %s not allowed for /questions", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (qh *QuestionHandlers) HandleQuestionByID(w http.ResponseWriter, r *http.Request, id int) {
	utils.LogHTTP("%s /questions/%d", r.Method, id)
	switch r.Method {
	case http.MethodGet:
		qh.getQuestionByIDWithAuth(w, r, id)
	case http.MethodPut:
		qh.updateQuestionWithAuth(w, r, id)
	case http.MethodDelete:
		qh.deleteQuestionWithAuth(w, r, id)
	default:
		utils.LogHTTP("Method %s not allowed for /questions/%d", r.Method, id)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (qh *QuestionHandlers) getQuestionsWithAuth(w http.ResponseWriter, r *http.Request) {
	session := getSessionFromRequest(r, qh.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	questions, err := qh.db.GetAllQuestionsForUser(session.UserID, session.Role)
	if err != nil {
		utils.LogError("Failed to fetch questions: %v", err)
		http.Error(w, "Failed to fetch questions", http.StatusInternalServerError)
		return
	}

	utils.LogHTTP("Returning %d questions for user %s", len(questions), session.Username)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"questions": questions,
	})
}

func (qh *QuestionHandlers) createQuestionWithAuth(w http.ResponseWriter, r *http.Request) {
	session := getSessionFromRequest(r, qh.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req models.QuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogHTTP("Invalid JSON in create request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Set question status based on user role
	if session.Role == "admin" {
		// Admins can create approved questions directly
		if req.Status == "" {
			req.Status = "approved"
		}
	} else {
		// Regular users and moderators create pending questions
		req.Status = "pending"
	}

	// Create question using updated function that accepts creator ID
	question, err := qh.db.CreateQuestionWithAuth(req, session.UserID, session.Role)
	if err != nil {
		utils.LogError("Failed to create question: %v", err)
		http.Error(w, "Failed to create question", http.StatusInternalServerError)
		return
	}

	utils.LogHTTP("Created question ID %d by user %s (status: %s)", question.ID, session.Username, question.Status)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(question)
}

func (qh *QuestionHandlers) getQuestionByIDWithAuth(w http.ResponseWriter, r *http.Request, id int) {
	session := getSessionFromRequest(r, qh.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	question, err := qh.db.GetQuestionByID(id)
	if err != nil {
		utils.LogHTTP("Question ID %d not found: %v", id, err)
		http.Error(w, "Question not found", http.StatusNotFound)
		return
	}

	// Check permissions - users can only see approved questions or their own
	if session.Role != "admin" && session.Role != "moderator" {
		if question.Status != "approved" && question.CreatedBy != session.UserID {
			http.Error(w, "Question not found", http.StatusNotFound)
			return
		}
	}

	utils.LogHTTP("Returning question ID %d", id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(question)
}

func (qh *QuestionHandlers) updateQuestionWithAuth(w http.ResponseWriter, r *http.Request, id int) {
	session := getSessionFromRequest(r, qh.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Get existing question to check permissions
	question, err := qh.db.GetQuestionByID(id)
	if err != nil {
		utils.LogHTTP("Question ID %d not found: %v", id, err)
		http.Error(w, "Question not found", http.StatusNotFound)
		return
	}

	// Check if user can edit this question
	if !session.CanEditQuestion(question) {
		http.Error(w, "Insufficient permissions to edit this question", http.StatusForbidden)
		return
	}

	var req models.QuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogHTTP("Invalid JSON in update request for ID %d: %v", id, err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Regular users can't change status, moderators/admins can
	if session.Role == "user" {
		req.Status = "" // Preserve existing status
	}

	updatedQuestion, err := qh.db.UpdateQuestionWithAuth(id, req, session.UserID, session.Role)
	if err != nil {
		utils.LogError("Failed to update question ID %d: %v", id, err)
		http.Error(w, "Failed to update question", http.StatusInternalServerError)
		return
	}

	utils.LogHTTP("Updated question ID %d by user %s", id, session.Username)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedQuestion)
}

func (qh *QuestionHandlers) deleteQuestionWithAuth(w http.ResponseWriter, r *http.Request, id int) {
	session := getSessionFromRequest(r, qh.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Get existing question to check permissions
	question, err := qh.db.GetQuestionByID(id)
	if err != nil {
		utils.LogHTTP("Question ID %d not found: %v", id, err)
		http.Error(w, "Question not found", http.StatusNotFound)
		return
	}

	// Check if user can delete this question
	if !session.CanEditQuestion(question) {
		http.Error(w, "Insufficient permissions to delete this question", http.StatusForbidden)
		return
	}

	err = qh.db.DeleteQuestion(id)
	if err != nil {
		utils.LogError("Failed to delete question ID %d: %v", id, err)
		http.Error(w, "Failed to delete question", http.StatusInternalServerError)
		return
	}

	utils.LogHTTP("Deleted question ID %d by user %s", id, session.Username)
	w.WriteHeader(http.StatusNoContent)
}

func (qh *QuestionHandlers) GetNextQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.LogHTTP("Method %s not allowed for /questions/next", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session := getSessionFromRequest(r, qh.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	count := 10
	if countStr := r.URL.Query().Get("count"); countStr != "" {
		if c, err := strconv.Atoi(countStr); err == nil && c > 0 && c <= 50 {
			count = c
		}
	}

	utils.LogHTTP("Getting next %d questions for user %s", count, session.Username)
	questions, err := qh.db.GetNextQuestionsForUser(session.UserID, count)
	if err != nil {
		utils.LogError("Failed to fetch next questions: %v", err)
		http.Error(w, "Failed to fetch next questions", http.StatusInternalServerError)
		return
	}

	utils.LogHTTP("Returning %d next questions", len(questions))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"questions": questions,
		"count":     len(questions),
	})
}

func (qh *QuestionHandlers) ImportQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.LogHTTP("Method %s not allowed for /import", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	utils.LogImport("Starting question import process")

	var importReq models.ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&importReq); err != nil {
		utils.LogError("Invalid JSON in import request: %v", err)
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	utils.LogImport("Received import request with %d questions", len(importReq.Questions))

	if len(importReq.Questions) == 0 {
		utils.LogImport("No questions provided in import request")
		http.Error(w, "No questions provided", http.StatusBadRequest)
		return
	}

	if len(importReq.Questions) > 1000 {
		utils.LogImport("Too many questions in import request: %d (max 1000)", len(importReq.Questions))
		http.Error(w, "Too many questions (max 1000 per import)", http.StatusBadRequest)
		return
	}

	result, err := qh.db.ImportQuestions(importReq)
	if err != nil {
		utils.LogError("Import failed: %v", err)
		http.Error(w, "Import failed", http.StatusInternalServerError)
		return
	}

	utils.LogImport("Import completed: %d imported, %d skipped, %d errors",
		result.ImportedQuestions, result.SkippedQuestions, len(result.Errors))

	w.Header().Set("Content-Type", "application/json")
	if result.ImportedQuestions > 0 {
		w.WriteHeader(http.StatusCreated)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(result)
}

func (qh *QuestionHandlers) HandleQuestionApproval(w http.ResponseWriter, r *http.Request, questionID int) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session := getSessionFromRequest(r, qh.sessionStore)
	if session == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	if !session.CanApproveQuestions() {
		http.Error(w, "Insufficient permissions", http.StatusForbidden)
		return
	}

	var req models.ApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogHTTP("Invalid JSON in approval request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Action != "approve" && req.Action != "reject" {
		http.Error(w, "Action must be 'approve' or 'reject'", http.StatusBadRequest)
		return
	}

	// Get the question
	question, err := qh.db.GetQuestionByID(questionID)
	if err != nil {
		utils.LogHTTP("Question ID %d not found: %v", questionID, err)
		http.Error(w, "Question not found", http.StatusNotFound)
		return
	}

	if question.Status != "pending" {
		http.Error(w, "Question is not pending approval", http.StatusBadRequest)
		return
	}

	// Update question status
	var newStatus string
	if req.Action == "approve" {
		newStatus = "approved"
	} else {
		newStatus = "rejected"
	}

	_, err = qh.db.Exec(`
		UPDATE questions 
		SET status = ?, approved_by = ?, approved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, newStatus, session.UserID, questionID)

	if err != nil {
		utils.LogError("Failed to update question approval status: %v", err)
		http.Error(w, "Failed to update question", http.StatusInternalServerError)
		return
	}

	utils.LogHTTP("Question ID %d %s by user %s", questionID, req.Action+"d", session.Username)

	// Return updated question
	updatedQuestion, _ := qh.db.GetQuestionByID(questionID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updatedQuestion)
}
