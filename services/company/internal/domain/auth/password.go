package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory               = 64 * 1024
	argonIterations           = 3
	argonParallelism          = 2
	argonSaltLength           = 16
	argonKeyLength            = 32
	generatedPasswordLength   = 14
	generatedPasswordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
)

var (
	ErrInvalidPasswordHash = errors.New("некорректный хэш пароля")
	ErrPasswordTooShort    = errors.New("Пароль должен содержать не менее 8 символов")
	ErrPasswordTooLong     = errors.New("Пароль должен содержать не более 256 символов")
)

func GeneratePassword() (string, error) {
	alphabetLength := big.NewInt(int64(len(generatedPasswordAlphabet)))
	password := make([]byte, generatedPasswordLength)
	for index := range password {
		alphabetIndex, err := rand.Int(rand.Reader, alphabetLength)
		if err != nil {
			return "", fmt.Errorf("сгенерировать пароль: %w", err)
		}
		password[index] = generatedPasswordAlphabet[alphabetIndex.Int64()]
	}
	return string(password), nil
}

func HashPassword(password string) (string, error) {
	passwordLength := len([]rune(password))
	if passwordLength < 8 {
		return "", ErrPasswordTooShort
	}
	if passwordLength > 256 {
		return "", ErrPasswordTooLong
	}
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("сгенерировать salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonIterations,
		argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func VerifyPassword(password, encoded string) (bool, error) {
	if len([]rune(password)) > 256 {
		return false, nil
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrInvalidPasswordHash
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || version != argon2.Version {
		return false, ErrInvalidPasswordHash
	}
	var memory uint32
	var iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, ErrInvalidPasswordHash
	}
	if memory == 0 || iterations == 0 || parallelism == 0 || memory > 1024*1024 || iterations > 20 {
		return false, ErrInvalidPasswordHash
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return false, ErrInvalidPasswordHash
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return false, ErrInvalidPasswordHash
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
