package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

type EmailMessage struct {
	To      string
	Subject string
	Text    string
}

type EmailSender interface {
	SendEmail(ctx context.Context, message EmailMessage) error
}

type SMSSender interface {
	SendSMS(ctx context.Context, to, text string) error
}

type LogEmailSender struct {
	logger zerolog.Logger
}

func NewLogEmailSender(logger zerolog.Logger) *LogEmailSender {
	return &LogEmailSender{logger: logger}
}

func (s *LogEmailSender) SendEmail(_ context.Context, message EmailMessage) error {
	s.logger.Debug().
		Str("channel", string(ChannelEmail)).
		Str("to", message.To).
		Str("subject", message.Subject).
		Str("body", message.Text).
		Msg("email delivered to the log sender")
	return nil
}

type LogSMSSender struct {
	logger zerolog.Logger
}

func NewLogSMSSender(logger zerolog.Logger) *LogSMSSender {
	return &LogSMSSender{logger: logger}
}

func (s *LogSMSSender) SendSMS(_ context.Context, to, text string) error {
	s.logger.Debug().
		Str("channel", string(ChannelPhone)).
		Str("to", to).
		Str("body", text).
		Msg("text message delivered to the log sender")
	return nil
}

type NoopSMSSender struct{}

func NewNoopSMSSender() *NoopSMSSender {
	return &NoopSMSSender{}
}

func (*NoopSMSSender) SendSMS(context.Context, string, string) error {
	return ErrNotConfigured
}

type SMTPOptions struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	StartTLS bool
	Timeout  time.Duration
}

type SMTPEmailSender struct {
	options SMTPOptions
	dial    func(ctx context.Context, address string) (net.Conn, error)
}

const defaultSMTPTimeout = 15 * time.Second

func NewSMTPEmailSender(options SMTPOptions) (*SMTPEmailSender, error) {
	if strings.TrimSpace(options.Host) == "" {
		return nil, fmt.Errorf("%w: SMTP_HOST is empty", ErrNotConfigured)
	}
	if options.Port <= 0 || options.Port > 65535 {
		return nil, fmt.Errorf("%w: SMTP_PORT is out of range", ErrNotConfigured)
	}
	if strings.TrimSpace(options.From) == "" {
		return nil, fmt.Errorf("%w: SMTP_FROM is empty", ErrNotConfigured)
	}
	if options.Timeout <= 0 {
		options.Timeout = defaultSMTPTimeout
	}
	sender := &SMTPEmailSender{options: options}
	sender.dial = func(ctx context.Context, address string) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: options.Timeout}
		return dialer.DialContext(ctx, "tcp", address)
	}
	return sender, nil
}

func (s *SMTPEmailSender) SendEmail(ctx context.Context, message EmailMessage) error {
	address := net.JoinHostPort(s.options.Host, fmt.Sprintf("%d", s.options.Port))

	conn, err := s.dial(ctx, address)
	if err != nil {
		return fmt.Errorf("dial smtp server: %w", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(s.options.Timeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return fmt.Errorf("set smtp deadline: %w", err)
	}

	client, err := smtp.NewClient(conn, s.options.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("open smtp session: %w", err)
	}
	defer func() {
		_ = client.Close()
	}()

	if s.options.StartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("smtp server does not offer STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: s.options.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("start smtp tls: %w", err)
		}
	}

	if s.options.Username != "" {
		auth := smtp.PlainAuth("", s.options.Username, s.options.Password, s.options.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("authenticate to smtp server: %w", err)
		}
	}

	if err := client.Mail(s.options.From); err != nil {
		return fmt.Errorf("smtp sender rejected: %w", err)
	}
	if err := client.Rcpt(message.To); err != nil {
		return fmt.Errorf("smtp recipient rejected: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open smtp data: %w", err)
	}
	if _, err := writer.Write(buildMIME(s.options.From, message)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write smtp body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close smtp body: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("close smtp session: %w", err)
	}
	return nil
}

func buildMIME(from string, message EmailMessage) []byte {
	var builder strings.Builder
	builder.WriteString("From: " + sanitizeHeader(from) + "\r\n")
	builder.WriteString("To: " + sanitizeHeader(message.To) + "\r\n")
	builder.WriteString("Subject: " + sanitizeHeader(message.Subject) + "\r\n")
	builder.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	builder.WriteString("\r\n")
	builder.WriteString(strings.ReplaceAll(message.Text, "\n", "\r\n"))
	builder.WriteString("\r\n")
	return []byte(builder.String())
}

func sanitizeHeader(value string) string {
	replacer := strings.NewReplacer("\r", " ", "\n", " ")
	return strings.TrimSpace(replacer.Replace(value))
}
