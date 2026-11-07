package clients

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	filesv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/files/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type cloneClient struct {
	filesv1.FilesServiceClient
	t                 *testing.T
	companyID, userID uuid.UUID
	calls             int
}

func (c *cloneClient) CloneFilesForOwner(ctx context.Context, req *filesv1.CloneFilesForOwnerRequest, _ ...grpc.CallOption) (*filesv1.CloneFilesForOwnerResponse, error) {
	c.calls++
	md, _ := metadata.FromOutgoingContext(ctx)
	for key, want := range map[string]string{
		"x-academy-service-token": "test-service-credential",
		"x-company-id":            c.companyID.String(),
		"x-user-id":               c.userID.String(),
		"x-user-role":             "partner",
	} {
		if got := md.Get(key); len(got) != 1 || got[0] != want {
			c.t.Errorf("%s=%v, want exact authenticated context", key, got)
		}
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
		c.t.Error("clone RPC must have a bounded context")
	}
	if req.GetIdempotencyKey() != "clone-job" {
		c.t.Error("clone retry identity was not preserved")
	}
	return &filesv1.CloneFilesForOwnerResponse{State: filesv1.FileCloneState_FILE_CLONE_STATE_SUCCEEDED}, nil
}

func TestFilesCloneRequiresAndSendsServiceIdentity(t *testing.T) {
	c := &cloneClient{t: t, companyID: uuid.New(), userID: uuid.New()}
	for _, token := range []string{"", "test-service-credential"} {
		client := NewFiles(c, token)
		_, err := client.CloneFilesForOwner(context.Background(), c.companyID, c.userID, "partner", "clone-job", "course_version", uuid.New(), []uuid.UUID{uuid.New()})
		if token == "" && err == nil {
			t.Fatal("missing credential must fail before calling Files")
		}
		if token != "" && err != nil {
			t.Fatal(err)
		}
	}
	if c.calls != 1 {
		t.Fatalf("Files calls=%d, want only the authenticated call", c.calls)
	}
}
