package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/adamspd/QuizzApi/models"
)

// Environment utilities
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func GetEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// Answer checking utilities
func NormalizeAnswer(answer string) string {
	normalized := strings.ToLower(strings.TrimSpace(answer))
	LogDebug("Normalized answer: '%s' -> '%s'", answer, normalized)
	return normalized
}

func CheckAnswer(question *models.Question, userAnswer string) bool {
	switch question.QuestionType {
	case "open_text":
		return NormalizeAnswer(userAnswer) == NormalizeAnswer(question.Answer)

	case "multiple_choice", "true_false":
		return NormalizeAnswer(userAnswer) == NormalizeAnswer(question.Answer)

	case "multiple_select":
		var correctAnswers []string
		if err := json.Unmarshal([]byte(question.Answer), &correctAnswers); err != nil {
			LogError("Failed to parse multiple_select answer: %v", err)
			return false
		}

		// Parse user answer - could be JSON array or comma-separated
		var userAnswers []string
		userAnswer = strings.TrimSpace(userAnswer)

		if strings.HasPrefix(userAnswer, "[") {
			// It's JSON array from frontend
			if err := json.Unmarshal([]byte(userAnswer), &userAnswers); err != nil {
				LogError("Failed to parse user JSON answer: %v", err)
				return false
			}
		} else {
			// Fallback: comma-separated
			userAnswers = strings.Split(userAnswer, ",")
			for i, answer := range userAnswers {
				userAnswers[i] = strings.TrimSpace(answer)
			}
		}

		// Now do SET comparison, not order-dependent bullshit
		if len(correctAnswers) != len(userAnswers) {
			return false
		}

		// Create maps for O(1) lookup instead of nested loops
		correctSet := make(map[string]bool)
		for _, answer := range correctAnswers {
			correctSet[NormalizeAnswer(answer)] = true
		}

		for _, userAns := range userAnswers {
			if !correctSet[NormalizeAnswer(userAns)] {
				return false
			}
		}

		return true

	default:
		LogError("Unknown question type: %s", question.QuestionType)
		return false
	}
}

// Validation utilities
func ValidateUserRequest(req *models.UserRequest, isUpdate bool) error {
	if strings.TrimSpace(req.Username) == "" {
		return fmt.Errorf("username is required")
	}

	if strings.TrimSpace(req.Email) == "" {
		return fmt.Errorf("email is required")
	}

	// Password required for creation, optional for updates
	if !isUpdate && strings.TrimSpace(req.Password) == "" {
		return fmt.Errorf("password is required")
	}

	if req.Password != "" && len(req.Password) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}

	// Validate role
	validRoles := []string{"user", "moderator", "admin"}
	if req.Role != "" {
		roleValid := false
		for _, role := range validRoles {
			if req.Role == role {
				roleValid = true
				break
			}
		}
		if !roleValid {
			return fmt.Errorf("invalid role: %s", req.Role)
		}
	}

	return nil
}
