package application

import "fmt"

type ErrorKind uint8

const (
	ErrorValidation ErrorKind = iota + 1
	ErrorUnauthenticated
	ErrorForbidden
	ErrorNotFound
	ErrorConflict
	ErrorUpstream
	ErrorInternal
)

const (
	ErrorCodeAmoAccountInvalid         = "AMO_ACCOUNT_INVALID"
	ErrorCodeAmoAccountAlreadyExists   = "AMO_ACCOUNT_ALREADY_EXISTS"
	ErrorCodeRegistrationTokenInvalid  = "REGISTRATION_TOKEN_INVALID"
	ErrorCodeRegistrationTokenExpired  = "REGISTRATION_TOKEN_EXPIRED"
	ErrorCodeRegistrationTokenConsumed = "REGISTRATION_TOKEN_CONSUMED"
	ErrorCodeRegistrationTokenRevoked  = "REGISTRATION_TOKEN_REVOKED"
)

// Error carries a stable user-facing Russian message independently of the
// transport used to deliver it.
type Error struct {
	Kind    ErrorKind
	Code    string
	Details map[string]string
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

func invalidAccessLink() error {
	return &Error{Kind: ErrorUnauthenticated, Message: "Ссылка доступа недействительна"}
}

func coded(kind ErrorKind, code, message string) error {
	return &Error{Kind: kind, Code: code, Message: message}
}

func registrationTokenInvalid() error {
	return coded(ErrorNotFound, ErrorCodeRegistrationTokenInvalid, "Токен регистрации недействителен")
}

func registrationTokenExpired() error {
	return coded(ErrorValidation, ErrorCodeRegistrationTokenExpired, "Срок действия токена регистрации истёк")
}

func registrationTokenConsumed() error {
	return coded(ErrorConflict, ErrorCodeRegistrationTokenConsumed, "Токен регистрации уже использован")
}

func registrationTokenRevoked() error {
	return coded(ErrorConflict, ErrorCodeRegistrationTokenRevoked, "Токен регистрации отозван")
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

func upstream(message string, cause error) error {
	return &Error{Kind: ErrorUpstream, Message: message, Cause: cause}
}

func internal(message string, cause error) error {
	return &Error{Kind: ErrorInternal, Message: message, Cause: cause}
}
