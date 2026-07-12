package application

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/files/internal/domain"
)

type fakeRepo struct {
	createErr error
	file      domain.File
}

func (f *fakeRepo) Create(_ context.Context, v domain.File) (domain.File, error) {
	if f.createErr != nil {
		return domain.File{}, f.createErr
	}
	v.CreatedAt = time.Now()
	f.file = v
	return v, nil
}
func (f *fakeRepo) Get(context.Context, uuid.UUID, uuid.UUID) (domain.File, error) {
	return f.file, nil
}
func (f *fakeRepo) Delete(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return f.file.ObjectKey, nil
}

type fakeObjects struct {
	put     []byte
	removed bool
}

func (f *fakeObjects) Put(_ context.Context, _ string, r io.Reader, _ int64, _ string) error {
	f.put, _ = io.ReadAll(r)
	return nil
}
func (f *fakeObjects) Remove(context.Context, string) error { f.removed = true; return nil }
func (f *fakeObjects) DownloadURL(context.Context, string, time.Duration) (string, error) {
	return "https://example.test/file", nil
}

func TestUploadAndCleanup(t *testing.T) {
	objects := &fakeObjects{}
	repo := &fakeRepo{}
	s := New(repo, objects, 100, map[string]struct{}{"image/png": {}}, time.Minute)
	_, err := s.Upload(context.Background(), uuid.New(), uuid.New(), domain.Upload{Name: "x.png", ContentType: "image/png", Size: 3, Purpose: domain.PurposeAttachment}, bytes.NewBufferString("png"))
	if err != nil || string(objects.put) != "png" {
		t.Fatalf("upload: %v %q", err, objects.put)
	}
	repo.createErr = errors.New("db")
	_, err = s.Upload(context.Background(), uuid.New(), uuid.New(), domain.Upload{Name: "x.png", ContentType: "image/png", Size: 3, Purpose: domain.PurposeAttachment}, bytes.NewBufferString("png"))
	if err == nil || !objects.removed {
		t.Fatal("object must be removed after metadata failure")
	}
}
