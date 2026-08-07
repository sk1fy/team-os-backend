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
	ErrorCodeBootstrapInvalid        = "BOOTSTRAP_INVALID"
	ErrorCodeBootstrapExpired        = "BOOTSTRAP_EXPIRED"
	ErrorCodeBootstrapConsumed       = "BOOTSTRAP_CONSUMED"
	ErrorCodeExternalUserDeactivated = "EXTERNAL_USER_DEACTIVATED"
	ErrorCodeIntegrationFrozen       = "INTEGRATION_FROZEN"
	ErrorCodeOnboardingCompleted     = "ONBOARDING_COMPLETED"
	ErrorCodeNoPendingActivation     = "NO_PENDING_ACTIVATION"
	ErrorCodeProvisioningConflict    = "PROVISIONING_CONFLICT"
	ErrorCodeSSOInvalid              = "SSO_INVALID"
	ErrorCodeSSOExpired              = "SSO_EXPIRED"
	ErrorCodeSSOConsumed             = "SSO_CONSUMED"
	ErrorCodePendingBootstrapLocked  = "PENDING_BOOTSTRAP_USER_LOCKED"
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

func bootstrapInvalid() error {
	return coded(ErrorNotFound, ErrorCodeBootstrapInvalid, "Ссылка активации недействительна")
}

func bootstrapExpired() error {
	return coded(ErrorValidation, ErrorCodeBootstrapExpired, "Срок действия ссылки активации истёк")
}

func bootstrapConsumed() error {
	return coded(ErrorConflict, ErrorCodeBootstrapConsumed, "Ссылка активации уже использована")
}

func ssoInvalid() error {
	return coded(ErrorUnauthenticated, ErrorCodeSSOInvalid, "Ссылка входа недействительна")
}

func ssoExpired() error {
	return coded(ErrorUnauthenticated, ErrorCodeSSOExpired, "Срок действия ссылки входа истёк")
}

func ssoConsumed() error {
	return coded(ErrorUnauthenticated, ErrorCodeSSOConsumed, "Ссылка входа уже использована")
}

func integrationFrozen() error {
	return coded(ErrorForbidden, ErrorCodeIntegrationFrozen, "Интеграция временно недоступна")
}

func externalUserDeactivated() error {
	return coded(ErrorForbidden, ErrorCodeExternalUserDeactivated, "Учётная запись внешнего пользователя деактивирована")
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
