package grpc

import (
	"context"
	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"testing"
)

func TestTaskControlRuntimeRequiresGatewayCredential(t *testing.T) {
	server := NewServer(nil, nil, "test-service-secret-that-is-long-enough")
	for _, md := range []metadata.MD{metadata.Pairs(), metadata.Pairs("x-teamos-service", "task-control", "x-teamos-service-token", "wrong"), metadata.Pairs("x-teamos-service", "provisioning", "x-teamos-service-token", "test-service-secret-that-is-long-enough")} {
		_, e := server.TaskControlConnection(metadata.NewIncomingContext(context.Background(), md), &v.TaskControlConnectionRequest{Action: v.TaskControlConnectionRequest_ACTION_EXCHANGE, Credential: "forged"})
		if status.Code(e) != codes.Unauthenticated {
			t.Fatal(e)
		}
	}
}
