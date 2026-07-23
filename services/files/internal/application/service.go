package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/files/internal/domain"
)

var (
	ErrNotFound        = errors.New("Файл не найден")
	ErrForbidden       = errors.New("Недостаточно прав для удаления файла")
	ErrUploadForbidden = errors.New("Недостаточно прав для загрузки файла")
	ErrCloneForbidden  = errors.New("Недостаточно прав для копирования файлов")
	ErrInvalidClone    = errors.New("Некорректные параметры копирования файлов")
	ErrIdempotencyKey  = errors.New("Ключ идемпотентности уже использован с другими параметрами")
)

type Repository interface {
	Create(context.Context, domain.File) (domain.File, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (domain.File, error)
	Delete(context.Context, uuid.UUID, uuid.UUID) (string, error)
	BeginClone(context.Context, domain.CloneOperation) (domain.CloneOperation, error)
	StartClone(context.Context, uuid.UUID, uuid.UUID) (domain.CloneOperation, bool, error)
	CompleteClone(context.Context, domain.CloneOperation, []domain.ClonedFile) (domain.CloneOperation, error)
	FailClone(context.Context, uuid.UUID, uuid.UUID, string) (domain.CloneOperation, error)
}

type ObjectStore interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Copy(context.Context, string, string) error
	Remove(context.Context, string) error
	DownloadURL(context.Context, string, time.Duration) (string, error)
}

// CloneFilesForOwner creates immutable physical copies for another aggregate.
// The operation can safely be retried with the same key and exact parameters.
func (s *Service) CloneFilesForOwner(
	ctx context.Context,
	companyID, userID uuid.UUID,
	role, idempotencyKey string,
	target domain.FileOwner,
	sourceIDs []uuid.UUID,
) (domain.CloneOperation, error) {
	if role != "owner" && role != "admin" && role != "partner" {
		return domain.CloneOperation{}, ErrCloneForbidden
	}
	if err := validateCloneInput(idempotencyKey, target, sourceIDs); err != nil {
		return domain.CloneOperation{}, err
	}

	requested := domain.CloneOperation{
		ID: uuid.New(), CompanyID: companyID, RequestedBy: userID,
		IdempotencyKey: idempotencyKey, TargetOwner: target,
		SourceFileIDs: append([]uuid.UUID(nil), sourceIDs...),
		State:         domain.ClonePending,
	}
	operation, err := s.repo.BeginClone(ctx, requested)
	if err != nil {
		return domain.CloneOperation{}, fmt.Errorf("создать операцию копирования: %w", err)
	}
	if operation.TargetOwner != target || !equalUUIDs(operation.SourceFileIDs, sourceIDs) {
		return domain.CloneOperation{}, ErrIdempotencyKey
	}
	if operation.State == domain.CloneSucceeded || operation.State == domain.CloneInProgress {
		return operation, nil
	}
	operation, started, err := s.repo.StartClone(ctx, companyID, operation.ID)
	if err != nil {
		return domain.CloneOperation{}, fmt.Errorf("запустить операцию копирования: %w", err)
	}
	if !started {
		return operation, nil
	}

	clones := make([]domain.ClonedFile, 0, len(sourceIDs))
	copiedKeys := make([]string, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		source, getErr := s.repo.Get(ctx, companyID, sourceID)
		if getErr != nil {
			return s.failClone(ctx, operation, copiedKeys, "Исходный файл не найден", getErr)
		}
		targetID := uuid.NewSHA1(operation.ID, []byte(sourceID.String()))
		targetKey := fmt.Sprintf("%s/clones/%s/%s", companyID, operation.ID, targetID)
		if copyErr := s.objects.Copy(ctx, source.ObjectKey, targetKey); copyErr != nil {
			return s.failClone(ctx, operation, copiedKeys, "Не удалось скопировать файл", copyErr)
		}
		copiedKeys = append(copiedKeys, targetKey)
		clones = append(clones, domain.ClonedFile{
			SourceFileID: sourceID,
			File: domain.File{
				ID: targetID, CompanyID: companyID, UploadedBy: userID,
				ObjectKey: targetKey, Name: source.Name, ContentType: source.ContentType,
				Size: source.Size, Purpose: source.Purpose,
			},
		})
	}
	completed, err := s.repo.CompleteClone(ctx, operation, clones)
	if err != nil {
		return s.failClone(ctx, operation, copiedKeys, "Не удалось сохранить копии файлов", err)
	}
	return completed, nil
}

