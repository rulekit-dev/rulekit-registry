package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
)

type Mailer interface {
	SendOTP(ctx context.Context, toEmail, code string) error
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	// UseTLS controls whether to use STARTTLS (true) or plain SMTP (false).
	UseTLS bool
}

type smtpMailer struct {
	cfg SMTPConfig
}

func NewSMTP(cfg SMTPConfig) Mailer {
	return &smtpMailer{cfg: cfg}
}

func (m *smtpMailer) SendOTP(_ context.Context, toEmail, code string) error {
	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	msg := buildOTPMessage(m.cfg.From, toEmail, code)

	var auth smtp.Auth
	if m.cfg.Username != "" {
		auth = smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	}

	if m.cfg.UseTLS {
		return sendTLS(addr, m.cfg.Host, auth, m.cfg.From, toEmail, msg)
	}
	return smtp.SendMail(addr, auth, m.cfg.From, []string{toEmail}, []byte(msg))
}

func sendTLS(addr, host string, auth smtp.Auth, from, to, msg string) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("mailer: tls dial: %w", err)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("mailer: smtp client: %w", err)
	}
	defer c.Close()
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("mailer: smtp auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("mailer: smtp MAIL: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("mailer: smtp RCPT: %w", err)
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("mailer: smtp DATA: %w", err)
	}
	if _, err := fmt.Fprint(wc, msg); err != nil {
		return fmt.Errorf("mailer: smtp write: %w", err)
	}
	return wc.Close()
}

// stdoutMailer prints OTP codes to stdout — useful for local dev without SMTP.
type stdoutMailer struct{}

func NewStdout() Mailer { return &stdoutMailer{} }

func (m *stdoutMailer) SendOTP(_ context.Context, toEmail, code string) error {
	slog.Info("OTP code (no SMTP configured)", "email", toEmail, "code", code)
	return nil
}

func buildOTPMessage(from, to, code string) string {
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: Your RuleKit login code\r\n")
	sb.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString("Your RuleKit login code is: " + code + "\r\n")
	sb.WriteString("\r\nThis code expires in 10 minutes. Do not share it.\r\n")
	return sb.String()
}
