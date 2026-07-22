package mailer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
)

type SMTPConfig struct {
	Host, Username, Password, FromAddress, FromName string
	Port                                            int
	RequireTLS                                      bool
}

type SMTPSender struct {
	config SMTPConfig
	dialer net.Dialer
}

func NewSMTPSender(config SMTPConfig) (*SMTPSender, error) {
	config.Host = strings.TrimSpace(config.Host)
	config.FromAddress = strings.ToLower(strings.TrimSpace(config.FromAddress))
	config.FromName = strings.TrimSpace(config.FromName)
	if config.Host == "" || config.Port < 1 || config.Port > 65535 {
		return nil, fmt.Errorf("некорректная конфигурация SMTP")
	}
	if _, err := exactAddress(config.FromAddress); err != nil {
		return nil, fmt.Errorf("некорректный адрес отправителя SMTP")
	}
	return &SMTPSender{config: config}, nil
}

func (s *SMTPSender) SendVerificationCode(ctx context.Context, message VerificationMessage) error {
	if s == nil {
		return ErrProviderUnavailable
	}
	if err := validateMessage(message); err != nil {
		return err
	}
	recipient, err := exactAddress(message.Recipient)
	if err != nil {
		return ErrInvalidMessage
	}
	body, err := buildVerificationEmail(s.config, recipient, message)
	if err != nil {
		return ErrInvalidMessage
	}
	address := net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))
	connection, err := s.dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return ErrProviderUnavailable
	}
	defer func() { _ = connection.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		if err = connection.SetDeadline(deadline); err != nil {
			return ErrProviderUnavailable
		}
	}
	client, err := smtp.NewClient(connection, s.config.Host)
	if err != nil {
		return ErrProviderUnavailable
	}
	defer func() { _ = client.Close() }()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err = client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: s.config.Host}); err != nil {
			return ErrProviderUnavailable
		}
	} else if s.config.RequireTLS {
		return ErrProviderUnavailable
	}
	if s.config.Username != "" {
		auth := smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)
		if err = client.Auth(auth); err != nil {
			return ErrProviderUnavailable
		}
	}
	if err = client.Mail(s.config.FromAddress); err != nil {
		return ErrProviderUnavailable
	}
	if err = client.Rcpt(recipient); err != nil {
		return ErrProviderUnavailable
	}
	writer, err := client.Data()
	if err != nil {
		return ErrProviderUnavailable
	}
	if _, err = io.Copy(writer, bytes.NewReader(body)); err != nil {
		_ = writer.Close()
		return ErrProviderUnavailable
	}
	if err = writer.Close(); err != nil {
		return ErrProviderUnavailable
	}
	if err = client.Quit(); err != nil {
		return ErrProviderUnavailable
	}
	return nil
}

func buildVerificationEmail(config SMTPConfig, recipient string, message VerificationMessage) ([]byte, error) {
	if _, err := exactAddress(recipient); err != nil || !verificationCodePattern.MatchString(message.Code) {
		return nil, ErrInvalidMessage
	}
	from := config.FromAddress
	if config.FromName != "" {
		from = (&mail.Address{Name: config.FromName, Address: config.FromAddress}).String()
	}
	var body bytes.Buffer
	w := bufio.NewWriter(&body)
	headers := [][2]string{
		{"From", from},
		{"To", recipient},
		{"Subject", mime.QEncoding.Encode("utf-8", "Код подтверждения TeamOS")},
		{"Message-ID", "<" + message.IdempotencyKey + "@teamos.local>"},
		{"MIME-Version", "1.0"},
		{"Content-Type", `text/plain; charset="UTF-8"`},
		{"Content-Transfer-Encoding", "8bit"},
	}
	for _, header := range headers {
		if strings.ContainsAny(header[1], "\r\n") {
			return nil, ErrInvalidMessage
		}
		_, _ = fmt.Fprintf(w, "%s: %s\r\n", header[0], header[1])
	}
	_, _ = fmt.Fprintf(w, "\r\nКод подтверждения: %s\r\n\r\n", message.Code)
	_, _ = fmt.Fprint(w, "Код действует 10 минут. Никому его не сообщайте.\r\n")
	if err := w.Flush(); err != nil {
		return nil, err
	}
	return body.Bytes(), nil
}

func exactAddress(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	address, err := mail.ParseAddress(trimmed)
	if err != nil || !strings.EqualFold(address.Address, trimmed) {
		return "", ErrInvalidMessage
	}
	return strings.ToLower(address.Address), nil
}
