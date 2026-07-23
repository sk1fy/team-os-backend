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
	operation domain.CloneOperation
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
func (f *fakeRepo) BeginClone(_ context.Context, operation domain.CloneOperation) (domain.CloneOperation, error) {
	if f.operation.ID != uuid.Nil {
		return f.operation, nil
	}
	f.operation = operation
	return operation, nil
}
func (f *fakeRepo) StartClone(_ context.Context, _, _ uuid.UUID) (domain.CloneOperation, bool, error) {
	f.operation.State = domain.CloneInProgress
	return f.operation, true, nil
}
func (f *fakeRepo) CompleteClone(_ context.Context, operation domain.CloneOperation, files []domain.ClonedFile) (domain.CloneOperation, error) {
	operation.State = domain.CloneSucceeded
	operation.Files = files
	f.operation = operation
	return operation, nil
}
func (f *fakeRepo) FailClone(_ context.Context, _, _ uuid.UUID, message string) (domain.CloneOperation, error) {
	f.operation.State = domain.CloneFailed
	f.operation.ErrorMessage = message
	return f.operation, nil
}

type fakeObjects struct {
	put     []byte
	removed bool
	copies  [][2]string
}

func (f *fakeObjects) Put(_ context.Context, _ string, r io.Reader, _ int64, _ string) error {
	f.put, _ = io.ReadAll(r)
	return nil
}
func (f *fakeObjects) Copy(_ context.Context, source, target string) error {
	f.copies = append(f.copies, [2]string{source, target})
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
	_, err := s.Upload(context.Background(), uuid.New(), uuid.New(), "owner", domain.Upload{Name: "x.png", ContentType: "image/png", Size: 3, Purpose: domain.PurposeAttachment}, bytes.NewBufferString("png"))
	if err != nil || string(objects.put) != "png" {
		t.Fatalf("upload: %v %q", err, objects.put)
	}
	repo.createErr = errors.New("db")
	_, err = s.Upload(context.Background(), uuid.New(), uuid.New(), "owner", domain.Upload{Name: "x.png", ContentType: "image/png", Size: 3, Purpose: domain.PurposeAttachment}, bytes.NewBufferString("png"))
	if err == nil || !objects.removed {
		t.Fatal("object must be removed after metadata failure")
	}
}

func TestEmployeeCannotUpload(t *testing.T) {
	objects := &fakeObjects{}
	repo := &fakeRepo{}
	service := New(repo, objects, 100, map[string]struct{}{"image/png": {}}, time.Minute)

	_, err := service.Upload(context.Background(), uuid.New(), uuid.New(), "employee", domain.Upload{
		Name: "x.png", ContentType: "image/png", Size: 3, Purpose: domain.PurposeAttachment,
	}, bytes.NewBufferString("png"))
	if !errors.Is(err, ErrUploadForbidden) {
		t.Fatalf("Upload() error = %v, want %v", err, ErrUploadForbidden)
	}
	if len(objects.put) != 0 || repo.file.ID != uuid.Nil {
		t.Fatal("запрещённая загрузка не должна обращаться к хранилищам")
	}
}

func TestDeleteAuthorization(t *testing.T) {
	uploaderID := uuid.New()
	repo := &fakeRepo{file: domain.File{UploadedBy: uploaderID, ObjectKey: "object"}}

	for _, test := range []struct {
		name    string
		userID  uuid.UUID
		role    string
		wantErr error
	}{
		{name: "uploader", userID: uploaderID},
		{name: "owner", userID: uuid.New(), role: "owner"},
		{name: "admin", userID: uuid.New(), role: "admin"},
		{name: "other user", userID: uuid.New(), role: "employee", wantErr: ErrForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			objects := &fakeObjects{}
			service := New(repo, objects, 100, nil, time.Minute)
			err := service.Delete(context.Background(), uuid.New(), test.userID, test.role, uuid.New())
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Delete() error = %v, want %v", err, test.wantErr)
			}
			if objects.removed != (test.wantErr == nil) {
				t.Fatalf("object removed = %v", objects.removed)
			}
		})
	}
}

func TestCloneFilesForOwnerIsIdempotent(t *testing.T) {
	companyID, userID, sourceID := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeRepo{file: domain.File{
		ID: sourceID, CompanyID: companyID, UploadedBy: userID,
		ObjectKey: "source", Name: "manual.pdf", ContentType: "application/pdf",
		Size: 12, Purpose: domain.PurposeAttachment,
	}}
	objects := &fakeObjects{}
	service := New(repo, objects, 100, nil, time.Minute)
	target := domain.FileOwner{Type: domain.OwnerCourseVersion, ID: uuid.New()}

	first, err := service.CloneFilesForOwner(context.Background(), companyID, userID, "admin", "copy-1", target, []uuid.UUID{sourceID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CloneFilesForOwner(context.Background(), companyID, userID, "admin", "copy-1", target, []uuid.UUID{sourceID})
	if err != nil {
		t.Fatal(err)
	}
	if first.State != domain.CloneSucceeded || second.State != domain.CloneSucceeded || len(objects.copies) != 1 {
		t.Fatalf("first=%+v second=%+v copies=%d", first, second, len(objects.copies))
	}
}

func TestCloneFilesForOwnerRejectsKeyReuse(t *testing.T) {
	companyID, userID, sourceID := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeRepo{file: domain.File{ID: sourceID, CompanyID: companyID, ObjectKey: "source"}}
	service := New(repo, &fakeObjects{}, 100, nil, time.Minute)
	firstTarget := domain.FileOwner{Type: domain.OwnerCourseVersion, ID: uuid.New()}
	if _, err := service.CloneFilesForOwner(context.Background(), companyID, userID, "partner", "copy-1", firstTarget, []uuid.UUID{sourceID}); err != nil {
		t.Fatal(err)
	}
	_, err := service.CloneFilesForOwner(context.Background(), companyID, userID, "partner", "copy-1", domain.FileOwner{Type: domain.OwnerCourseVersion, ID: uuid.New()}, []uuid.UUID{sourceID})
	if !errors.Is(err, ErrIdempotencyKey) {
		t.Fatalf("error = %v, want %v", err, ErrIdempotencyKey)
	}
}
