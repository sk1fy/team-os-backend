package httpx

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type internalRPCError struct{ cause error }

func (e *internalRPCError) Error() string { return e.GRPCStatus().Err().Error() }
func (e *internalRPCError) Unwrap() error { return e.cause }
func (*internalRPCError) GRPCStatus() *status.Status {
	return status.New(codes.Internal, "Внутренняя ошибка сервиса")
}

// InternalRPCError retains a private cause while exposing only a safe gRPC status.
func InternalRPCError(cause error) error { return &internalRPCError{cause: cause} }

// LogRPCErrors records internal causes before gRPC serializes their public status.
func LogRPCErrors(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		response, err := handler(ctx, request)
		var internal *internalRPCError
		if errors.As(err, &internal) {
			attrs := []any{"method", info.FullMethod, "error", internal.cause}
			if md, ok := metadata.FromIncomingContext(ctx); ok {
				if ids := md.Get("x-request-id"); len(ids) == 1 && len(ids[0]) <= 128 && !strings.ContainsAny(ids[0], "\r\n") {
					attrs = append(attrs, "request_id", ids[0])
				}
			}
			if span := trace.SpanContextFromContext(ctx); span.IsValid() {
				attrs = append(attrs, "trace_id", span.TraceID().String())
			}
			logger.ErrorContext(ctx, "internal RPC failed", attrs...)
		}
		return response, err
	}
}
