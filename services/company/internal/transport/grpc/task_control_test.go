package grpc

import (
	"context"
	"testing"

	"github.com/sk1fy/team-os-backend/services/company/internal/application"

	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestTaskControlRejectsBrowserAuthority(t *testing.T) {
	for _, body := range []string{`{"companyId":"forged"}`, `{"isAdmin":true}`, `{"role":"owner"}`} {
		for _, out := range []any{&application.TaskControlLinkInput{}, &application.TaskControlModeInput{}, &application.TaskControlRevokeInput{}, &application.TaskControlDraftInput{}} {
			if err := taskControlDecode([]byte(body), out); err == nil {
				t.Fatalf("accepted authority: %s", body)
			}
		}
	}
}

func TestTaskControlRuntimeRequiresGatewayCredential(t *testing.T) {
	server := NewServer(nil, nil, "test-service-secret-that-is-long-enough")
	for _, md := range []metadata.MD{metadata.Pairs(), metadata.Pairs("x-teamos-service", "task-control", "x-teamos-service-token", "wrong"), metadata.Pairs("x-teamos-service", "provisioning", "x-teamos-service-token", "test-service-secret-that-is-long-enough")} {
		_, e := server.TaskControlConnection(metadata.NewIncomingContext(context.Background(), md), &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_EXCHANGE, Credential: "forged"})
		if status.Code(e) != codes.Unauthenticated {
			t.Fatal(e)
		}
	}
}

func TestTaskControlTelemetryRequiresGatewayServiceCredential(t *testing.T) {
	server := NewServer(nil, nil, "test-service-secret-that-is-long-enough")
	for _, action := range []v.TaskControlConnectionRequest_Action{v.TaskControlConnectionRequest_ACTION_ACK, v.TaskControlConnectionRequest_ACTION_ACK_V2, v.TaskControlConnectionRequest_ACTION_TELEMETRY} {
		_, err := server.TaskControlConnection(context.Background(), &v.TaskControlConnectionRequest{Action: action, Credential: "forged"})
		if status.Code(err) != codes.Unauthenticated {
			t.Fatal(action, err)
		}
	}
}

func TestTaskControlTelemetryRejectsFreeTextBeforeApplication(t *testing.T) {
	server := NewServer(nil, nil, "test-service-secret-that-is-long-enough")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-teamos-service", "task-control", "x-teamos-service-token", "test-service-secret-that-is-long-enough"))
	for _, body := range []string{`{"message":"sensitive text"}`, `{"rules":[]}`, `{"assertion":"secret"}`} {
		_, err := server.TaskControlConnection(ctx, &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_TELEMETRY, Credential: "opaque-runtime", JsonBody: []byte(body)})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatal(body, err)
		}
	}
}
