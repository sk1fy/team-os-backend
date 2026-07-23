package deliverycrypto

import (
	"bytes"
	"errors"
	"testing"
)

func TestCipherRoundTripAndRandomNonce(t *testing.T) {
	cipher, err := New(bytes.Repeat([]byte{0x42}, 32), "v1")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	want := Payload{RecipientEmail: "learner@example.com", VerificationCode: "123456"}
	first, err := cipher.Seal("a303c0a0-a06b-4168-a72a-0e93d3aee983", want)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	second, err := cipher.Seal("a303c0a0-a06b-4168-a72a-0e93d3aee983", want)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("повторное шифрование должно использовать новый nonce")
	}
	got, err := cipher.Open("a303c0a0-a06b-4168-a72a-0e93d3aee983", "v1", first)
	if err != nil || got != want {
		t.Fatalf("Open() = (%+v, %v), ожидалось %+v", got, err, want)
	}
}

func TestCipherBindsEnvelopeToChallengeAndKeyID(t *testing.T) {
	cipher, err := New(bytes.Repeat([]byte{0x24}, 32), "active-key")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	envelope, err := cipher.Seal("challenge-a", Payload{RecipientEmail: "learner@example.com", VerificationCode: "123456"})
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	for _, test := range []struct{ challengeID, keyID string }{{"challenge-b", "active-key"}, {"challenge-a", "old-key"}} {
		if _, err = cipher.Open(test.challengeID, test.keyID, envelope); !errors.Is(err, ErrInvalidEnvelope) {
			t.Fatalf("Open(%q, %q) error = %v", test.challengeID, test.keyID, err)
		}
	}
}

func TestNewRequiresAES256Key(t *testing.T) {
	if _, err := New(make([]byte, 16), "v1"); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("New() error = %v", err)
	}
}
