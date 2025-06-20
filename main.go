package main

import (
	"database/sql"
	"github.com/joho/godotenv"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Printf("[WARN] No .env file found or failed to load it: %v", err)
	}
	// Set up proper logging with timestamps and microseconds
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	logStartup("French Citizenship API starting...")

	// Get environment variables
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

	// Load email configuration
	logStartup("Loading email configuration...")
	emailConfig := LoadEmailConfig()
	logStartup("Email configuration loaded - SMTP: %s:%d", emailConfig.SMTPHost, emailConfig.SMTPPort)
	if emailConfig.Username == "" {
		logStartup("SMTP not configured - emails will be logged to console")
	}

	// Initialize database with auth tables
	logStartup("Initializing database connection...")
	db, err := InitDBWithAuth(dbPath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize database: %v", err)
	}

	// Initialize session store
	logStartup("Initializing session store...")
	sessionStore := NewSessionStore()
	logStartup("Session store initialized with automatic cleanup")

	// Create default admin user if none exists
	if err := createDefaultAdminUser(db); err != nil {
		logError("Failed to create default admin user: %v", err)
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
	router := NewRouter(db, sessionStore, emailConfig)

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
	logStartup("Auth endpoints available at:")
	logStartup("  POST /auth/register - Register new user")
	logStartup("  POST /auth/login - Login")
	logStartup("  POST /auth/logout - Logout")
	logStartup("  GET  /auth/me - Get current user info")
	logStartup("  GET  /verify-email?token=... - Verify email")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[FATAL] Server failed to start: %v", err)
	}
}

// InitDBWithAuth initializes database with auth tables
func InitDBWithAuth(dbPath string) (*DB, error) {
	logStartup("Initializing database at: %s", dbPath)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		logError("Failed to open database: %v", err)
		return nil, err
	}

	if err := db.Ping(); err != nil {
		logError("Failed to ping database: %v", err)
		return nil, err
	}

	logStartup("Database connection established")

	if err := createTablesWithAuth(db); err != nil {
		logError("Failed to create tables: %v", err)
		return nil, err
	}

	logStartup("Database tables initialized successfully")
	return &DB{db}, nil
}

// createDefaultAdminUser creates a default admin user if none exists
func createDefaultAdminUser(db *DB) error {
	logStartup("Checking for existing admin users...")

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		logStartup("Admin user(s) already exist, skipping default admin creation")
		return nil
	}

	logStartup("No admin users found, creating default admin user...")

	// Create default admin user
	adminReq := UserRequest{
		Username: os.Getenv("ADMIN_USERNAME"),
		Email:    os.Getenv("ADMIN_EMAIL"),
		Password: os.Getenv("ADMIN_PASSWORD"),
		Role:     "admin",
	}

	// Set email as verified for admin
	isActive := true
	adminReq.IsActive = &isActive

	admin, err := db.CreateUser(adminReq)
	if err != nil {
		return err
	}

	// Mark admin email as verified
	_, err = db.Exec("UPDATE users SET email_verified = 1, email_verified_at = CURRENT_TIMESTAMP WHERE id = ?", admin.ID)
	if err != nil {
		logError("Failed to mark admin email as verified: %v", err)
	}

	logStartup("Default admin user created:")
	logStartup("  Username: %s", admin.Username)

	return nil
}
