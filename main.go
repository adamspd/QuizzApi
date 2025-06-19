package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Set up proper logging with timestamps and microseconds
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	logStartup("French Citizenship API starting...")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8043"
		logStartup("Using default port: %s", port)
	} else {
		logStartup("Using port from environment: %s", port)
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./citizenship.db"
		logStartup("Using default database path: %s", dbPath)
	} else {
		logStartup("Using database path from environment: %s", dbPath)
	}

	// Initialize database
	logStartup("Initializing database connection...")
	db, err := InitDB(dbPath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize database: %v", err)
	}

	// Set up graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		logShutdown("Received shutdown signal, closing database...")
		if err := db.Close(); err != nil {
			logError("Error closing database: %v", err)
		} else {
			logShutdown("Database connection closed successfully")
		}
		os.Exit(0)
	}()

	// Setup API routes
	logStartup("Setting up API routes...")
	router := NewRouter(db)

	// Create server with timeouts
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logStartup("Starting HTTP server on port %s...", port)
	logStartup("Server ready to accept connections at http://localhost:%s", port)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[FATAL] Server failed to start: %v", err)
	}
}
