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
	ErrorCodeAmoAccountInvalid             = "AMO_ACCOUNT_INVALID"
	ErrorCodeAmoAccountAlreadyExists       = "AMO_ACCOUNT_ALREADY_EXISTS"
	ErrorCodeRegistrationTokenInvalid      = "REGISTRATION_TOKEN_INVALID"
	ErrorCodeRegistrationTokenExpired      = "REGISTRATION_TOKEN_EXPIRED"
	ErrorCodeRegistrationTokenConsumed     = "REGISTRATION_TOKEN_CONSUMED"
	ErrorCodeRegistrationTokenRevoked      = "REGISTRATION_TOKEN_REVOKED"
	ErrorCodeLoginReservationInvalid       = "LOGIN_RESERVATION_INVALID"
	ErrorCodeLoginReservationExpired       = "LOGIN_RESERVATION_EXPIRED"
	ErrorCodeLoginReservationConsumed      = "LOGIN_RESERVATION_CONSUMED"
	ErrorCodeAmoTokenInvalid               = "AMO_TOKEN_INVALID"
	ErrorCodeWidgetNotInstalled            = "WIDGET_NOT_INSTALLED"
	ErrorCodeWidgetNotPaid                 = "WIDGET_NOT_PAID"
	ErrorCodeAmoWidgetSessionUnavailable   = "AMO_WIDGET_SESSION_UNAVAILABLE"
	ErrorCodeAmoWidgetUserMismatch         = "AMO_WIDGET_USER_MISMATCH"
	ErrorCodeAmoWidgetContinuationInvalid  = "AMO_WIDGET_CONTINUATION_INVALID"
	ErrorCodeAmoWidgetContinuationExpired  = "AMO_WIDGET_CONTINUATION_EXPIRED"
	ErrorCodeAmoWidgetContinuationConsumed = "AMO_WIDGET_CONTINUATION_CONSUMED"
	ErrorCodeAmoWidgetPasswordInvalid      = "AMO_WIDGET_PASSWORD_INVALID"
	ErrorCodeAmoSessionAccessNotFound      = "AMO_SESSION_ACCESS_NOT_FOUND"
	ErrorCodeAmoSessionAccessMismatch      = "AMO_SESSION_ACCESS_ACCOUNT_MISMATCH"
	ErrorCodeAmoSessionAccessLocked        = "AMO_SESSION_ACCESS_LOCKED"
	ErrorCodeAmoSessionAccessForbidden     = "AMO_SESSION_ACCESS_FORBIDDEN"
	ErrorCodeAmoAdminAssertionInvalid      = "AMO_ADMIN_ASSERTION_INVALID"
	ErrorCodeAmoAdminSelfLoginForbidden    = "AMO_ADMIN_SELF_LOGIN_FORBIDDEN"
	ErrorCodeAmoAdminSelfLoginNotFound     = "AMO_ADMIN_SELF_LOGIN_NOT_FOUND"
	ErrorCodeAmoAdminSelfLoginUnavailable  = "AMO_ADMIN_SELF_LOGIN_UNAVAILABLE"
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
	return &Error{Kind: ErrorUnauthenticated, Message: "Неверный логин или пароль"}
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

func loginReservationInvalid() error {
	return coded(ErrorValidation, ErrorCodeLoginReservationInvalid, "Резервация логина недействительна")
}

func loginReservationExpired() error {
	return coded(ErrorValidation, ErrorCodeLoginReservationExpired, "Срок резервации логина истёк")
}

func loginReservationConsumed() error {
	return coded(ErrorConflict, ErrorCodeLoginReservationConsumed, "Резервация логина уже использована")
}

func amoWidgetContinuationInvalid() error {
	return coded(ErrorUnauthenticated, ErrorCodeAmoWidgetContinuationInvalid, "Ссылка входа из amoCRM недействительна")
}

func amoWidgetContinuationExpired() error {
	return coded(ErrorUnauthenticated, ErrorCodeAmoWidgetContinuationExpired, "Срок действия ссылки входа из amoCRM истёк")
}

func amoWidgetContinuationConsumed() error {
	return coded(ErrorUnauthenticated, ErrorCodeAmoWidgetContinuationConsumed, "Ссылка входа из amoCRM уже использована")
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
