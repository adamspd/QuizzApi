package auth

import (
	"fmt"
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

// SendVerificationEmail sends an email verification link
func (es *EmailService) SendVerificationEmail(user *models.User, token string) error {
	if es.config.Username == "" || es.config.Password == "" {
		utils.LogInfo("SMTP not configured, logging verification token instead")
		utils.LogInfo("=== EMAIL VERIFICATION ===")
		utils.LogInfo("To: %s", user.Email)
		utils.LogInfo("Verification URL: %s/verify-email?token=%s", es.config.BaseURL, token)
		utils.LogInfo("==========================")
		return nil
	}

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

	return es.sendEmail(user.Email, subject, body)
}

// sendEmail sends an email using SMTP
func (es *EmailService) sendEmail(to, subject, body string) error {
	utils.LogInfo("Sending email to %s: %s", to, subject)

	// Prepare message
	message := fmt.Sprintf("From: %s <%s>\r\n"+
		"To: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s\r\n", es.config.FromName, es.config.FromAddress, to, subject, body)

	// SMTP authentication
	auth := smtp.PlainAuth("", es.config.Username, es.config.Password, es.config.SMTPHost)

	// Send email
	addr := fmt.Sprintf("%s:%d", es.config.SMTPHost, es.config.SMTPPort)
	err := smtp.SendMail(addr, auth, es.config.FromAddress, []string{to}, []byte(message))

	if err != nil {
		utils.LogError("Failed to send email to %s: %v", to, err)
		return err
	}

	utils.LogInfo("Email sent successfully to %s", to)
	return nil
}
