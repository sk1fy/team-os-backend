package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/pkg/eventbus"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/deliverycrypto"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/mailer"
	"github.com/sk1fy/team-os-backend/services/notifications/internal/storage"
)

type fakeVerificationDeliveryStore struct {
	claimResult storage.ClaimEmailDeliveryResult
	claimErr    error
	claimed     storage.ClaimEmailDeliveryInput
	markedSent  bool
	failedCode  string
}

func (f *fakeVerificationDeliveryStore) Claim(_ context.Context, input storage.ClaimEmailDeliveryInput) (storage.ClaimEmailDeliveryResult, error) {
	f.claimed = input
	return f.claimResult, f.claimErr
}

func (f *fakeVerificationDeliveryStore) MarkSent(_ context.Context, _, _ uuid.UUID, _ time.Time) error {
	f.markedSent = true
	return nil
}

func (f *fakeVerificationDeliveryStore) MarkFailed(_ context.Context, _, _ uuid.UUID, code string, _ time.Time) error {
	f.failedCode = code
	return nil
}

type fakeVerificationEmailSender struct {
	message mailer.VerificationMessage
	err     error
	calls   int
}

func (f *fakeVerificationEmailSender) SendVerificationCode(_ context.Context, message mailer.VerificationMessage) error {
	f.calls++
	f.message = message
	return f.err
}

func TestHandleExternalEmailVerificationDeliversOnce(t *testing.T) {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	store := &fakeVerificationDeliveryStore{claimResult: storage.ClaimEmailDeliveryResult{ShouldSend: true, Attempts: 1}}
	sender := &fakeVerificationEmailSender{}
	service, cipher := verificationService(t, store, sender, now)
	event := verificationEvent(t, cipher, " Learner@Example.com ", "123456", now.Add(10*time.Minute))
	if bytes.Contains(event.Payload, []byte("Learner@Example.com")) || bytes.Contains(event.Payload, []byte("123456")) {
		t.Fatalf("event payload содержит открытый email или код: %s", event.Payload)
	}

	handled, err := service.Handle(externalEmailVerificationSubject)(context.Background(), event)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if !handled || sender.calls != 1 || !store.markedSent {
		t.Fatalf("доставка не завершена: handled=%v calls=%d sent=%v", handled, sender.calls, store.markedSent)
	}
	if sender.message.Recipient != "learner@example.com" || sender.message.Code != "123456" {
		t.Fatalf("неверное письмо: %+v", sender.message)
	}
	if len(store.claimed.RecipientFingerprint) != sha256Size {
		t.Fatalf("fingerprint length = %d", len(store.claimed.RecipientFingerprint))
	}
}

func TestHandleExternalEmailVerificationAcknowledgesDuplicate(t *testing.T) {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	store := &fakeVerificationDeliveryStore{claimResult: storage.ClaimEmailDeliveryResult{Terminal: true, Attempts: 1}}
	sender := &fakeVerificationEmailSender{}
	service, cipher := verificationService(t, store, sender, now)

	handled, err := service.Handle(externalEmailVerificationSubject)(context.Background(), verificationEvent(t, cipher, "learner@example.com", "123456", now.Add(10*time.Minute)))
	if err != nil || handled || sender.calls != 0 {
		t.Fatalf("дубликат обработан неверно: handled=%v calls=%d err=%v", handled, sender.calls, err)
	}
}

func TestHandleExternalEmailVerificationPersistsSafeFailure(t *testing.T) {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	store := &fakeVerificationDeliveryStore{claimResult: storage.ClaimEmailDeliveryResult{ShouldSend: true, Attempts: 1}}
	sender := &fakeVerificationEmailSender{err: errors.New("SMTP revealed learner@example.com and 123456")}
	service, cipher := verificationService(t, store, sender, now)

	_, err := service.Handle(externalEmailVerificationSubject)(context.Background(), verificationEvent(t, cipher, "learner@example.com", "123456", now.Add(10*time.Minute)))
	if !errors.Is(err, ErrVerificationEmailDelivery) || store.failedCode != "provider_unavailable" {
		t.Fatalf("неверная ошибка доставки: err=%v code=%q", err, store.failedCode)
	}
	if strings.Contains(err.Error(), "learner@example.com") || strings.Contains(err.Error(), "123456") {
		t.Fatalf("ошибка раскрывает PII или код: %v", err)
	}
}

func TestHandleExternalEmailVerificationRejectsInvalidCodeWithoutEcho(t *testing.T) {
	now := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	service, cipher := verificationService(t, &fakeVerificationDeliveryStore{}, &fakeVerificationEmailSender{}, now)
	_, err := service.Handle(externalEmailVerificationSubject)(context.Background(), verificationEvent(t, cipher, "secret@example.com", "not-secret", now.Add(10*time.Minute)))
	if !errors.Is(err, ErrInvalidVerificationEmailEvent) {
		t.Fatalf("Handle() error = %v", err)
	}
	if strings.Contains(err.Error(), "secret@example.com") || strings.Contains(err.Error(), "not-secret") {
		t.Fatalf("ошибка раскрывает входные данные: %v", err)
	}
}

func TestRecipientFingerprintIsTenantScopedAndNormalized(t *testing.T) {
	companyID := uuid.New()
	first := recipientFingerprint(companyID, "learner@example.com")
	second := recipientFingerprint(companyID, "learner@example.com")
	otherCompany := recipientFingerprint(uuid.New(), "learner@example.com")
	if string(first) != string(second) || string(first) == string(otherCompany) {
		t.Fatal("fingerprint должен быть стабильным внутри tenant и отличаться между tenant")
	}
}

const sha256Size = 32

func verificationService(
	t *testing.T,
	store verificationEmailDeliveryStore,
	sender mailer.Sender,
	now time.Time,
) (*Service, *deliverycrypto.Cipher) {
	t.Helper()
	cipher, err := deliverycrypto.New(bytes.Repeat([]byte{0x42}, 32), "v1")
	if err != nil {
		t.Fatalf("deliverycrypto.New() error = %v", err)
	}
	return &Service{
		emailDeliveries: store, emailSender: sender, emailDecryptor: cipher,
		now: func() time.Time { return now },
	}, cipher
}

func verificationEvent(t *testing.T, cipher *deliverycrypto.Cipher, recipient, code string, expiresAt time.Time) eventbus.Event {
	t.Helper()
	challengeID := uuid.NewString()
	encrypted, err := cipher.Seal(challengeID, deliverycrypto.Payload{
		RecipientEmail: recipient, VerificationCode: code,
	})
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	payload, err := json.Marshal(externalEmailVerificationPayload{
		ChallengeID: challengeID, EncryptedDeliveryPayload: encrypted,
		ExpiresAt: expiresAt, Purpose: "personal_access", KeyID: "v1",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return eventbus.Event{
		EventID: uuid.NewString(), OccurredAt: expiresAt.Add(-10 * time.Minute),
		CompanyID: uuid.NewString(), Payload: payload,
	}
}
