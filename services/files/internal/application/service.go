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
)

type Repository interface {
	Create(context.Context, domain.File) (domain.File, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (domain.File, error)
	Delete(context.Context, uuid.UUID, uuid.UUID) (string, error)
}

type ObjectStore interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Remove(context.Context, string) error
	DownloadURL(context.Context, string, time.Duration) (string, error)
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
