package mailer

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var (
	ErrInvalidMessage      = errors.New("некорректное письмо с кодом подтверждения")
	ErrProviderUnavailable = errors.New("сервис отправки email временно недоступен")
)

var verificationCodePattern = regexp.MustCompile(`^[0-9]{6}$`)

type VerificationMessage struct {
	CompanyID, ChallengeID, IdempotencyKey string
	Recipient, Code                        string
	ExpiresAt                              time.Time
}

type Sender interface {
	SendVerificationCode(context.Context, VerificationMessage) error
}

func validateMessage(message VerificationMessage) error {
	if message.CompanyID == "" || message.ChallengeID == "" || message.IdempotencyKey == "" ||
		message.Recipient == "" || !verificationCodePattern.MatchString(message.Code) ||
		message.ExpiresAt.IsZero() {
		return ErrInvalidMessage
	}
	return nil
}
