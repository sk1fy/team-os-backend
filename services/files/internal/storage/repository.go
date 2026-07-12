package storage

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/services/files/internal/application"
	"github.com/sk1fy/team-os-backend/services/files/internal/domain"
	"github.com/sk1fy/team-os-backend/services/files/internal/storage/db"
)

type Repository struct{ q *db.Queries }

func New(pool *pgxpool.Pool) *Repository { return &Repository{q: db.New(pool)} }

func (r *Repository) Create(ctx context.Context, f domain.File) (domain.File, error) {
	row, err := r.q.CreateFile(ctx, db.CreateFileParams{ID: f.ID, CompanyID: f.CompanyID, UploadedBy: f.UploadedBy, ObjectKey: f.ObjectKey, Name: f.Name, ContentType: f.ContentType, Size: f.Size, Purpose: string(f.Purpose)})
	if err != nil {
		return domain.File{}, err
	}
	return mapFile(row), nil
}
func (r *Repository) Get(ctx context.Context, companyID, id uuid.UUID) (domain.File, error) {
	row, err := r.q.GetFile(ctx, db.GetFileParams{ID: id, CompanyID: companyID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.File{}, application.ErrNotFound
	}
	if err != nil {
		return domain.File{}, err
	}
	return mapFile(row), nil
}
func (r *Repository) Delete(ctx context.Context, companyID, id uuid.UUID) (string, error) {
	key, err := r.q.DeleteFile(ctx, db.DeleteFileParams{ID: id, CompanyID: companyID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", application.ErrNotFound
	}
	return key, err
}
func mapFile(v db.File) domain.File {
	return domain.File{ID: v.ID, CompanyID: v.CompanyID, UploadedBy: v.UploadedBy, ObjectKey: v.ObjectKey, Name: v.Name, ContentType: v.ContentType, Size: v.Size, Purpose: domain.Purpose(v.Purpose), CreatedAt: v.CreatedAt.Time}
}
