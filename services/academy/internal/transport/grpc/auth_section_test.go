package grpc

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type sectionClaimsVerifier struct {
	claims *sharedauth.Claims
}

func (v sectionClaimsVerifier) Verify(string) (*sharedauth.Claims, error) {
	return v.claims, nil
}

func TestEmployeeAcademySectionIsRequired(t *testing.T) {
	userID, companyID := uuid.New(), uuid.New()
	claims := &sharedauth.Claims{
		CompanyID: companyID.String(), Role: "employee",
		RegisteredClaims: jwt.RegisteredClaims{Subject: userID.String()},
	}
	server := &Server{verifier: sectionClaimsVerifier{claims: claims}}
	ctx := metadata.NewIncomingContext(
		context.Background(), metadata.Pairs("authorization", "Bearer token"),
	)
	if _, err := server.actor(ctx); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("actor() error = %v, want permission denied", err)
	}
	claims.SectionAccess = []string{"academy"}
	actor, err := server.actor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(actor.SectionAccess) != 1 || actor.SectionAccess[0] != "academy" {
		t.Fatalf("actor = %#v", actor)
	}
}
