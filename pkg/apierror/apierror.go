// Package apierror defines the public error shape shared by TeamOS services.
package apierror

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

const (
	// DefaultInternalMessage is deliberately safe to return to a client.
	DefaultInternalMessage = "Что-то пошло не так. Попробуйте ещё раз."
	defaultUnauthorized    = "Требуется авторизация"
	defaultForbidden       = "Недостаточно прав для выполнения операции"
)

// Error is the API-visible part of an application error. The wrapped cause is
// intentionally not serialized, so internal details never leak to a client.
type Error struct {
	Message string `json:"message"`
	Status  int    `json:"status"`

	cause error
}

// Envelope mirrors the frontend ApiError response contract.
type Envelope struct {
	Error *Error `json:"error"`
}

// New creates an API error. Invalid HTTP error statuses are converted to 500.
func New(status int, message string) *Error {
	if status < http.StatusBadRequest || status > 599 {
		status = http.StatusInternalServerError
	}

	message = strings.TrimSpace(message)
	if message == "" {
		message = defaultMessage(status)
	}

	return &Error{Message: message, Status: status}
}

// Wrap creates an API error while retaining a cause for logs and errors.Is/As.
func Wrap(cause error, status int, message string) *Error {
	err := New(status, message)
	err.cause = cause
	return err
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.Message
}

// Unwrap exposes the internal cause to error inspection without serializing it.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// BadRequest reports a validation or domain-rule error.
func BadRequest(message string) *Error {
	return New(http.StatusBadRequest, message)
}

// Unauthorized reports a missing or invalid authentication credential.
func Unauthorized(message ...string) *Error {
	return New(http.StatusUnauthorized, firstMessage(defaultUnauthorized, message))
}

// Forbidden reports that the authenticated actor lacks permission.
func Forbidden(message ...string) *Error {
	return New(http.StatusForbidden, firstMessage(defaultForbidden, message))
}

// NotFound reports a missing entity using a Russian grammatical form for the
// entity names used by TeamOS. Domain code can use New for a custom phrase.
func NotFound(entity string) *Error {
	entity = strings.TrimSpace(entity)
	if entity == "" {
		entity = "Объект"
	}
	return New(http.StatusNotFound, entity+" "+notFoundForm(entity))
}

// Conflict reports an optimistic-lock or uniqueness conflict.
func Conflict(message string) *Error {
	return New(http.StatusConflict, message)
}

// Internal returns a client-safe 500 error and optionally retains its cause.
func Internal(cause ...error) *Error {
	var wrapped error
	if len(cause) > 0 {
		wrapped = cause[0]
	}
	return Wrap(wrapped, http.StatusInternalServerError, DefaultInternalMessage)
}

// From preserves an existing API error or converts an unknown error to a safe
// internal error.
func From(err error) *Error {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return Internal(err)
}

// Write serializes err using the stable {"error": {...}} API contract.
func Write(w http.ResponseWriter, err error) {
	apiErr := From(err)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(apiErr.Status)
	_ = json.NewEncoder(w).Encode(Envelope{Error: apiErr})
}

func firstMessage(fallback string, messages []string) string {
	if len(messages) == 0 || strings.TrimSpace(messages[0]) == "" {
		return fallback
	}
	return messages[0]
}

func defaultMessage(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "Некорректный запрос"
	case http.StatusUnauthorized:
		return defaultUnauthorized
	case http.StatusForbidden:
		return defaultForbidden
	case http.StatusNotFound:
		return "Запрошенный объект не найден"
	case http.StatusConflict:
		return "Конфликт данных"
	default:
		return DefaultInternalMessage
	}
}

func notFoundForm(entity string) string {
	word := strings.ToLower(entity)

	// The soft sign is ambiguous in Russian, so keep the known masculine
	// TeamOS entity explicit and treat other soft-sign nouns as feminine.
	if word == "пользователь" {
		return "не найден"
	}
	if strings.HasSuffix(word, "ие") || strings.HasSuffix(word, "ое") || strings.HasSuffix(word, "о") {
		return "не найдено"
	}
	if strings.HasSuffix(word, "а") || strings.HasSuffix(word, "я") || strings.HasSuffix(word, "ь") {
		return "не найдена"
	}
	return "не найден"
}
