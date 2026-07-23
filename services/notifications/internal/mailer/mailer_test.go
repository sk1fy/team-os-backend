package mailer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestBuildVerificationEmailUsesRussianText(t *testing.T) {
	message := VerificationMessage{
		CompanyID: "8b28eb90-92ef-4984-b546-a8a9b61e019f", ChallengeID: "a303c0a0-a06b-4168-a72a-0e93d3aee983",
		IdempotencyKey: "f44a6a45-e8cd-4717-a988-ec5f9f8f8bf3", Recipient: "learner@example.com",
		Code: "123456", ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	got, err := buildVerificationEmail(SMTPConfig{FromAddress: "noreply@teamos.local", FromName: "TeamOS"}, message.Recipient, message)
	if err != nil {
		t.Fatalf("buildVerificationEmail() error = %v", err)
	}
	text := string(got)
	for _, fragment := range []string{"Код подтверждения: 123456", "Код действует 10 минут", "Никому его не сообщайте"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("письмо не содержит %q: %s", fragment, text)
		}
	}
}

func TestBuildVerificationEmailRejectsHeaderInjection(t *testing.T) {
	_, err := buildVerificationEmail(SMTPConfig{FromAddress: "noreply@teamos.local"}, "learner@example.com\r\nBcc: attacker@example.com", VerificationMessage{Code: "123456"})
	if err == nil {
		t.Fatal("ожидалась ошибка для инъекции заголовка")
	}
}

func TestLogSenderDoesNotRevealRecipientOrCode(t *testing.T) {
	var output bytes.Buffer
	sender := NewLogSender(slog.New(slog.NewJSONHandler(&output, nil)))
	message := VerificationMessage{
		CompanyID: "8b28eb90-92ef-4984-b546-a8a9b61e019f", ChallengeID: "a303c0a0-a06b-4168-a72a-0e93d3aee983",
		IdempotencyKey: "f44a6a45-e8cd-4717-a988-ec5f9f8f8bf3", Recipient: "secret@example.com",
		Code: "654321", ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	if err := sender.SendVerificationCode(context.Background(), message); err != nil {
		t.Fatalf("SendVerificationCode() error = %v", err)
	}
	if strings.Contains(output.String(), message.Recipient) || strings.Contains(output.String(), message.Code) {
		t.Fatalf("лог раскрыл email или код: %s", output.String())
	}
}
