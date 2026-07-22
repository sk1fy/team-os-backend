package application

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"regexp"
	"testing"
)

func TestExternalTokensAreOpaqueAndKeyed(t *testing.T) {
	service := &Service{externalSecret: []byte("0123456789abcdef0123456789abcdef")}
	first, firstHash, prefix, err := service.generateExternalToken()
	if err != nil {
		t.Fatal(err)
	}
	second, secondHash, _, err := service.generateExternalToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || secureHashEqual(firstHash, secondHash) {
		t.Fatal("внешние токены повторились")
	}
	if len(firstHash) != 32 || len(prefix) != externalTokenPrefixLength || prefix != first[:externalTokenPrefixLength] {
		t.Fatalf("некорректный hash/prefix: hash=%d prefix=%q", len(firstHash), prefix)
	}
	if !secureHashEqual(firstHash, service.externalTokenHash(first)) {
		t.Fatal("hash токена нестабилен")
	}
}

func TestExternalVerificationDeliveryIsEncryptedAtRest(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	service := &Service{externalEmailKey: key}
	ciphertext, err := service.encryptExternalVerificationDelivery(
		"challenge-1", "learner@example.com", "123456",
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(ciphertext) == "" || regexp.MustCompile(`learner|123456`).Match(ciphertext) {
		t.Fatal("ciphertext раскрыл email или код")
	}
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	plain, err := aead.Open(nil, ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():], []byte("challenge-1"))
	if err != nil {
		t.Fatal(err)
	}
	var delivery externalVerificationDelivery
	if err = json.Unmarshal(plain, &delivery); err != nil ||
		delivery.RecipientEmail != "learner@example.com" || delivery.VerificationCode != "123456" {
		t.Fatalf("delivery=%+v err=%v", delivery, err)
	}
}

func TestVerificationCodeIsSixDigitsAndBoundToChallenge(t *testing.T) {
	service := &Service{externalSecret: []byte("0123456789abcdef0123456789abcdef")}
	code, hash, err := service.generateVerificationCode("challenge-1", "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) {
		t.Fatalf("код=%q", code)
	}
	if !secureHashEqual(hash, service.verificationCodeHash("challenge-1", "user@example.com", code)) {
		t.Fatal("код не проверяется")
	}
	if secureHashEqual(hash, service.verificationCodeHash("challenge-2", "user@example.com", code)) ||
		secureHashEqual(hash, service.verificationCodeHash("challenge-1", "other@example.com", code)) {
		t.Fatal("hash кода не привязан к challenge/email")
	}
}