func (s *Service) failClone(
	ctx context.Context,
	operation domain.CloneOperation,
	copiedKeys []string,
	message string,
	cause error,
) (domain.CloneOperation, error) {
	cleanupContext := context.WithoutCancel(ctx)
	for _, key := range copiedKeys {
		_ = s.objects.Remove(cleanupContext, key)
	}
	failed, err := s.repo.FailClone(cleanupContext, operation.CompanyID, operation.ID, message)
	if err != nil {
		return domain.CloneOperation{}, fmt.Errorf("%s: %w", message,
			errors.Join(cause, fmt.Errorf("сохранить ошибку: %w", err)))
	}
	return failed, nil
}

func validateCloneInput(key string, target domain.FileOwner, sourceIDs []uuid.UUID) error {
	if len(key) == 0 || len(key) > 200 || target.ID == uuid.Nil || len(sourceIDs) == 0 || len(sourceIDs) > 100 {
		return ErrInvalidClone
	}
	switch target.Type {
	case domain.OwnerCourseVersion, domain.OwnerTemplateVersion, domain.OwnerArticleVersion:
	default:
		return ErrInvalidClone
	}
	seen := make(map[uuid.UUID]struct{}, len(sourceIDs))
	for _, id := range sourceIDs {
		if id == uuid.Nil {
			return ErrInvalidClone
		}
		if _, exists := seen[id]; exists {
			return ErrInvalidClone
		}
		seen[id] = struct{}{}
	}
	return nil
}

func equalUUIDs(left, right []uuid.UUID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type Service struct {
	repo    Repository
	objects ObjectStore
	maxSize int64
	allowed map[string]struct{}
	urlTTL  time.Duration
}

func New(repo Repository, objects ObjectStore, maxSize int64, allowed map[string]struct{}, urlTTL time.Duration) *Service {
	return &Service{repo: repo, objects: objects, maxSize: maxSize, allowed: allowed, urlTTL: urlTTL}
}

func (s *Service) Validate(v domain.Upload) error {
	return domain.ValidateUpload(v, s.maxSize, s.allowed)
}

func (s *Service) Upload(ctx context.Context, companyID, userID uuid.UUID, role string, v domain.Upload, body io.Reader) (domain.File, error) {
	if role == "employee" {
		return domain.File{}, ErrUploadForbidden
	}
	if err := s.Validate(v); err != nil {
		return domain.File{}, err
	}
	id := uuid.New()
	f := domain.File{ID: id, CompanyID: companyID, UploadedBy: userID, ObjectKey: fmt.Sprintf("%s/%s", companyID, id), Name: v.Name, ContentType: v.ContentType, Size: v.Size, Purpose: v.Purpose}
	if err := s.objects.Put(ctx, f.ObjectKey, body, v.Size, v.ContentType); err != nil {
		return domain.File{}, fmt.Errorf("загрузить объект: %w", err)
	}
	created, err := s.repo.Create(ctx, f)
	if err != nil {
		_ = s.objects.Remove(context.WithoutCancel(ctx), f.ObjectKey)
		return domain.File{}, fmt.Errorf("сохранить метаданные: %w", err)
	}
	return created, nil
}

func (s *Service) Get(ctx context.Context, companyID, id uuid.UUID) (domain.File, string, error) {
	// Reading is intentionally authorized at the company boundary only for now.
	// Parent-entity ACLs require an additive ownership link; see production-security.md §9.1.
	f, err := s.repo.Get(ctx, companyID, id)
	if err != nil {
		return domain.File{}, "", err
	}
	url, err := s.objects.DownloadURL(ctx, f.ObjectKey, s.urlTTL)
	if err != nil {
		return domain.File{}, "", fmt.Errorf("создать ссылку: %w", err)
	}
	return f, url, nil
}

func (s *Service) Delete(ctx context.Context, companyID, userID uuid.UUID, role string, id uuid.UUID) error {
	f, err := s.repo.Get(ctx, companyID, id)
	if err != nil {
		return err
	}
	if f.UploadedBy != userID && role != "owner" && role != "admin" {
		return ErrForbidden
	}
	if err = s.objects.Remove(ctx, f.ObjectKey); err != nil {
		return fmt.Errorf("удалить объект: %w", err)
	}
	if _, err = s.repo.Delete(ctx, companyID, id); err != nil {
		return fmt.Errorf("удалить метаданные: %w", err)
	}
	return nil
}
