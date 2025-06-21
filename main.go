package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/adamspd/QuizzApi/auth"
	"github.com/adamspd/QuizzApi/db"
	"github.com/adamspd/QuizzApi/handlers"
	"github.com/adamspd/QuizzApi/models"
	"github.com/adamspd/QuizzApi/utils"
)

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Printf("[WARN] No .env file found or failed to load it: %v", err)
	}

	// Set up proper logging with timestamps and microseconds
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	utils.LogStartup("French Citizenship API starting...")

	// Get environment variables
	port := utils.GetEnvOrDefault("PORT", "8043")
	utils.LogStartup("Using port: %s", port)

	dbPath := utils.GetEnvOrDefault("DB_PATH", "./citizenship.db")
	utils.LogStartup("Using database path: %s", dbPath)

	// Load email configuration
	utils.LogStartup("Loading email configuration...")
	emailConfig := auth.LoadEmailConfig()
	utils.LogStartup("Email configuration loaded - SMTP: %s:%d", emailConfig.SMTPHost, emailConfig.SMTPPort)
	if emailConfig.Username == "" {
		utils.LogStartup("SMTP not configured - emails will be logged to console")
	}

	// Initialize database
	utils.LogStartup("Initializing database connection...")
	database, err := db.InitDB(dbPath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize database: %v", err)
	}

	// Initialize session store
	utils.LogStartup("Initializing session store...")
	sessionStore := auth.NewSessionStore()
	utils.LogStartup("Session store initialized with automatic cleanup")

	// Create default admin user if none exists
	if err := createDefaultAdminUser(database); err != nil {
		utils.LogError("Failed to create default admin user: %v", err)
	}

	// Set up graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		utils.LogShutdown("Received shutdown signal, closing database...")
		if err := database.Close(); err != nil {
			utils.LogError("Error closing database: %v", err)
		} else {
			utils.LogShutdown("Database connection closed successfully")
		}
		os.Exit(0)
	}()

	// Setup API routes
	utils.LogStartup("Setting up API routes...")
	router := handlers.NewRouter(database, sessionStore, emailConfig)

	// Create server with timeouts
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	utils.LogStartup("Starting HTTP server on port %s...", port)
	utils.LogStartup("Server ready to accept connections at http://localhost:%s", port)
	utils.LogStartup("Auth endpoints available at:")
	utils.LogStartup("  POST /auth/register - Register new user")
	utils.LogStartup("  POST /auth/login - Login")
	utils.LogStartup("  POST /auth/logout - Logout")
	utils.LogStartup("  GET  /auth/me - Get current user info")
	utils.LogStartup("  GET  /verify-email?token=... - Verify email")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[FATAL] Server failed to start: %v", err)
	}
}

// createDefaultAdminUser creates a default admin user if none exists
func createDefaultAdminUser(database *db.DB) error {
	utils.LogStartup("Checking for existing admin users...")

	var count int
	err := database.QueryRow("SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		utils.LogStartup("Admin user(s) already exist, skipping default admin creation")
		return nil
	}

	utils.LogStartup("No admin users found, creating default admin user...")

	// Get admin credentials from environment
	adminUsername := utils.GetEnvOrDefault("ADMIN_USERNAME", "admin")
	adminEmail := utils.GetEnvOrDefault("ADMIN_EMAIL", "admin@example.com")
	adminPassword := utils.GetEnvOrDefault("ADMIN_PASSWORD", "admin123")

	// Create default admin user
	adminReq := models.UserRequest{
		Username: adminUsername,
		Email:    adminEmail,
		Password: adminPassword,
		Role:     "admin",
	}

	// Set email as verified for admin
	isActive := true
	adminReq.IsActive = &isActive

	admin, err := database.CreateUser(adminReq)
	if err != nil {
		return err
	}

	// Mark admin email as verified
	_, err = database.Exec("UPDATE users SET email_verified = 1, email_verified_at = CURRENT_TIMESTAMP WHERE id = ?", admin.ID)
	if err != nil {
		utils.LogError("Failed to mark admin email as verified: %v", err)
	}

	utils.LogStartup("Default admin user created:")
	utils.LogStartup("  Username: %s", admin.Username)
	utils.LogStartup("  Email: %s", admin.Email)
	utils.LogStartup("  Password: %s", adminPassword)

	return nil
}
