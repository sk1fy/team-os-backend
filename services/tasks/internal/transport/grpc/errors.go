package grpc

import (
	"context"
	"errors"

	"github.com/sk1fy/team-os-backend/services/tasks/internal/application"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func transportError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, "Запрос отменён")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "Истекло время ожидания ответа")
	}

	var applicationError *application.Error
	if !errors.As(err, &applicationError) {
		return status.Error(codes.Internal, "Внутренняя ошибка сервиса")
	}

	var code codes.Code
	message := applicationError.Message
	switch applicationError.Kind {
	case application.ErrorValidation:
		code = codes.InvalidArgument
	case application.ErrorUnauthenticated:
		code = codes.Unauthenticated
	case application.ErrorForbidden:
		code = codes.PermissionDenied
	case application.ErrorNotFound:
		code = codes.NotFound
	case application.ErrorConflict:
		code = codes.Aborted
	case application.ErrorInternal:
		return status.Error(codes.Internal, "Внутренняя ошибка сервиса")
	default:
		return status.Error(codes.Internal, "Внутренняя ошибка сервиса")
	}
	if message == "" {
		message = "Не удалось выполнить запрос"
	}
	return status.Error(code, message)
}

func invalidArgument(message string) error {
	return status.Error(codes.InvalidArgument, message)
}