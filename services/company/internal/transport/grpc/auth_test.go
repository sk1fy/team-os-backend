package grpc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestActorUsesVerifiedBearerClaims(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	userID := uuid.New()
	companyID := uuid.New()
	issuer := sharedauth.NewTokenIssuer(privateKey, "teamos-company", "teamos-api", time.Minute)
	raw, _, err := issuer.Issue(userID.String(), companyID.String(), "admin", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(nil, sharedauth.NewTokenVerifier(publicKey, "teamos-company", "teamos-api"))
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer "+raw,
		"user-id", uuid.NewString(),
		"company-id", uuid.NewString(),
	))

	actor, err := server.actor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if actor.UserID != userID || actor.CompanyID != companyID || actor.Role != "admin" {
		t.Fatalf("actor = %#v", actor)
	}
}

func TestActorRejectsMissingOrMalformedAuthorization(t *testing.T) {
	server := &Server{verifier: rejectingVerifier{}}
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{name: "metadata отсутствуют", ctx: context.Background()},
		{name: "схема не Bearer", ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Basic value"))},
		{name: "пустой Bearer", ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer"))},
		{name: "токен отклонён", ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer invalid"))},
		{name: "несколько заголовков", ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer one", "authorization", "Bearer two"))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := server.actor(test.ctx)
			if code := status.Code(err); code != codes.Unauthenticated {
				t.Fatalf("code = %v, want %v; err = %v", code, codes.Unauthenticated, err)
			}
		})
	}
}

func TestProtectedRPCRejectsRequestWithoutBearerBeforeApplicationCall(t *testing.T) {
	server := &Server{verifier: rejectingVerifier{}}
	_, err := server.GetCompany(context.Background(), &companyv1.GetCompanyRequest{})
	if code := status.Code(err); code != codes.Unauthenticated {
		t.Fatalf("code = %v, want %v; err = %v", code, codes.Unauthenticated, err)
	}
}

func TestSessionMetaUsesForwardedClientData(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"x-user-agent", "TeamOS Browser/1.0",
		"user-agent", "grpc-go/1.74",
		"x-client-ip", "203.0.113.7",
	))
	meta := sessionMeta(ctx)
	if meta.UserAgent != "TeamOS Browser/1.0" || meta.IPAddress != "203.0.113.7" {
		t.Fatalf("meta = %#v", meta)
	}
}

type rejectingVerifier struct{}

func (rejectingVerifier) Verify(string) (*sharedauth.Claims, error) {
	return nil, sharedauth.ErrInvalidToken
}
