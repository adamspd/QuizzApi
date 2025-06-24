package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/adamspd/QuizzApi/auth"
	"github.com/adamspd/QuizzApi/utils"
	"github.com/hibiken/asynq"
)

const (
	TypeSendEmail = "email:send"
)

type JobManager struct {
	client *asynq.Client
	server *asynq.Server
	mux    *asynq.ServeMux
}

type EmailPayload struct {
	To       string            `json:"to"`
	Subject  string            `json:"subject"`
	Body     string            `json:"body"`
	Type     string            `json:"type"`     // "verification", "password_reset", "notification", etc.
	Metadata map[string]string `json:"metadata"` // Extra data for logging/tracking
}

func NewJobManager(redisURL string) *JobManager {
	addr := strings.TrimPrefix(redisURL, "redis://")
	redisOpt := asynq.RedisClientOpt{
		Addr: addr,
	}

	client := asynq.NewClient(redisOpt)

	server := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: 10,
		Queues: map[string]int{
			"critical": 6, // Verification emails, password resets
			"default":  3, // General notifications
			"low":      1, // Marketing, etc.
		},
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			utils.LogError("Job failed: type=%s error=%v", task.Type(), err)
		}),
		Logger: &AsynqLogger{},
	})

	mux := asynq.NewServeMux()

	return &JobManager{
		client: client,
		server: server,
		mux:    mux,
	}
}

func (jm *JobManager) RegisterHandlers(emailService *auth.EmailService) {
	jm.mux.HandleFunc(TypeSendEmail, jm.handleSendEmail(emailService))
}

func (jm *JobManager) Start() error {
	utils.LogStartup("Starting job queue worker...")
	return jm.server.Run(jm.mux)
}

func (jm *JobManager) Stop() {
	utils.LogShutdown("Stopping job queue...")
	jm.server.Stop()
	jm.server.Shutdown()
	jm.client.Close()
}

// QueueEmail - Generic method to queue any email
func (jm *JobManager) QueueEmail(to, subject, body, emailType string, metadata map[string]string, priority string) error {
	if metadata == nil {
		metadata = make(map[string]string)
	}

	payload := EmailPayload{
		To:       to,
		Subject:  subject,
		Body:     body,
		Type:     emailType,
		Metadata: metadata,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal email payload: %w", err)
	}

	task := asynq.NewTask(TypeSendEmail, payloadBytes)

	// Set queue based on priority
	queue := "default"
	maxRetries := 3
	timeout := 60

	switch priority {
	case "critical":
		queue = "critical"
		maxRetries = 5
		timeout = 120
	case "low":
		queue = "low"
		maxRetries = 2
		timeout = 30
	}

	// Build options array to ensure timeout is always set
	var opts []asynq.Option
	opts = append(opts, asynq.Queue(queue))
	opts = append(opts, asynq.MaxRetry(maxRetries))

	// FIX: Always set timeout, never leave it unset
	timeoutDuration := time.Duration(timeout) * time.Second
	opts = append(opts, asynq.Timeout(timeoutDuration))

	info, err := jm.client.Enqueue(task, opts...)
	if err != nil {
		return fmt.Errorf("failed to enqueue email task: %w", err)
	}

	utils.LogInfo("Queued email job: ID=%s type=%s to=%s priority=%s timeout=%ds",
		info.ID, emailType, to, priority, timeout)
	return nil
}

func (jm *JobManager) QueueVerificationEmail(to, subject, body string, userID int, token string) error {
	metadata := map[string]string{
		"user_id": fmt.Sprintf("%d", userID),
		"token":   token,
	}
	return jm.QueueEmail(to, subject, body, "verification", metadata, "critical")
}

func (jm *JobManager) QueuePasswordResetEmail(to, subject, body string, userID int, token string) error {
	metadata := map[string]string{
		"user_id": fmt.Sprintf("%d", userID),
		"token":   token,
	}
	return jm.QueueEmail(to, subject, body, "password_reset", metadata, "critical")
}

func (jm *JobManager) QueueNotificationEmail(to, subject, body string, userID int) error {
	metadata := map[string]string{
		"user_id": fmt.Sprintf("%d", userID),
	}
	return jm.QueueEmail(to, subject, body, "notification", metadata, "default")
}

func (jm *JobManager) handleSendEmail(emailService *auth.EmailService) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, task *asynq.Task) error {
		var payload EmailPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return fmt.Errorf("failed to unmarshal email payload: %w", err)
		}

		utils.LogInfo("Processing email job: type=%s to=%s subject=%s", payload.Type, payload.To, payload.Subject)

		// Send the email using your existing email service
		if err := emailService.SendEmail(payload.To, payload.Subject, payload.Body); err != nil {
			// Log metadata for debugging
			metadataStr := ""
			for k, v := range payload.Metadata {
				metadataStr += fmt.Sprintf("%s=%s ", k, v)
			}

			return fmt.Errorf("failed to send %s email to %s (metadata: %s): %w",
				payload.Type, payload.To, metadataStr, err)
		}

		utils.LogInfo("Successfully sent %s email to %s", payload.Type, payload.To)
		return nil
	}
}

// Custom logger that uses your existing logging
type AsynqLogger struct{}

func (l *AsynqLogger) Debug(args ...interface{}) {
	utils.LogDebug(fmt.Sprint(args...))
}

func (l *AsynqLogger) Info(args ...interface{}) {
	utils.LogInfo(fmt.Sprint(args...))
}

func (l *AsynqLogger) Warn(args ...interface{}) {
	utils.LogError(fmt.Sprint(args...))
}

func (l *AsynqLogger) Error(args ...interface{}) {
	utils.LogError(fmt.Sprint(args...))
}

func (l *AsynqLogger) Fatal(args ...interface{}) {
	utils.LogError(fmt.Sprint(args...))
}
