package grpc

import (
	"context"
	"testing"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestTasksServiceIdentityIsLimitedToPositionResolution(t *testing.T) {
	companyID := uuid.New()
	server := (&Server{}).WithTasksServiceToken("tasks-secret")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-tasks-service-token", "tasks-secret", "x-company-id", companyID.String()))
	actor, err := server.positionResolverActor(ctx)
	if err != nil || actor.CompanyID != companyID {
		t.Fatalf("actor=%+v err=%v", actor, err)
	}
	if _, err := server.ResolveDepartmentUsers(ctx, &companyv1.ResolveDepartmentUsersRequest{DepartmentId: uuid.New().String()}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("service credential authorized another RPC: %v", err)
	}
	for _, pairs := range [][]string{
		{"x-tasks-service-token", "wrong", "x-company-id", companyID.String()},
		{"x-tasks-service-token", "tasks-secret"},
		{"x-tasks-service-token", "tasks-secret", "x-company-id", "invalid"},
		{"x-tasks-service-token", "tasks-secret", "x-company-id", uuid.Nil.String()},
		{"x-tasks-service-token", "tasks-secret", "x-tasks-service-token", "tasks-secret", "x-company-id", companyID.String()},
	} {
		if _, err := server.positionResolverActor(metadata.NewIncomingContext(context.Background(), metadata.Pairs(pairs...))); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("invalid service identity accepted: %v", err)
		}
	}
}
