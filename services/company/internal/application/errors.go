package application

import "fmt"

type ErrorKind uint8

const (
	ErrorValidation ErrorKind = iota + 1
	ErrorUnauthenticated
	ErrorForbidden
	ErrorNotFound
	ErrorConflict
	ErrorInternal
)

// Error carries a stable user-facing Russian message independently of the
// transport used to deliver it.
type Error struct {
	Kind    ErrorKind
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *Error) Unwrap() error { return e.Cause }

func validation(message string) error {
	return &Error{Kind: ErrorValidation, Message: message}
}

func unauthenticated() error {
	return &Error{Kind: ErrorUnauthenticated, Message: "Неверный email или пароль"}
}

func invalidSession() error {
	return &Error{Kind: ErrorUnauthenticated, Message: "Сессия недействительна или истекла"}
}

func forbidden(message string) error {
	return &Error{Kind: ErrorForbidden, Message: message}
}

func notFound(entity string) error {
	return &Error{Kind: ErrorNotFound, Message: entity + " не найден"}
}

func conflict(message string) error {
	return &Error{Kind: ErrorConflict, Message: message}
}

func internal(message string, cause error) error {
	return &Error{Kind: ErrorInternal, Message: message, Cause: cause}
}
