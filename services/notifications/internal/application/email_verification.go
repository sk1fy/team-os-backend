package application

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/pkg/eventbus"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/mailer"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/storage"
)

const externalEmailVerificationSubject = "teamos.academy.external_email_verification.requested.v1"

var (
	ErrInvalidVerificationEmailEvent = errors.New("некорректное событие запроса email-верификации")
	ErrVerificationEmailDelivery     = errors.New("не удалось доставить email-код подтверждения")
	purposePattern                   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

const (
	deliveryLease   = 12 * time.Second
	deliveryTimeout = 10 * time.Second
)

type externalEmailVerificationPayload struct {
	ChallengeID              string    `json:"challengeId"`
	EncryptedDeliveryPayload []byte    `json:"encryptedDeliveryPayload"`
	ExpiresAt                time.Time `json:"expiresAt"`
	Purpose                  string    `json:"purpose"`
	KeyID                    string    `json:"keyId"`
}

type verificationEmailDeliveryStore interface {
	Claim(context.Context, storage.ClaimEmailDeliveryInput) (storage.ClaimEmailDeliveryResult, error)
	MarkSent(context.Context, uuid.UUID, uuid.UUID, time.Time) error
	MarkFailed(context.Context, uuid.UUID, uuid.UUID, string, time.Time) error
}

func (s *Service) handleExternalEmailVerification(ctx context.Context, event eventbus.Event) (bool, error) {
	var payload externalEmailVerificationPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return false, ErrInvalidVerificationEmailEvent
	}
	eventID, companyID, err := eventIDs(event)
	if err != nil {
		return false, ErrInvalidVerificationEmailEvent
	}
	challengeID, err := uuid.Parse(payload.ChallengeID)
	if err != nil {
		return false, ErrInvalidVerificationEmailEvent
	}
	if s.emailDecryptor == nil || len(payload.EncryptedDeliveryPayload) == 0 || payload.ExpiresAt.IsZero() {
		return false, ErrInvalidVerificationEmailEvent
	}
	secret, err := s.emailDecryptor.Open(challengeID.String(), payload.KeyID, payload.EncryptedDeliveryPayload)
	if err != nil {
		return false, ErrInvalidVerificationEmailEvent
	}
	recipient, err := normalizeRecipientEmail(secret.RecipientEmail)
	if err != nil || !mailerCodeIsValid(secret.VerificationCode) {
		return false, ErrInvalidVerificationEmailEvent
	}
	purpose := strings.ToLower(strings.TrimSpace(payload.Purpose))
	if !purposePattern.MatchString(purpose) {
		return false, ErrInvalidVerificationEmailEvent
	}
	now := time.Now().UTC()
	if s.now != nil {
		now = s.now().UTC()
	}
	claim, err := s.emailDeliveries.Claim(ctx, storage.ClaimEmailDeliveryInput{
		ID: uuid.New(), EventID: eventID, CompanyID: companyID, ChallengeID: challengeID,
		Purpose: purpose, RecipientFingerprint: recipientFingerprint(companyID, recipient),
		ExpiresAt: payload.ExpiresAt.UTC(), Now: now, StaleBefore: now.Add(-deliveryLease),
	})
	if err != nil {
		return false, ErrVerificationEmailDelivery
	}
	if !claim.ShouldSend {
		return false, nil
	}
	if s.emailSender == nil {
		_ = s.emailDeliveries.MarkFailed(ctx, companyID, challengeID, "sender_not_configured", now)
		return false, ErrVerificationEmailDelivery
	}
	deliveryContext, cancel := context.WithTimeout(ctx, deliveryTimeout)
	defer cancel()
	err = s.emailSender.SendVerificationCode(deliveryContext, mailer.VerificationMessage{
		CompanyID: companyID.String(), ChallengeID: challengeID.String(), IdempotencyKey: challengeID.String(),
		Recipient: recipient, Code: secret.VerificationCode, ExpiresAt: payload.ExpiresAt.UTC(),
	})
	if err != nil {
		failedAt := now
		if s.now != nil {
			failedAt = s.now().UTC()
		}
		if markErr := s.emailDeliveries.MarkFailed(ctx, companyID, challengeID, deliveryErrorCode(err), failedAt); markErr != nil {
			return false, ErrVerificationEmailDelivery
		}
		return false, ErrVerificationEmailDelivery
	}
	sentAt := now
	if s.now != nil {
		sentAt = s.now().UTC()
	}
	if err = s.emailDeliveries.MarkSent(ctx, companyID, challengeID, sentAt); err != nil {
		return false, ErrVerificationEmailDelivery
	}
	return true, nil
}

func normalizeRecipientEmail(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(normalized)
	if err != nil || !strings.EqualFold(address.Address, normalized) {
		return "", ErrInvalidVerificationEmailEvent
	}
	return normalized, nil
}

func mailerCodeIsValid(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, symbol := range value {
		if symbol < '0' || symbol > '9' {
			return false
		}
	}
	return true
}

func recipientFingerprint(companyID uuid.UUID, normalizedEmail string) []byte {
	digest := sha256.Sum256([]byte(companyID.String() + "\x00" + normalizedEmail))
	return digest[:]
}

func deliveryErrorCode(err error) string {
	switch {
	case errors.Is(err, mailer.ErrInvalidMessage):
		return "invalid_message"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "timeout"
	default:
		return "provider_unavailable"
	}
}
