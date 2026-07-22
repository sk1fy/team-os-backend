package grpc

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	filesv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/files/v1"
	"github.com/sk1fy/team-os-backend/services/files/internal/application"
	"github.com/sk1fy/team-os-backend/services/files/internal/domain"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type testRepo struct{ file domain.File }

func (r *testRepo) Create(_ context.Context, f domain.File) (domain.File, error) {
	f.CreatedAt = time.Now()
	r.file = f
	return f, nil
}
func (r *testRepo) Get(context.Context, uuid.UUID, uuid.UUID) (domain.File, error) {
	return r.file, nil
}
func (r *testRepo) Delete(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return r.file.ObjectKey, nil
}
func (r *testRepo) BeginClone(_ context.Context, operation domain.CloneOperation) (domain.CloneOperation, error) {
	return operation, nil
}
func (r *testRepo) StartClone(_ context.Context, _, _ uuid.UUID) (domain.CloneOperation, bool, error) {
	return domain.CloneOperation{}, false, nil
}
func (r *testRepo) CompleteClone(_ context.Context, operation domain.CloneOperation, files []domain.ClonedFile) (domain.CloneOperation, error) {
	operation.Files = files
	operation.State = domain.CloneSucceeded
	return operation, nil
}
func (r *testRepo) FailClone(_ context.Context, _, _ uuid.UUID, message string) (domain.CloneOperation, error) {
	return domain.CloneOperation{State: domain.CloneFailed, ErrorMessage: message}, nil
}

type testObjects struct{ uploaded []byte }

func (o *testObjects) Put(_ context.Context, _ string, body io.Reader, _ int64, _ string) error {
	o.uploaded, _ = io.ReadAll(body)
	return nil
}
func (*testObjects) Copy(context.Context, string, string) error { return nil }
func (*testObjects) Remove(context.Context, string) error       { return nil }
func (*testObjects) DownloadURL(context.Context, string, time.Duration) (string, error) {
	return "https://example.test", nil
}

func TestUploadFileChecksActualSize(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-user-id", uuid.NewString(), "x-company-id", uuid.NewString()))
	stream, err := client.UploadFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Send(&filesv1.UploadFileRequest{Payload: &filesv1.UploadFileRequest_Info{Info: &filesv1.UploadFileInfo{Name: "note.txt", ContentType: "text/plain", Size: 1, Purpose: filesv1.FilePurpose_FILE_PURPOSE_ATTACHMENT}}}); err != nil {
		t.Fatal(err)
	}
	if err = stream.Send(&filesv1.UploadFileRequest{Payload: &filesv1.UploadFileRequest_Chunk{Chunk: []byte("ab")}}); err != nil {
		t.Fatal(err)
	}
	_, err = stream.CloseAndRecv()
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("got %v, want InvalidArgument", err)
	}
}

func TestUploadFileAcceptsPNG(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-user-id", uuid.NewString(), "x-company-id", uuid.NewString()))
	stream, err := client.UploadFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	_ = stream.Send(&filesv1.UploadFileRequest{Payload: &filesv1.UploadFileRequest_Info{Info: &filesv1.UploadFileInfo{Name: "avatar.png", ContentType: "image/png", Size: uint64(len(png)), Purpose: filesv1.FilePurpose_FILE_PURPOSE_AVATAR}}})
	_ = stream.Send(&filesv1.UploadFileRequest{Payload: &filesv1.UploadFileRequest_Chunk{Chunk: png}})
	response, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatal(err)
	}
	if response.GetFile().GetSize() != uint64(len(png)) {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestEmployeeCannotUploadFile(t *testing.T) {
	client, cleanup := newTestClient(t)
	defer cleanup()
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"x-user-id", uuid.NewString(),
		"x-company-id", uuid.NewString(),
		"x-user-role", "employee",
	))
	stream, err := client.UploadFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	_ = stream.Send(&filesv1.UploadFileRequest{Payload: &filesv1.UploadFileRequest_Info{Info: &filesv1.UploadFileInfo{
		Name: "avatar.png", ContentType: "image/png", Size: uint64(len(png)), Purpose: filesv1.FilePurpose_FILE_PURPOSE_AVATAR,
	}}})
	_ = stream.Send(&filesv1.UploadFileRequest{Payload: &filesv1.UploadFileRequest_Chunk{Chunk: png}})
	_, err = stream.CloseAndRecv()
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("got %v, want PermissionDenied", err)
	}
}

func newTestClient(t *testing.T) (filesv1.FilesServiceClient, func()) {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpcgo.NewServer()
	service := application.New(&testRepo{}, &testObjects{}, 1024, map[string]struct{}{"text/plain": {}, "image/png": {}}, time.Minute)
	filesv1.RegisterFilesServiceServer(server, New(service, nil, true, t.TempDir()))
	go func() { _ = server.Serve(listener) }()
	conn, err := grpcgo.NewClient("passthrough:///bufnet", grpcgo.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpcgo.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	return filesv1.NewFilesServiceClient(conn), func() { _ = conn.Close(); server.Stop(); _ = listener.Close() }
}
