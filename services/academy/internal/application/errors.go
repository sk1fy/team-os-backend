package application

import "fmt"

type ErrorKind uint8

const (
	ErrorValidation ErrorKind = iota + 1
	ErrorUnauthenticated
	ErrorForbidden
	ErrorNotFound
	ErrorConflict
	ErrorUnavailable
	ErrorInternal
)

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

func forbidden(message string) error {
	return &Error{Kind: ErrorForbidden, Message: message}
}

func notFound(entity string) error {
	return &Error{Kind: ErrorNotFound, Message: entity + " не найден"}
}

func conflict(message string) error {
	return &Error{Kind: ErrorConflict, Message: message}
}

func unavailable(message string, cause error) error {
	return &Error{Kind: ErrorUnavailable, Message: message, Cause: cause}
}

func internal(message string, cause error) error {
	return &Error{Kind: ErrorInternal, Message: message, Cause: cause}
}
