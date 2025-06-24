package auth

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"net/url"
	"time"

	"github.com/adamspd/QuizzApi/models"
	"github.com/adamspd/QuizzApi/utils"
)

// LoadEmailConfig loads email configuration from environment
func LoadEmailConfig() *models.EmailConfig {
	gracePeriodHours := utils.GetEnvInt("EMAIL_GRACE_PERIOD_HOURS", 2)

	return &models.EmailConfig{
		SMTPHost:    utils.GetEnvOrDefault("SMTP_HOST", "mail.adamspierredavid.com"),
		SMTPPort:    utils.GetEnvInt("SMTP_PORT", 465),
		Username:    utils.GetEnvOrDefault("SMTP_USERNAME", ""),
		Password:    utils.GetEnvOrDefault("SMTP_PASSWORD", ""),
		FromAddress: utils.GetEnvOrDefault("FROM_EMAIL", "noreply@adamspierredavid.com"),
		FromName:    utils.GetEnvOrDefault("FROM_NAME", "French Citizenship Training"),
		BaseURL:     utils.GetEnvOrDefault("BASE_URL", "http://localhost:8043"),
		GracePeriod: time.Duration(gracePeriodHours) * time.Hour,
	}
}

// EmailService handles email sending
type EmailService struct {
	config *models.EmailConfig
}

// NewEmailService creates a new email service
func NewEmailService(config *models.EmailConfig) *EmailService {
	return &EmailService{config: config}
}

func (es *EmailService) BuildVerificationEmail(user *models.User, token string) (string, string) {
	verificationURL := fmt.Sprintf("%s/verify-email?token=%s", es.config.BaseURL, url.QueryEscape(token))

	subject := "Verify your email address"
	body := fmt.Sprintf(`Hello %s,

Thank you for registering for French Citizenship Training!

Please click the link below to verify your email address:
%s

You have %d hours to use the application before verification is required.
After that, your account will be temporarily disabled until you verify your email.

This verification link will expire in 24 hours.

If you didn't create this account, please ignore this email.

Best regards,
French Citizenship Training Team`, user.Username, verificationURL, int(es.config.GracePeriod.Hours()))

	return subject, body
}

func (es *EmailService) SendEmail(to, subject, body string) error {
	if es.config.Username == "" || es.config.Password == "" {
		utils.LogInfo("SMTP not configured, logging email instead")
		utils.LogInfo("=== EMAIL ===")
		utils.LogInfo("To: %s", to)
		utils.LogInfo("Subject: %s", subject)
		utils.LogInfo("Body: %s", body)
		utils.LogInfo("=============")
		return nil
	}

	return es.sendEmail(to, subject, body)
}

// sendEmail sends an email using SMTP with SSL support
func (es *EmailService) sendEmail(to, subject, body string) error {
	utils.LogInfo("Sending email to %s: %s", to, subject)

	// Prepare message
	message := fmt.Sprintf("From: %s <%s>\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s\r\n", es.config.FromName, es.config.FromAddress, to, subject, body)

	// For port 465 (implicit SSL), we need to establish SSL connection first
	addr := fmt.Sprintf("%s:%d", es.config.SMTPHost, es.config.SMTPPort)

	var conn net.Conn
	var err error

	if es.config.SMTPPort == 465 {
		// Port 465 uses implicit SSL (SMTPS)
		utils.LogDebug("Connecting to SMTP server %s with SSL", addr)
		tlsConfig := &tls.Config{
			ServerName: es.config.SMTPHost,
		}
		conn, err = tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			utils.LogError("Failed to establish SSL connection to %s: %v", addr, err)
			return err
		}
	} else {
		// Port 587 or 25 uses plain connection with STARTTLS
		utils.LogDebug("Connecting to SMTP server %s (plain)", addr)
		conn, err = net.Dial("tcp", addr)
		if err != nil {
			utils.LogError("Failed to connect to %s: %v", addr, err)
			return err
		}
	}
	defer conn.Close()

	// Create an SMTP client
	client, err := smtp.NewClient(conn, es.config.SMTPHost)
	if err != nil {
		utils.LogError("Failed to create SMTP client: %v", err)
		return err
	}
	defer client.Quit()

	// For non-SSL connections, try STARTTLS if available
	if es.config.SMTPPort != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsConfig := &tls.Config{
				ServerName: es.config.SMTPHost,
			}
			if err = client.StartTLS(tlsConfig); err != nil {
				utils.LogError("Failed to start TLS: %v", err)
				return err
			}
			utils.LogDebug("STARTTLS initiated successfully")
		}
	}

	// Authenticate
	auth := smtp.PlainAuth("", es.config.Username, es.config.Password, es.config.SMTPHost)
	if err = client.Auth(auth); err != nil {
		utils.LogError("SMTP authentication failed: %v", err)
		return err
	}
	utils.LogDebug("SMTP authentication successful")

	// Set sender
	if err = client.Mail(es.config.FromAddress); err != nil {
		utils.LogError("Failed to set sender: %v", err)
		return err
	}

	// Set recipient
	if err = client.Rcpt(to); err != nil {
		utils.LogError("Failed to set recipient: %v", err)
		return err
	}

	// Send message body
	writer, err := client.Data()
	if err != nil {
		utils.LogError("Failed to open data writer: %v", err)
		return err
	}
	defer writer.Close()

	_, err = writer.Write([]byte(message))
	if err != nil {
		utils.LogError("Failed to write message: %v", err)
		return err
	}

	utils.LogInfo("Email sent successfully to %s", to)
	return nil
}
