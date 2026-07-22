package deliverycrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

var ErrInvalidEnvelope = errors.New("некорректный зашифрованный payload доставки")

type Payload struct {
	RecipientEmail   string `json:"recipientEmail"`
	VerificationCode string `json:"verificationCode"`
}

type Decryptor interface {
	Open(challengeID, keyID string, envelope []byte) (Payload, error)
}

type Cipher struct {
	aead  cipher.AEAD
	keyID string
}

func New(key []byte, keyID string) (*Cipher, error) {
	if len(key) != 32 {
		return nil, ErrInvalidEnvelope
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrInvalidEnvelope
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrInvalidEnvelope
	}
	return &Cipher{aead: aead, keyID: strings.TrimSpace(keyID)}, nil
}

func (c *Cipher) Open(challengeID, keyID string, envelope []byte) (Payload, error) {
	if c == nil || c.aead == nil || challengeID == "" || (keyID != "" && keyID != c.keyID) ||
		len(envelope) <= c.aead.NonceSize()+c.aead.Overhead() {
		return Payload{}, ErrInvalidEnvelope
	}
	nonceSize := c.aead.NonceSize()
	plaintext, err := c.aead.Open(nil, envelope[:nonceSize], envelope[nonceSize:], []byte(challengeID))
	if err != nil {
		return Payload{}, ErrInvalidEnvelope
	}
	defer clear(plaintext)
	var payload Payload
	if err = json.Unmarshal(plaintext, &payload); err != nil {
		return Payload{}, ErrInvalidEnvelope
	}
	return payload, nil
}

// Seal exists so producers and contract tests can use exactly the same
// nonce||ciphertext envelope. Academy implements the same wire contract in its
// own module and does not import this internal package.
func (c *Cipher) Seal(challengeID string, payload Payload) ([]byte, error) {
	if c == nil || c.aead == nil || challengeID == "" {
		return nil, ErrInvalidEnvelope
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrInvalidEnvelope
	}
	defer clear(plaintext)
	nonce := make([]byte, c.aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, ErrInvalidEnvelope
	}
	return c.aead.Seal(nonce, nonce, plaintext, []byte(challengeID)), nil
}
