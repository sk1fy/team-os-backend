package grpc

import (
	"context"
	"testing"

	"github.com/google/uuid"
	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDistributionRuntimeRequiresVerifiedBearer(t *testing.T) {
	s := &Server{verifier: rejectingVerifier{}}
	_, e := s.ReadDistributionRuntime(context.Background(), &v.ReadDistributionRuntimeRequest{Kind: "settings"})
	if status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	_, e = s.WriteDistributionRuntime(context.Background(), &v.WriteDistributionRuntimeRequest{Kind: "settings", Payload: []byte(`{"timezone":"UTC"}`)})
	if status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
}
func TestRuntimeIDsRejectNilAndMalformed(t *testing.T) {
	for _, raw := range []string{"", uuid.Nil.String(), "CRM-123"} {
		if _, e := runtimeID(raw, true); status.Code(e) != codes.InvalidArgument {
			t.Fatal(raw, e)
		}
	}
	id := uuid.New()
	if got, e := runtimeID(id.String(), true); e != nil || got != id {
		t.Fatal(got, e)
	}
	if got, e := runtimeID("", false); e != nil || got != uuid.Nil {
		t.Fatal(got, e)
	}
}
