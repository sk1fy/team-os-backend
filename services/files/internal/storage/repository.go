package storage

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sk1fy/team-os-backend/services/files/internal/application"
	"github.com/sk1fy/team-os-backend/services/files/internal/domain"
	"github.com/sk1fy/team-os-backend/services/files/internal/storage/db"
)

type Repository struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool, q: db.New(pool)} }

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

func (r *Repository) BeginClone(ctx context.Context, requested domain.CloneOperation) (domain.CloneOperation, error) {
	row, err := r.q.CreateCloneOperation(ctx, db.CreateCloneOperationParams{
		ID: requested.ID, CompanyID: requested.CompanyID,
		IdempotencyKey: requested.IdempotencyKey, RequestedBy: requested.RequestedBy,
		TargetOwnerType: string(requested.TargetOwner.Type), TargetOwnerID: requested.TargetOwner.ID,
		SourceFileIds: requested.SourceFileIDs,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = r.q.GetCloneOperationByKey(ctx, db.GetCloneOperationByKeyParams{
			CompanyID: requested.CompanyID, IdempotencyKey: requested.IdempotencyKey,
		})
	}
	if err != nil {
		return domain.CloneOperation{}, err
	}
	return r.mapClone(ctx, r.q, row)
}

func (r *Repository) StartClone(ctx context.Context, companyID, id uuid.UUID) (domain.CloneOperation, bool, error) {
	row, err := r.q.StartCloneOperation(ctx, db.StartCloneOperationParams{CompanyID: companyID, ID: id})
	if err == nil {
		operation, mapErr := r.mapClone(ctx, r.q, row)
		return operation, true, mapErr
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.CloneOperation{}, false, err
	}
	row, err = r.q.GetCloneOperation(ctx, db.GetCloneOperationParams{CompanyID: companyID, ID: id})
	if err != nil {
		return domain.CloneOperation{}, false, err
	}
	operation, err := r.mapClone(ctx, r.q, row)
	return operation, false, err
}

func (r *Repository) CompleteClone(
	ctx context.Context,
	operation domain.CloneOperation,
	clones []domain.ClonedFile,
) (domain.CloneOperation, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.CloneOperation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	for index, clone := range clones {
		file := clone.File
		if _, err = queries.CreateFile(ctx, db.CreateFileParams{
			ID: file.ID, CompanyID: file.CompanyID, UploadedBy: file.UploadedBy,
			ObjectKey: file.ObjectKey, Name: file.Name, ContentType: file.ContentType,
			Size: file.Size, Purpose: string(file.Purpose),
		}); err != nil {
			return domain.CloneOperation{}, err
		}
		if err = queries.CreateFileClone(ctx, db.CreateFileCloneParams{
			OperationID: operation.ID, Ordinal: int32(index),
			SourceFileID: clone.SourceFileID, TargetFileID: file.ID,
		}); err != nil {
			return domain.CloneOperation{}, err
		}
	}
	row, err := queries.CompleteCloneOperation(ctx, db.CompleteCloneOperationParams{
		CompanyID: operation.CompanyID, ID: operation.ID,
	})
	if err != nil {
		return domain.CloneOperation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.CloneOperation{}, err
	}
	return cloneOperationFromDB(row, clones), nil
}

func (r *Repository) FailClone(ctx context.Context, companyID, id uuid.UUID, message string) (domain.CloneOperation, error) {
	row, err := r.q.FailCloneOperation(ctx, db.FailCloneOperationParams{
		CompanyID: companyID, ID: id,
		ErrorMessage: pgtype.Text{String: message, Valid: true},
	})
	if err != nil {
		return domain.CloneOperation{}, err
	}
	return r.mapClone(ctx, r.q, row)
}

func (r *Repository) mapClone(ctx context.Context, queries *db.Queries, row db.FileCloneOperation) (domain.CloneOperation, error) {
	cloneRows, err := queries.ListFileClones(ctx, row.ID)
	if err != nil {
		return domain.CloneOperation{}, err
	}
	clones := make([]domain.ClonedFile, 0, len(cloneRows))
	for _, clone := range cloneRows {
		clones = append(clones, domain.ClonedFile{
			SourceFileID: clone.SourceFileID,
			File: domain.File{
				ID: clone.TargetFileID, CompanyID: clone.CompanyID, UploadedBy: clone.UploadedBy,
				ObjectKey: clone.ObjectKey, Name: clone.Name, ContentType: clone.ContentType,
				Size: clone.Size, Purpose: domain.Purpose(clone.Purpose), CreatedAt: clone.CreatedAt.Time,
			},
		})
	}
	return cloneOperationFromDB(row, clones), nil
}

func cloneOperationFromDB(row db.FileCloneOperation, clones []domain.ClonedFile) domain.CloneOperation {
	errorMessage := ""
	if row.ErrorMessage.Valid {
		errorMessage = row.ErrorMessage.String
	}
	createdAt, updatedAt := time.Time{}, time.Time{}
	if row.CreatedAt.Valid {
		createdAt = row.CreatedAt.Time
	}
	if row.UpdatedAt.Valid {
		updatedAt = row.UpdatedAt.Time
	}
	return domain.CloneOperation{
		ID: row.ID, CompanyID: row.CompanyID, IdempotencyKey: row.IdempotencyKey,
		RequestedBy:   row.RequestedBy,
		TargetOwner:   domain.FileOwner{Type: domain.OwnerType(row.TargetOwnerType), ID: row.TargetOwnerID},
		SourceFileIDs: append([]uuid.UUID(nil), row.SourceFileIds...),
		State:         domain.CloneState(row.State), Files: clones, ErrorMessage: errorMessage,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
}
func mapFile(v db.File) domain.File {
	return domain.File{ID: v.ID, CompanyID: v.CompanyID, UploadedBy: v.UploadedBy, ObjectKey: v.ObjectKey, Name: v.Name, ContentType: v.ContentType, Size: v.Size, Purpose: domain.Purpose(v.Purpose), CreatedAt: v.CreatedAt.Time}
}
