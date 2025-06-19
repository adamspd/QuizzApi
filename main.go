package main

import (
	"french-citizenship-api/internal/api"
	"french-citizenship-api/internal/storage"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Set up logging with timestamps
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("[STARTUP] French Citizenship API starting...")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8043" // Default port if not set
		log.Printf("[CONFIG] Using default port: %s", port)
	} else {
		log.Printf("[CONFIG] Using port from environment: %s", port)
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./citizenship.db"
		log.Printf("[CONFIG] Using default database path: %s", dbPath)
	} else {
		log.Printf("[CONFIG] Using database path from environment: %s", dbPath)
	}

	// Initialize database
	log.Println("[STARTUP] Initializing database connection...")
	db, err := storage.InitDB(dbPath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize database: %v", err)
	}

	// Set up graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		log.Println("[SHUTDOWN] Received shutdown signal, closing database...")
		if err := db.Close(); err != nil {
			log.Printf("[ERROR] Error closing database: %v", err)
		} else {
			log.Println("[SHUTDOWN] Database connection closed successfully")
		}
		os.Exit(0)
	}()

	// Setup API routes
	log.Println("[STARTUP] Setting up API routes...")
	router := api.NewRouter(db)

	// Create server with timeouts
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("[STARTUP] Starting HTTP server on port %s...", port)
	log.Printf("[STARTUP] Server ready to accept connections at http://localhost:%s", port)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[FATAL] Server failed to start: %v", err)
	}
}
