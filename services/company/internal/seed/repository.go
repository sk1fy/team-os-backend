package seed

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

const upsertCompanySQL = `
INSERT INTO companies (id, name, logo_url, created_at)
VALUES ($1, $2, $3, COALESCE($4::timestamptz, now()))
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name,
    logo_url = EXCLUDED.logo_url,
    updated_at = now()
WHERE (companies.name, companies.logo_url)
      IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.logo_url)`

const upsertUserSQL = `
INSERT INTO users (
    id, company_id, email, first_name, last_name, phone, avatar_url,
    role, status, birth_date, hired_at, vacation_allowance, created_at
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10::date, $11::date, $12::smallint,
    COALESCE($13::timestamptz, now())
)
ON CONFLICT (id) DO UPDATE
SET company_id = EXCLUDED.company_id,
    email = EXCLUDED.email,
    first_name = EXCLUDED.first_name,
    last_name = EXCLUDED.last_name,
    phone = EXCLUDED.phone,
    avatar_url = EXCLUDED.avatar_url,
    role = EXCLUDED.role,
    status = EXCLUDED.status,
    birth_date = EXCLUDED.birth_date,
    hired_at = EXCLUDED.hired_at,
    vacation_allowance = EXCLUDED.vacation_allowance,
    updated_at = now()
WHERE (
    users.company_id, users.email, users.first_name, users.last_name,
    users.phone, users.avatar_url, users.role, users.status,
    users.birth_date, users.hired_at, users.vacation_allowance
) IS DISTINCT FROM (
    EXCLUDED.company_id, EXCLUDED.email, EXCLUDED.first_name, EXCLUDED.last_name,
    EXCLUDED.phone, EXCLUDED.avatar_url, EXCLUDED.role, EXCLUDED.status,
    EXCLUDED.birth_date, EXCLUDED.hired_at, EXCLUDED.vacation_allowance
)`

const upsertCredentialSQL = `
INSERT INTO credentials (company_id, user_id, password_hash)
VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO UPDATE
SET company_id = EXCLUDED.company_id,
    password_hash = EXCLUDED.password_hash,
    updated_at = now()`

const upsertDepartmentSQL = `
INSERT INTO departments (
    id, company_id, name, parent_id, head_user_id, valuable_final_product, "order"
)
VALUES ($1, $2, $3, NULL, $4, $5, $6)
ON CONFLICT (id) DO UPDATE
SET company_id = EXCLUDED.company_id,
    name = EXCLUDED.name,
    head_user_id = EXCLUDED.head_user_id,
    valuable_final_product = EXCLUDED.valuable_final_product,
    "order" = EXCLUDED."order",
    updated_at = now()
WHERE (
    departments.company_id, departments.name, departments.head_user_id,
    departments.valuable_final_product, departments."order"
) IS DISTINCT FROM (
    EXCLUDED.company_id, EXCLUDED.name, EXCLUDED.head_user_id,
    EXCLUDED.valuable_final_product, EXCLUDED."order"
)`

const setDepartmentParentSQL = `
UPDATE departments
SET parent_id = $3::uuid,
    updated_at = now()
WHERE id = $1
  AND company_id = $2
  AND parent_id IS DISTINCT FROM $3::uuid`

const upsertPositionSQL = `
INSERT INTO positions (
    id, company_id, name, department_id, level, description,
    article_ids, required_course_ids
)
VALUES ($1, $2, $3, $4, $5, $6, $7::uuid[], $8::uuid[])
ON CONFLICT (id) DO UPDATE
SET company_id = EXCLUDED.company_id,
    name = EXCLUDED.name,
    department_id = EXCLUDED.department_id,
    level = EXCLUDED.level,
    description = EXCLUDED.description,
    article_ids = EXCLUDED.article_ids,
    required_course_ids = EXCLUDED.required_course_ids,
    updated_at = now()
WHERE (
    positions.company_id, positions.name, positions.department_id,
    positions.level, positions.description, positions.article_ids,
    positions.required_course_ids
) IS DISTINCT FROM (
    EXCLUDED.company_id, EXCLUDED.name, EXCLUDED.department_id,
    EXCLUDED.level, EXCLUDED.description, EXCLUDED.article_ids,
    EXCLUDED.required_course_ids
)`

const deleteUserPositionsSQL = `
DELETE FROM user_positions
WHERE company_id = $1
  AND user_id = ANY($2::uuid[])`

const upsertUserPositionSQL = `
INSERT INTO user_positions (company_id, user_id, position_id)
VALUES ($1, $2, $3)
ON CONFLICT (user_id) DO UPDATE
SET company_id = EXCLUDED.company_id,
    position_id = EXCLUDED.position_id`

