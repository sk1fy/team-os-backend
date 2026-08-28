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
	raw, _, err := issuer.Issue(
		userID.String(), companyID.String(), "employee", nil, nil,
		[]string{"schedule", "knowledge"},
	)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(nil, sharedauth.NewTokenVerifier(publicKey, "teamos-company", "teamos-api"), "gateway-service-token-at-least-32-bytes")
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer "+raw,
		"user-id", uuid.NewString(),
		"company-id", uuid.NewString(),
	))

	actor, err := server.actor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if actor.UserID != userID || actor.CompanyID != companyID || actor.Role != "employee" ||
		len(actor.SectionAccess) != 2 || actor.SectionAccess[1] != "knowledge" {
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
	_, err = server.CheckAmoSessionAccess(context.Background(), &companyv1.CheckAmoSessionAccessRequest{AmoAccountId: "31355990"})
	if code := status.Code(err); code != codes.Unauthenticated {
		t.Fatalf("amo session access code = %v, want %v; err = %v", code, codes.Unauthenticated, err)
	}
}

func TestAuthorizeProvisioningAcceptsTrustedServiceKey(t *testing.T) {
	const serviceKey = "provisioning-service-key-32-bytes-minimum"
	server := NewServer(nil, nil, serviceKey)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"x-teamos-service", "provisioning",
		"x-teamos-service-token", serviceKey,
	))

	if err := server.authorizeProvisioning(ctx); err != nil {
		t.Fatalf("authorizeProvisioning() error = %v", err)
	}
}

func TestAuthorizeProvisioningRejectsMissingOrInvalidKey(t *testing.T) {
	const serviceKey = "provisioning-service-key-32-bytes-minimum"
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{name: "metadata absent", ctx: context.Background()},
		{
			name: "wrong key",
			ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs(
				"x-teamos-service", "provisioning",
				"x-teamos-service-token", "other",
			)),
		},
		{
			name: "wrong marker",
			ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs(
				"x-teamos-service", "other",
				"x-teamos-service-token", serviceKey,
			)),
		},
		{
			name: "multiple credentials",
			ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs(
				"x-teamos-service", "provisioning",
				"x-teamos-service", "provisioning",
				"x-teamos-service-token", serviceKey,
				"x-teamos-service-token", serviceKey,
			)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if code := status.Code(NewServer(nil, nil, serviceKey).authorizeProvisioning(test.ctx)); code != codes.Unauthenticated {
				t.Fatalf("code = %v, want %v", code, codes.Unauthenticated)
			}
		})
	}
}

func TestAmoAdminSelfLoginRequiresGatewayServiceAuthentication(t *testing.T) {
	server := NewServer(nil, nil, "gateway-service-token-at-least-32-bytes")
	_, err := server.AmoAdminSelfLogin(context.Background(), &companyv1.AmoAdminSelfLoginRequest{
		AmoAccountId: "31355990", SelfUserId: "101",
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code=%v error=%v", status.Code(err), err)
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
