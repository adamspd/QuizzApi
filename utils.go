package main

import (
	"encoding/json"
	"log"
	"strings"
)

// Logging utilities
func logInfo(msg string, args ...interface{}) {
	log.Printf("[INFO] "+msg, args...)
}

func logError(msg string, args ...interface{}) {
	log.Printf("[ERROR] "+msg, args...)
}

func logDebug(msg string, args ...interface{}) {
	log.Printf("[DEBUG] "+msg, args...)
}

func logDB(msg string, args ...interface{}) {
	log.Printf("[DB] "+msg, args...)
}

func logHTTP(msg string, args ...interface{}) {
	log.Printf("[HTTP] "+msg, args...)
}

func logImport(msg string, args ...interface{}) {
	log.Printf("[IMPORT] "+msg, args...)
}

func logStartup(msg string, args ...interface{}) {
	log.Printf("[STARTUP] "+msg, args...)
}

func logShutdown(msg string, args ...interface{}) {
	log.Printf("[SHUTDOWN] "+msg, args...)
}

// Helper function to normalize answers for comparison
func normalizeAnswer(answer string) string {
	normalized := strings.ToLower(strings.TrimSpace(answer))
	logDebug("Normalized answer: '%s' -> '%s'", answer, normalized)
	return normalized
}

// Check answer based on question type
func checkAnswer(question *Question, userAnswer string) bool {
	switch question.QuestionType {
	case "open_text":
		return normalizeAnswer(userAnswer) == normalizeAnswer(question.Answer)

	case "multiple_choice", "true_false":
		return normalizeAnswer(userAnswer) == normalizeAnswer(question.Answer)

	case "multiple_select":
		var correctAnswers []string
		if err := json.Unmarshal([]byte(question.Answer), &correctAnswers); err != nil {
			logError("Failed to parse multiple_select answer: %v", err)
			return false
		}

		userAnswers := strings.Split(userAnswer, ",")
		for i, answer := range userAnswers {
			userAnswers[i] = strings.TrimSpace(answer)
		}

		normalizedCorrect := make([]string, len(correctAnswers))
		for i, answer := range correctAnswers {
			normalizedCorrect[i] = normalizeAnswer(answer)
		}

		normalizedUser := make([]string, len(userAnswers))
		for i, answer := range userAnswers {
			normalizedUser[i] = normalizeAnswer(answer)
		}

		if len(normalizedCorrect) != len(normalizedUser) {
			return false
		}

		for _, correct := range normalizedCorrect {
			found := false
			for _, user := range normalizedUser {
				if correct == user {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}

		return true

	default:
		logError("Unknown question type: %s", question.QuestionType)
		return false
	}
}