const upsertInviteSQL = `
INSERT INTO invites (
    id, company_id, email, token, role, position_id, department_id,
    invited_by_id, status, expires_at, created_at
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, $10, COALESCE($11::timestamptz, now())
)
ON CONFLICT (id) DO UPDATE
SET company_id = EXCLUDED.company_id,
    email = EXCLUDED.email,
    token = EXCLUDED.token,
    role = EXCLUDED.role,
    position_id = EXCLUDED.position_id,
    department_id = EXCLUDED.department_id,
    invited_by_id = EXCLUDED.invited_by_id,
    status = EXCLUDED.status,
    expires_at = EXCLUDED.expires_at,
    updated_at = now()
WHERE (
    invites.company_id, invites.email, invites.token, invites.role,
    invites.position_id, invites.department_id, invites.invited_by_id,
    invites.status, invites.expires_at
) IS DISTINCT FROM (
    EXCLUDED.company_id, EXCLUDED.email, EXCLUDED.token, EXCLUDED.role,
    EXCLUDED.position_id, EXCLUDED.department_id, EXCLUDED.invited_by_id,
    EXCLUDED.status, EXCLUDED.expires_at
)`

const setCompanyOwnerSQL = `
UPDATE companies
SET owner_id = $2,
    updated_at = now()
WHERE id = $1
  AND owner_id IS DISTINCT FROM $2`

// Apply writes the complete company fixture set using only the supplied
// transaction-bound executor. Callers are responsible for commit/rollback.
func Apply(ctx context.Context, tx execer, dataset Dataset, passwordHash string) error {
	if _, err := tx.Exec(ctx, "SET CONSTRAINTS ALL DEFERRED"); err != nil {
		return fmt.Errorf("отложить ограничения: %w", err)
	}
	if _, err := tx.Exec(
		ctx,
		upsertCompanySQL,
		dataset.Company.ID,
		dataset.Company.Name,
		dataset.Company.LogoURL,
		dataset.Company.CreatedAt,
	); err != nil {
		return fmt.Errorf("сохранить компанию: %w", err)
	}

	for _, user := range dataset.Users {
		if _, err := tx.Exec(
			ctx,
			upsertUserSQL,
			user.ID,
			user.CompanyID,
			user.Email,
			user.FirstName,
			user.LastName,
			user.Phone,
			user.AvatarURL,
			user.Role,
			user.Status,
			user.BirthDate,
			user.HiredAt,
			user.VacationAllowance,
			user.CreatedAt,
		); err != nil {
			return fmt.Errorf("сохранить пользователя %s: %w", user.ID, err)
		}
		if _, err := tx.Exec(ctx, upsertCredentialSQL, user.CompanyID, user.ID, passwordHash); err != nil {
			return fmt.Errorf("сохранить пароль пользователя %s: %w", user.ID, err)
		}
	}

	// Parent links are assigned in a second pass. This allows fixture files to
	// list children before parents even though the FK itself is not deferrable.
	for _, department := range dataset.Departments {
		if _, err := tx.Exec(
			ctx,
			upsertDepartmentSQL,
			department.ID,
			department.CompanyID,
			department.Name,
			department.HeadUserID,
			department.ValuableFinalProduct,
			department.Order,
		); err != nil {
			return fmt.Errorf("сохранить отдел %s: %w", department.ID, err)
		}
	}
	for _, department := range dataset.Departments {
		if _, err := tx.Exec(
			ctx,
			setDepartmentParentSQL,
			department.ID,
			department.CompanyID,
			department.ParentID,
		); err != nil {
			return fmt.Errorf("сохранить родителя отдела %s: %w", department.ID, err)
		}
	}

	for _, position := range dataset.Positions {
		if _, err := tx.Exec(
			ctx,
			upsertPositionSQL,
			position.ID,
			position.CompanyID,
			position.Name,
			position.DepartmentID,
			position.Level,
			position.Description,
			position.ArticleIDs,
			position.RequiredCourseIDs,
		); err != nil {
			return fmt.Errorf("сохранить должность %s: %w", position.ID, err)
		}
	}

	userIDs := make([]uuid.UUID, 0, len(dataset.Users))
	for _, user := range dataset.Users {
		userIDs = append(userIDs, user.ID)
	}
	if _, err := tx.Exec(ctx, deleteUserPositionsSQL, dataset.Company.ID, userIDs); err != nil {
		return fmt.Errorf("очистить назначения должностей: %w", err)
	}
	for _, user := range dataset.Users {
		if user.PositionID == nil {
			continue
		}
		if _, err := tx.Exec(ctx, upsertUserPositionSQL, user.CompanyID, user.ID, *user.PositionID); err != nil {
			return fmt.Errorf("назначить должность пользователю %s: %w", user.ID, err)
		}
	}

	for _, invite := range dataset.Invites {
		if _, err := tx.Exec(
			ctx,
			upsertInviteSQL,
			invite.ID,
			invite.CompanyID,
			invite.Email,
			invite.Token,
			invite.Role,
			invite.PositionID,
			invite.DepartmentID,
			invite.InvitedByID,
			invite.Status,
			invite.ExpiresAt,
			invite.CreatedAt,
		); err != nil {
			return fmt.Errorf("сохранить приглашение %s: %w", invite.ID, err)
		}
	}

	if _, err := tx.Exec(ctx, setCompanyOwnerSQL, dataset.Company.ID, dataset.Company.OwnerID); err != nil {
		return fmt.Errorf("назначить владельца компании: %w", err)
	}
	return nil
}
