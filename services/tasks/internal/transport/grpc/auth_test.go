package grpc

import (
	"context"
	"testing"

	"github.com/google/uuid"
	sharedauth "github.com/sk1fy/team-os-backend/pkg/auth"
	"google.golang.org/grpc/metadata"
)

type actorVerifier struct {
	claims *sharedauth.Claims
	token  string
}

func (v *actorVerifier) Verify(token string) (*sharedauth.Claims, error) {
	v.token = token
	return v.claims, nil
}
func TestActorCarriesVerifiedTokenToCompanyBoundary(t *testing.T) {
	verifier := &actorVerifier{claims: &sharedauth.Claims{CompanyID: uuid.NewString(), Role: "admin"}}
	verifier.claims.Subject = uuid.NewString()
	server := &Server{verifier: verifier}
	actor, err := server.actor(metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer caller-token")))
	if err != nil || actor.Token != "caller-token" || verifier.token != actor.Token {
		t.Fatalf("token was not forwarded: %v", err)
	}
}
