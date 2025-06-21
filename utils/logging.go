package utils

import "log"

func LogInfo(msg string, args ...interface{}) {
	log.Printf("[INFO] "+msg, args...)
}

func LogError(msg string, args ...interface{}) {
	log.Printf("[ERROR] "+msg, args...)
}

func LogDebug(msg string, args ...interface{}) {
	log.Printf("[DEBUG] "+msg, args...)
}

func LogDB(msg string, args ...interface{}) {
	log.Printf("[DB] "+msg, args...)
}

func LogHTTP(msg string, args ...interface{}) {
	log.Printf("[HTTP] "+msg, args...)
}

func LogImport(msg string, args ...interface{}) {
	log.Printf("[IMPORT] "+msg, args...)
}

func LogStartup(msg string, args ...interface{}) {
	log.Printf("[STARTUP] "+msg, args...)
}

func LogShutdown(msg string, args ...interface{}) {
	log.Printf("[SHUTDOWN] "+msg, args...)
}
