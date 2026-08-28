package application

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

const maxAmoCompanyBootstrapUsers = 1000

type normalizedAmoBootstrapUser struct {
	id        string
	email     string
	firstName string
	lastName  string
	role      string
	status    string
}

func (s *Service) BootstrapAmoCompany(
	ctx context.Context,
	input AmoCompanyBootstrapInput,
) (AmoCompanyBootstrapResult, error) {
	_, accountID, err := normalizeAmoAccount(amoWidgetProvider, input.AmoAccountID)
	if err != nil {
		return AmoCompanyBootstrapResult{}, err
	}
	selfUserID := strings.TrimSpace(input.SelfUserID)
	if !canonicalAmoID(selfUserID) || len(input.Users) == 0 || len(input.Users) > maxAmoCompanyBootstrapUsers {
		return AmoCompanyBootstrapResult{}, validation("Некорректный список пользователей amoCRM")
	}
	if len([]rune(strings.TrimSpace(input.Subdomain))) > 255 {
		return AmoCompanyBootstrapResult{}, validation("Слишком длинный поддомен amoCRM")
	}
	companyName := strings.TrimSpace(input.CompanyName)
	if companyName == "" {
		companyName = "Компания amoCRM " + accountID
	}
	if len([]rune(companyName)) > 255 {
		return AmoCompanyBootstrapResult{}, validation("Слишком длинное название компании")
	}

	users, err := normalizeAmoBootstrapUsers(input.Users, selfUserID)
	if err != nil {
		return AmoCompanyBootstrapResult{}, err
	}

	now := s.now().UTC()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return AmoCompanyBootstrapResult{}, internal("Не удалось начать создание компании TeamOS", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = queries.LockAmoAccount(ctx, db.LockAmoAccountParams{
		Provider: amoWidgetProvider, ExternalAccountID: accountID,
	}); err != nil {
		return AmoCompanyBootstrapResult{}, internal("Не удалось заблокировать аккаунт amoCRM", err)
	}
	integration, company, companyCreated, err := s.getOrCreateAmoWidgetCompany(
		ctx, queries, accountID, companyName, now, true,
	)
	if err != nil {
		return AmoCompanyBootstrapResult{}, err
	}
	if !companyCreated {
		return AmoCompanyBootstrapResult{}, conflict("Компания для этого аккаунта amoCRM уже создана")
	}

	var owner db.User
	for _, candidate := range users {
		user, _, created, createErr := s.getOrCreateAmoWidgetUser(
			ctx, queries, company.ID, integration.ID, accountID, candidate.id,
			candidate.email, candidate.firstName, candidate.lastName, now,
		)
		if createErr != nil {
			return AmoCompanyBootstrapResult{}, createErr
		}
		if !created {
			return AmoCompanyBootstrapResult{}, conflict("Пользователь amoCRM уже зарегистрирован в TeamOS")
		}
		user, err = queries.SetAmoBootstrapUserState(ctx, db.SetAmoBootstrapUserStateParams{
			Role: candidate.role, Status: candidate.status, UpdatedAt: now,
			CompanyID: company.ID, UserID: user.ID,
		})
		if err != nil {
			return AmoCompanyBootstrapResult{}, internal("Не удалось назначить доступ пользователю amoCRM", err)
		}
		createdUser, loginErr := userFromDBWithLogin(ctx, queries, user, nil)
		if loginErr != nil {
			return AmoCompanyBootstrapResult{}, loginErr
		}
		if err = s.emit(ctx, queries, company.ID, user.ID, "teamos.org.user.created.v1", map[string]any{
			"user": userEventSnapshot(createdUser, nil),
		}); err != nil {
			return AmoCompanyBootstrapResult{}, err
		}
		if candidate.id == selfUserID {
			owner = user
		}
	}
	if owner.ID == uuid.Nil {
		return AmoCompanyBootstrapResult{}, validation("Текущий пользователь отсутствует в списке amoCRM")
	}
	company, err = queries.SetCompanyOwner(ctx, db.SetCompanyOwnerParams{
		ID: company.ID, OwnerID: uuid.NullUUID{UUID: owner.ID, Valid: true},
	})
	if err != nil {
		return AmoCompanyBootstrapResult{}, internal("Не удалось назначить владельца компании", err)
	}
	if err = s.emit(ctx, queries, company.ID, owner.ID, "teamos.company.company.created.v1", map[string]any{
		"companyId": company.ID.String(), "name": company.Name, "ownerUserId": owner.ID.String(),
		"source": "amo_browser_assertion", "subdomain": strings.TrimSpace(input.Subdomain),
	}); err != nil {
		return AmoCompanyBootstrapResult{}, err
	}
	link, err := ensureAmoWidgetAccessLink(ctx, queries, company.ID, owner.ID)
	if err != nil {
		return AmoCompanyBootstrapResult{}, err
	}
	link, err = queries.SetAccessLinkEntryContext(ctx, db.SetAccessLinkEntryContextParams{
		CompanyID:    company.ID,
		UserID:       owner.ID,
		EntryContext: pgtype.Text{String: "company_created", Valid: true},
	})
	if err != nil {
		return AmoCompanyBootstrapResult{}, internal("Не удалось подготовить приветствие новой компании", err)
	}
	if err = createUserAdminAudit(
		ctx, queries, company.ID, &owner.ID, nil, "system", "amo_company_bootstrap",
		nil, map[string]any{"role": "owner", "assertionSource": "client_snapshot"},
		input.RequestID, now,
	); err != nil {
		return AmoCompanyBootstrapResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AmoCompanyBootstrapResult{}, internal("Не удалось завершить создание компании TeamOS", err)
	}
	return AmoCompanyBootstrapResult{
		Action: "register", CompanyID: company.ID, UserID: owner.ID,
		Role: "owner", AccessToken: link.Token,
	}, nil
}

func normalizeAmoBootstrapUsers(
	input []AmoCompanyBootstrapUser,
	selfUserID string,
) ([]normalizedAmoBootstrapUser, error) {
	seenIDs := make(map[string]struct{}, len(input))
	seenEmails := make(map[string]string, len(input))
	result := make([]normalizedAmoBootstrapUser, 0, len(input))
	selfFound := false
	for _, value := range input {
		id := strings.TrimSpace(value.ID)
		if !canonicalAmoID(id) {
			return nil, validation("Некорректный ID пользователя amoCRM")
		}
		if _, duplicate := seenIDs[id]; duplicate {
			return nil, validation("Список пользователей amoCRM содержит дубликаты")
		}
		seenIDs[id] = struct{}{}
		email, err := normalizeEmail(value.Email)
		if err != nil {
			return nil, err
		}
		if previousID, duplicate := seenEmails[email]; duplicate && previousID != id {
			return nil, validation("Список пользователей amoCRM содержит одинаковые email")
		}
		seenEmails[email] = id
		firstName, lastName, _ := splitEmployeeName(value.Name)
		if len([]rune(firstName)) > 255 || len([]rune(lastName)) > 255 {
			return nil, validation("Слишком длинное имя пользователя amoCRM")
		}
		role := "employee"
		if value.IsAdmin {
			role = "admin"
		}
		status := "deactivated"
		if value.IsActive {
			status = "active"
		}
		if id == selfUserID {
			if !value.IsAdmin || !value.IsActive {
				return nil, validation("Создать компанию может только активный администратор amoCRM")
			}
			role = "owner"
			selfFound = true
		}
		result = append(result, normalizedAmoBootstrapUser{
			id: id, email: email, firstName: firstName, lastName: lastName,
			role: role, status: status,
		})
	}
	if !selfFound {
		return nil, validation("Текущий пользователь отсутствует в списке amoCRM")
	}
	return result, nil
}
