package application

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

const (
	externalTokenBytes         = 32
	externalTokenPrefixLength  = 10
	externalVerificationDigits = 6
)

func (s *Service) generateExternalToken() (token string, hash []byte, prefix string, err error) {
	if len(s.externalSecret) < 32 {
		return "", nil, "", errors.New("секрет внешних токенов не настроен")
	}
	random := make([]byte, externalTokenBytes)
	if _, err = rand.Read(random); err != nil {
		return "", nil, "", fmt.Errorf("создать внешний токен: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(random)
	hash = s.externalTokenHash(token)
	prefix = token[:min(externalTokenPrefixLength, len(token))]
	return token, hash, prefix, nil
}

func (s *Service) externalTokenHash(token string) []byte {
	digest := hmac.New(sha256.New, s.externalSecret)
	_, _ = digest.Write([]byte(strings.TrimSpace(token)))
	return digest.Sum(nil)
}

func (s *Service) generateVerificationCode(challengeID, normalizedEmail string) (string, []byte, error) {
	if len(s.externalSecret) < 32 {
		return "", nil, errors.New("секрет внешней верификации не настроен")
	}
	limit := big.NewInt(1_000_000)
	value, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", nil, fmt.Errorf("создать код подтверждения: %w", err)
	}
	code := fmt.Sprintf("%0*d", externalVerificationDigits, value.Int64())
	return code, s.verificationCodeHash(challengeID, normalizedEmail, code), nil
}

func (s *Service) verificationCodeHash(challengeID, normalizedEmail, code string) []byte {
	digest := hmac.New(sha256.New, s.externalSecret)
	_, _ = digest.Write([]byte("external-verification\x00" + challengeID + "\x00" + normalizedEmail + "\x00" + code))
	return digest.Sum(nil)
}

func secureHashEqual(left, right []byte) bool { return hmac.Equal(left, right) }

type externalVerificationDelivery struct {
	RecipientEmail   string `json:"recipientEmail"`
	VerificationCode string `json:"verificationCode"`
}

func (s *Service) encryptExternalVerificationDelivery(
	challengeID, recipientEmail, code string,
) ([]byte, error) {
	if len(s.externalEmailKey) != 32 {
		return nil, errors.New("ключ шифрования внешних писем не настроен")
	}
	block, err := aes.NewCipher(s.externalEmailKey)
	if err != nil {
		return nil, fmt.Errorf("создать шифр внешнего письма: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("создать AEAD внешнего письма: %w", err)
	}
	plain, err := json.Marshal(externalVerificationDelivery{
		RecipientEmail: recipientEmail, VerificationCode: code,
	})
	if err != nil {
		return nil, fmt.Errorf("сформировать внешнее письмо: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("создать nonce внешнего письма: %w", err)
	}
	return aead.Seal(nonce, nonce, plain, []byte(challengeID)), nil
}
