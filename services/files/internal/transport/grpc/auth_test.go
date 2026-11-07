package grpc

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestMetadataCannotImpersonateFileActor(t *testing.T) {
	md := metadata.Pairs("x-user-id", uuid.NewString(), "x-company-id", uuid.NewString(), "x-user-role", "admin")
	resolver := actorResolver{academyServiceToken: "test-academy-credential"}
	ctx := metadata.NewIncomingContext(context.Background(), md)
	if _, err := resolver.actor(ctx); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unsigned metadata authorized a file operation: %v", err)
	}
	if _, err := resolver.cloneActor(ctx); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unsigned metadata authorized a clone: %v", err)
	}
	md.Set("x-academy-service-token", "wrong-credential")
	if _, err := resolver.cloneActor(metadata.NewIncomingContext(context.Background(), md)); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("wrong service credential authorized a clone: %v", err)
	}
	md.Set("x-academy-service-token", "test-academy-credential")
	ctx = metadata.NewIncomingContext(context.Background(), md)
	got, err := resolver.cloneActor(ctx)
	if err != nil || got.userID.String() != md.Get("x-user-id")[0] || got.companyID.String() != md.Get("x-company-id")[0] || got.role != "admin" {
		t.Fatalf("authenticated Academy clone actor=%+v err=%v", got, err)
	}
	if _, err = resolver.actor(ctx); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("clone credential authorized a general file operation: %v", err)
	}
	md.Set("authorization", "Bearer invalid")
	if _, err = resolver.cloneActor(metadata.NewIncomingContext(context.Background(), md)); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("invalid bearer fell back to service identity: %v", err)
	}
}
