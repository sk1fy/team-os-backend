package grpc

import (
	"errors"
	"testing"

	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTransportErrorMapsApplicationKinds(t *testing.T) {
	tests := []struct {
		name string
		kind application.ErrorKind
		code codes.Code
	}{
		{name: "validation", kind: application.ErrorValidation, code: codes.InvalidArgument},
		{name: "unauthenticated", kind: application.ErrorUnauthenticated, code: codes.Unauthenticated},
		{name: "forbidden", kind: application.ErrorForbidden, code: codes.PermissionDenied},
		{name: "not found", kind: application.ErrorNotFound, code: codes.NotFound},
		{name: "conflict", kind: application.ErrorConflict, code: codes.AlreadyExists},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := transportError(&application.Error{Kind: test.kind, Message: "Безопасное сообщение"})
			if code := status.Code(err); code != test.code {
				t.Fatalf("code = %v, want %v", code, test.code)
			}
			if message := status.Convert(err).Message(); message != "Безопасное сообщение" {
				t.Fatalf("message = %q", message)
			}
		})
	}
}

func TestTransportErrorDoesNotExposeInternalCause(t *testing.T) {
	secret := "password=do-not-leak"
	tests := []error{
		&application.Error{Kind: application.ErrorInternal, Message: "Ошибка БД", Cause: errors.New(secret)},
		errors.New(secret),
	}
	for _, source := range tests {
		err := transportError(source)
		if code := status.Code(err); code != codes.Internal {
			t.Fatalf("code = %v, want %v", code, codes.Internal)
		}
		if message := status.Convert(err).Message(); message != "Внутренняя ошибка сервиса" {
			t.Fatalf("message = %q", message)
		}
	}
}
