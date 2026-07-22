package mailer

import (
	"context"
	"log/slog"
)

// LogSender is a safe development sink. It confirms that a delivery request
// reached notifications, but deliberately never prints the recipient or code.
type LogSender struct {
	logger *slog.Logger
}

func NewLogSender(logger *slog.Logger) *LogSender {
	return &LogSender{logger: logger}
}

func (s *LogSender) SendVerificationCode(_ context.Context, message VerificationMessage) error {
	if err := validateMessage(message); err != nil {
		return err
	}
	if s != nil && s.logger != nil {
		s.logger.Info("dev email delivery accepted",
			"companyId", message.CompanyID,
			"challengeId", message.ChallengeID,
			"deliveryId", message.IdempotencyKey,
		)
	}
	return nil
}
