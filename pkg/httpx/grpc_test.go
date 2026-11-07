package httpx

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestRPCInternalCauseIsLoggedButNotReturned(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	cause := errors.New("database connection unavailable")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-request-id", "request-17"))
	_, err := LogRPCErrors(logger)(ctx, map[string]string{"token": "never-log-request"}, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Read"}, func(context.Context, any) (any, error) {
		return nil, InternalRPCError(cause)
	})
	if !errors.Is(err, cause) || status.Code(err) != codes.Internal || strings.Contains(err.Error(), cause.Error()) {
		t.Fatalf("internal cause/status handling error=%v", err)
	}
	for _, want := range []string{cause.Error(), "request-17", "/test.Service/Read"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing diagnostic %q: %s", want, &output)
		}
	}
	if strings.Contains(output.String(), "never-log-request") {
		t.Fatal("RPC request was logged")
	}
	output.Reset()
	_, _ = LogRPCErrors(logger)(ctx, nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
		return nil, status.Error(codes.InvalidArgument, "invalid")
	})
	if output.Len() != 0 {
		t.Fatal("expected validation error was logged as an internal failure")
	}
}
