package clients

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type companyRPC struct {
	companyv1.CompanyServiceClient
	invoke func(context.Context, *companyv1.ResolvePositionUsersRequest) (*companyv1.ResolvePositionUsersResponse, error)
}

func (f companyRPC) ResolvePositionUsers(ctx context.Context, r *companyv1.ResolvePositionUsersRequest, _ ...grpc.CallOption) (*companyv1.ResolvePositionUsersResponse, error) {
	return f.invoke(ctx, r)
}
func TestCompanyResolverIdentityAndDeadline(t *testing.T) {
	companyID, positionID, userID := uuid.New(), uuid.New(), uuid.New()
	for _, token := range []string{"caller-jwt", ""} {
		t.Run(token, func(t *testing.T) {
			client := NewCompany(companyRPC{invoke: func(ctx context.Context, r *companyv1.ResolvePositionUsersRequest) (*companyv1.ResolvePositionUsersResponse, error) {
				md, _ := metadata.FromOutgoingContext(ctx)
				if token != "" {
					if got := md.Get("authorization"); len(got) != 1 || got[0] != "Bearer "+token {
						t.Fatalf("authorization=%v", got)
					}
					if len(md.Get("x-tasks-service-token")) != 0 {
						t.Fatal("service token accompanies user JWT")
					}
				} else {
					if md.Get("x-tasks-service-token")[0] != "service-secret" || md.Get("x-company-id")[0] != companyID.String() {
						t.Fatal("wrong service identity")
					}
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 2*time.Second {
					t.Fatal("missing bounded deadline")
				}
				if r.PositionId != positionID.String() {
					t.Fatal("wrong position")
				}
				return &companyv1.ResolvePositionUsersResponse{UserIds: []string{userID.String()}}, nil
			}}, "service-secret")
			ids, err := client.ResolvePositionUsers(context.Background(), companyID, token, positionID)
			if err != nil || len(ids) != 1 || ids[0] != userID {
				t.Fatalf("users=%v err=%v", ids, err)
			}
		})
	}
}
func TestCompanyResolverPreservesFailureAndRejectsInvalidIDs(t *testing.T) {
	failure := errors.New("unavailable")
	client := NewCompany(companyRPC{invoke: func(context.Context, *companyv1.ResolvePositionUsersRequest) (*companyv1.ResolvePositionUsersResponse, error) {
		return nil, failure
	}}, "service")
	if _, err := client.ResolvePositionUsers(context.Background(), uuid.New(), "", uuid.New()); !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
	client = NewCompany(companyRPC{invoke: func(context.Context, *companyv1.ResolvePositionUsersRequest) (*companyv1.ResolvePositionUsersResponse, error) {
		return &companyv1.ResolvePositionUsersResponse{UserIds: []string{"bad-id"}}, nil
	}}, "service")
	if _, err := client.ResolvePositionUsers(context.Background(), uuid.New(), "", uuid.New()); err == nil {
		t.Fatal("invalid owner response accepted")
	}
}
