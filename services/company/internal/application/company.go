package application

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func (s *Service) GetCurrentUser(ctx context.Context, actor Actor) (User, error) {
	row, err := db.New(s.pool).GetUserWithPositions(ctx, db.GetUserWithPositionsParams{
		CompanyID: actor.CompanyID, ID: actor.UserID,
	})
	if isNoRows(err) {
		return User{}, notFound("Пользователь")
	}
	if err != nil {
		return User{}, internal("Не удалось получить пользователя", err)
	}
	return userFromJoinedRow(row), nil
}

func (s *Service) UpdateCurrentUser(
	ctx context.Context,
	actor Actor,
	input UpdateCurrentUserInput,
) (User, error) {
	if input.FirstName != nil {
		value := strings.TrimSpace(*input.FirstName)
		if value == "" {
			return User{}, validation("Укажите имя")
		}
		input.FirstName = &value
	}
	if input.LastName != nil {
		value := strings.TrimSpace(*input.LastName)
		if value == "" {
			return User{}, validation("Укажите фамилию")
		}
		input.LastName = &value
	}
	if input.Phone != nil {
		input.Phone = trimmedOptional(input.Phone)
	}
	if input.AvatarURL != nil {
		input.AvatarURL = trimmedOptional(input.AvatarURL)
	}
	queries := db.New(s.pool)
	user, err := queries.UpdateCurrentUser(ctx, db.UpdateCurrentUserParams{
		FirstName: pgText(input.FirstName), LastName: pgText(input.LastName),
		SetPhone: input.SetPhone, Phone: pgText(input.Phone),
		SetAvatarUrl: input.SetAvatarURL, AvatarUrl: pgText(input.AvatarURL),
		CompanyID: actor.CompanyID, ID: actor.UserID,
	})
	if isNoRows(err) {
		return User{}, notFound("Пользователь")
	}
	if err != nil {
		return User{}, internal("Не удалось обновить пользователя", err)
	}
	positions, err := queries.GetUserPositionIDs(ctx, db.GetUserPositionIDsParams{
		CompanyID: actor.CompanyID, UserID: actor.UserID,
	})
	if err != nil {
		return User{}, internal("Не удалось получить должность", err)
	}
	return userFromDB(user, positions), nil
}

func (s *Service) GetCompany(ctx context.Context, actor Actor) (Company, error) {
	company, err := db.New(s.pool).GetCompany(ctx, actor.CompanyID)
	if isNoRows(err) {
		return Company{}, notFound("Компания")
	}
	if err != nil {
		return Company{}, internal("Не удалось получить компанию", err)
	}
	return companyFromDB(company), nil
}

func (s *Service) UpdateCompany(ctx context.Context, actor Actor, input UpdateCompanyInput) (Company, error) {
	if err := requireAdministrator(actor); err != nil {
		return Company{}, err
	}
	if input.Name != nil {
		value := strings.TrimSpace(*input.Name)
		if value == "" {
			return Company{}, validation("Укажите название компании")
		}
		input.Name = &value
	}
	if input.LogoURL != nil {
		value := strings.TrimSpace(*input.LogoURL)
		input.LogoURL = &value
	}
	company, err := db.New(s.pool).UpdateCompany(ctx, db.UpdateCompanyParams{
		Name: pgText(input.Name), SetLogo: input.SetLogoURL,
		LogoUrl: pgtype.Text{String: valueOrEmpty(input.LogoURL), Valid: input.LogoURL != nil && valueOrEmpty(input.LogoURL) != ""},
		ID:      actor.CompanyID,
	})
	if isNoRows(err) {
		return Company{}, notFound("Компания")
	}
	if err != nil {
		return Company{}, internal("Не удалось обновить компанию", err)
	}
	return companyFromDB(company), nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
