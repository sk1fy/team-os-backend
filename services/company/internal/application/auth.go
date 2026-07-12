package application

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	domainauth "github.com/sk1fy/team-os-backend/services/company/internal/domain/auth"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func (s *Service) Register(ctx context.Context, input RegisterInput, meta SessionMeta) (AuthResult, error) {
	companyName, err := requiredText(input.CompanyName, "Укажите название компании")
	if err != nil {
		return AuthResult{}, err
	}
	firstName, err := requiredText(input.FirstName, "Укажите имя")
	if err != nil {
		return AuthResult{}, err
	}
	lastName, err := requiredText(input.LastName, "Укажите фамилию")
	if err != nil {
		return AuthResult{}, err
	}
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return AuthResult{}, err
	}
	releasePasswordSlot, err := s.acquirePasswordSlot(ctx)
	if err != nil {
		return AuthResult{}, internal("Не удалось обработать пароль", err)
	}
	passwordHash, err := domainauth.HashPassword(input.Password)
	releasePasswordSlot()
	if err != nil {
		return AuthResult{}, validation(err.Error())
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return AuthResult{}, internal("Не удалось начать регистрацию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	companyID, userID := uuid.New(), uuid.New()
	_, err = queries.CreateCompany(ctx, db.CreateCompanyParams{ID: companyID, Name: companyName})
	if err != nil {
		return AuthResult{}, internal("Не удалось создать компанию", err)
	}
	user, err := queries.CreateUser(ctx, db.CreateUserParams{
		ID: userID, CompanyID: companyID, Email: email, FirstName: firstName, LastName: lastName,
		Role: "owner", Status: "active",
	})
	if isUniqueViolation(err) {
		return AuthResult{}, conflict("Пользователь с таким email уже существует")
	}
	if err != nil {
		return AuthResult{}, internal("Не удалось создать владельца", err)
	}
	if err = queries.SetCredential(ctx, db.SetCredentialParams{
		CompanyID: companyID, UserID: userID, PasswordHash: passwordHash,
	}); err != nil {
		return AuthResult{}, internal("Не удалось сохранить пароль", err)
	}
	if _, err = queries.SetCompanyOwner(ctx, db.SetCompanyOwnerParams{
		ID: companyID, OwnerID: uuid.NullUUID{UUID: userID, Valid: true},
	}); err != nil {
		return AuthResult{}, internal("Не удалось назначить владельца", err)
	}
	if err = s.emit(ctx, queries, companyID, userID, "teamos.org.user.created.v1", map[string]any{
		"user": userEventSnapshot(userFromDB(user, nil), nil),
	}); err != nil {
		return AuthResult{}, err
	}
	result, err := s.createSession(ctx, queries, user, meta, uuid.NullUUID{})
	if err != nil {
		return AuthResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AuthResult{}, internal("Не удалось завершить регистрацию", err)
	}
	return result, nil
}

func (s *Service) Login(ctx context.Context, input LoginInput, meta SessionMeta) (AuthResult, error) {
	email, err := normalizeEmail(input.Email)
	if err != nil {
		releasePasswordSlot, slotErr := s.acquirePasswordSlot(ctx)
		if slotErr != nil {
			return AuthResult{}, internal("Не удалось выполнить вход", slotErr)
		}
		_, _ = domainauth.VerifyPassword(input.Password, s.dummyHash)
		releasePasswordSlot()
		return AuthResult{}, unauthenticated()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AuthResult{}, internal("Не удалось выполнить вход", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	row, err := queries.GetUserForLogin(ctx, email)
	if err != nil {
		releasePasswordSlot, slotErr := s.acquirePasswordSlot(ctx)
		if slotErr != nil {
			return AuthResult{}, internal("Не удалось выполнить вход", slotErr)
		}
		_, _ = domainauth.VerifyPassword(input.Password, s.dummyHash)
		releasePasswordSlot()
		if isNoRows(err) {
			return AuthResult{}, unauthenticated()
		}
		return AuthResult{}, internal("Не удалось выполнить вход", err)
	}
	releasePasswordSlot, err := s.acquirePasswordSlot(ctx)
	if err != nil {
		return AuthResult{}, internal("Не удалось выполнить вход", err)
	}
	valid, verifyErr := domainauth.VerifyPassword(input.Password, row.PasswordHash)
	releasePasswordSlot()
	if verifyErr != nil || !valid || row.User.Status != "active" {
		return AuthResult{}, unauthenticated()
	}

	result, err := s.createSession(ctx, queries, row.User, meta, uuid.NullUUID{})
	if err != nil {
		return AuthResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AuthResult{}, internal("Не удалось создать сессию", err)
	}
	return result, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string, meta SessionMeta) (AuthResult, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return AuthResult{}, invalidSession()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AuthResult{}, internal("Не удалось обновить сессию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	now := s.now().UTC()
	session, err := queries.GetSessionByHashForUpdate(ctx, domainauth.HashRefreshToken(refreshToken))
	if isNoRows(err) {
		return AuthResult{}, invalidSession()
	}
	if err != nil {
		return AuthResult{}, internal("Не удалось проверить сессию", err)
	}
	if session.RevokedAt.Valid {
		// A revoked token with a replacement was already rotated. Seeing it
		// again is reuse and invalidates the whole account session set. A token
		// revoked by an explicit logout has no replacement and must not let an
		// old cookie become a denial-of-service primitive.
		if session.ReplacedBy.Valid {
			if err = queries.RevokeAllUserSessions(ctx, db.RevokeAllUserSessionsParams{
				UserID: session.UserID, RevokedAt: pgtype.Timestamptz{Time: now, Valid: true},
			}); err != nil {
				return AuthResult{}, internal("Не удалось отозвать сессии", err)
			}
			if err = tx.Commit(ctx); err != nil {
				return AuthResult{}, internal("Не удалось отозвать сессии", err)
			}
		}
		return AuthResult{}, invalidSession()
	}
	if !session.ExpiresAt.After(now) {
		_, err = queries.RevokeSessionByHash(ctx, db.RevokeSessionByHashParams{
			RefreshHash: session.RefreshHash,
			LastUsedAt:  pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			return AuthResult{}, internal("Не удалось закрыть истёкшую сессию", err)
		}
		if err = tx.Commit(ctx); err != nil {
			return AuthResult{}, internal("Не удалось закрыть истёкшую сессию", err)
		}
		return AuthResult{}, invalidSession()
	}
	user, err := queries.GetUser(ctx, db.GetUserParams{CompanyID: session.CompanyID, ID: session.UserID})
	if err != nil || user.Status != "active" {
		return AuthResult{}, invalidSession()
	}
	newSessionID := uuid.New()
	result, err := s.createSessionWithID(
		ctx, queries, user, meta, newSessionID,
		uuid.NullUUID{UUID: session.ID, Valid: true},
	)
	if err != nil {
		return AuthResult{}, err
	}
	rows, err := queries.RotateSession(ctx, db.RotateSessionParams{
		ID:         session.ID,
		RevokedAt:  pgtype.Timestamptz{Time: now, Valid: true},
		ReplacedBy: uuid.NullUUID{UUID: newSessionID, Valid: true},
	})
	if err != nil || rows != 1 {
		return AuthResult{}, invalidSession()
	}
	if err = tx.Commit(ctx); err != nil {
		return AuthResult{}, internal("Не удалось обновить сессию", err)
	}
	return result, nil
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if strings.TrimSpace(refreshToken) == "" {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return internal("Не удалось завершить сессию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	session, err := queries.GetSessionByHashForUpdate(ctx, domainauth.HashRefreshToken(refreshToken))
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return internal("Не удалось завершить сессию", err)
	}
	if err = queries.RevokeAllUserSessions(ctx, db.RevokeAllUserSessionsParams{
		UserID:    session.UserID,
		RevokedAt: pgtype.Timestamptz{Time: s.now().UTC(), Valid: true},
	}); err != nil {
		return internal("Не удалось завершить сессию", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось завершить сессию", err)
	}
	return nil
}

func (s *Service) GetInviteByToken(ctx context.Context, token string) (Invite, error) {
	invite, err := db.New(s.pool).GetInviteByToken(ctx, token)
	if isNoRows(err) {
		return Invite{}, notFound("Приглашение")
	}
	if err != nil {
		return Invite{}, internal("Не удалось получить приглашение", err)
	}
	result := inviteFromDB(invite)
	if result.Status == "pending" && !invite.ExpiresAt.After(s.now()) {
		result.Status = "expired"
	}
	return result, nil
}

func (s *Service) AcceptInvite(ctx context.Context, input AcceptInviteInput, meta SessionMeta) (AuthResult, error) {
	firstName, err := requiredText(input.FirstName, "Укажите имя")
	if err != nil {
		return AuthResult{}, err
	}
	lastName, err := requiredText(input.LastName, "Укажите фамилию")
	if err != nil {
		return AuthResult{}, err
	}
	releasePasswordSlot, err := s.acquirePasswordSlot(ctx)
	if err != nil {
		return AuthResult{}, internal("Не удалось обработать пароль", err)
	}
	passwordHash, err := domainauth.HashPassword(input.Password)
	releasePasswordSlot()
	if err != nil {
		return AuthResult{}, validation(err.Error())
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return AuthResult{}, internal("Не удалось принять приглашение", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	invite, err := queries.GetInviteByTokenForUpdate(ctx, input.Token)
	if isNoRows(err) {
		return AuthResult{}, notFound("Приглашение")
	}
	if err != nil {
		return AuthResult{}, internal("Не удалось проверить приглашение", err)
	}
	if invite.Status != "pending" || !invite.ExpiresAt.After(s.now()) {
		return AuthResult{}, validation("Приглашение недействительно или истекло")
	}
	email := input.Email
	if invite.Email.Valid {
		email = invite.Email.String
	}
	email, err = normalizeEmail(email)
	if err != nil {
		if !invite.Email.Valid {
			return AuthResult{}, validation("Для приглашения по ссылке укажите email")
		}
		return AuthResult{}, err
	}

	user, findErr := queries.GetUserByEmailForUpdate(ctx, email)
	if findErr != nil && !isNoRows(findErr) {
		return AuthResult{}, internal("Не удалось проверить пользователя", findErr)
	}
	if findErr == nil {
		if user.CompanyID != invite.CompanyID {
			return AuthResult{}, conflict("Пользователь с таким email уже существует")
		}
		if user.Status == "active" {
			return AuthResult{}, conflict("Пользователь с таким email уже активен")
		}
		user, err = queries.ActivateInvitedUser(ctx, db.ActivateInvitedUserParams{
			ID: user.ID, FirstName: firstName, LastName: lastName,
			Role: invite.Role, CompanyID: invite.CompanyID,
		})
		if err != nil {
			return AuthResult{}, internal("Не удалось активировать пользователя", err)
		}
	} else {
		user, err = queries.CreateUser(ctx, db.CreateUserParams{
			ID: uuid.New(), CompanyID: invite.CompanyID, Email: email,
			FirstName: firstName, LastName: lastName, Role: invite.Role, Status: "active",
		})
		if err != nil {
			return AuthResult{}, internal("Не удалось создать пользователя", err)
		}
	}
	if err = queries.SetCredential(ctx, db.SetCredentialParams{
		CompanyID: user.CompanyID, UserID: user.ID, PasswordHash: passwordHash,
	}); err != nil {
		return AuthResult{}, internal("Не удалось сохранить пароль", err)
	}
	if err = queries.DeleteUserPositions(ctx, db.DeleteUserPositionsParams{
		CompanyID: user.CompanyID, UserID: user.ID,
	}); err != nil {
		return AuthResult{}, internal("Не удалось обновить должность", err)
	}
	if invite.PositionID.Valid {
		if err = queries.AssignUserPosition(ctx, db.AssignUserPositionParams{
			CompanyID: user.CompanyID, UserID: user.ID, PositionID: invite.PositionID.UUID,
		}); err != nil {
			return AuthResult{}, internal("Не удалось назначить должность", err)
		}
	}
	if _, err = queries.AcceptInvite(ctx, invite.ID); err != nil {
		return AuthResult{}, validation("Приглашение недействительно или истекло")
	}
	positionIDs, err := queries.GetUserPositionIDs(ctx, db.GetUserPositionIDsParams{CompanyID: user.CompanyID, UserID: user.ID})
	if err != nil {
		return AuthResult{}, internal("Не удалось получить должность", err)
	}
	departmentIDs, err := queries.GetUserDepartmentClaims(ctx, db.GetUserDepartmentClaimsParams{CompanyID: user.CompanyID, UserID: user.ID})
	if err != nil {
		return AuthResult{}, internal("Не удалось получить отделы", err)
	}
	if err = s.emit(ctx, queries, user.CompanyID, user.ID, "teamos.org.user.updated.v1", map[string]any{
		"user":          userEventSnapshot(userFromDB(user, positionIDs), departmentIDs),
		"changedFields": []string{"firstName", "lastName", "role", "status", "positionIds"},
	}); err != nil {
		return AuthResult{}, err
	}
	result, err := s.createSession(ctx, queries, user, meta, uuid.NullUUID{})
	if err != nil {
		return AuthResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AuthResult{}, internal("Не удалось принять приглашение", err)
	}
	return result, nil
}

func (s *Service) createSession(
	ctx context.Context,
	queries *db.Queries,
	user db.User,
	meta SessionMeta,
	rotatedFrom uuid.NullUUID,
) (AuthResult, error) {
	return s.createSessionWithID(ctx, queries, user, meta, uuid.New(), rotatedFrom)
}

func (s *Service) createSessionWithID(
	ctx context.Context,
	queries *db.Queries,
	user db.User,
	meta SessionMeta,
	sessionID uuid.UUID,
	rotatedFrom uuid.NullUUID,
) (AuthResult, error) {
	positionIDs, err := queries.GetUserPositionIDs(ctx, db.GetUserPositionIDsParams{
		CompanyID: user.CompanyID, UserID: user.ID,
	})
	if err != nil {
		return AuthResult{}, internal("Не удалось получить должности", err)
	}
	departmentIDs, err := queries.GetUserDepartmentClaims(ctx, db.GetUserDepartmentClaimsParams{
		CompanyID: user.CompanyID, UserID: user.ID,
	})
	if err != nil {
		return AuthResult{}, internal("Не удалось получить отделы", err)
	}
	positions := make([]string, len(positionIDs))
	for index, id := range positionIDs {
		positions[index] = id.String()
	}
	departments := make([]string, len(departmentIDs))
	for index, id := range departmentIDs {
		departments[index] = id.String()
	}
	accessToken, accessExpiresAt, err := s.issuer.Issue(
		user.ID.String(), user.CompanyID.String(), user.Role, positions, departments,
	)
	if err != nil {
		return AuthResult{}, internal("Не удалось выпустить access token", err)
	}
	refreshToken, refreshHash, err := domainauth.NewRefreshToken()
	if err != nil {
		return AuthResult{}, internal("Не удалось выпустить refresh token", err)
	}
	refreshExpiresAt := s.now().UTC().Add(s.refreshTTL)
	_, err = queries.CreateSession(ctx, db.CreateSessionParams{
		ID: sessionID, CompanyID: user.CompanyID, UserID: user.ID,
		RefreshHash: refreshHash, ExpiresAt: refreshExpiresAt, RotatedFrom: rotatedFrom,
		UserAgent: pgtype.Text{String: meta.UserAgent, Valid: meta.UserAgent != ""},
		IpAddress: parseIPAddress(meta.IPAddress),
	})
	if err != nil {
		return AuthResult{}, internal("Не удалось сохранить сессию", err)
	}
	return AuthResult{
		AccessToken: accessToken, AccessExpiresAt: accessExpiresAt,
		RefreshToken: refreshToken, RefreshExpiresAt: refreshExpiresAt,
		User: userFromDB(user, positionIDs),
	}, nil
}
