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

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return AuthResult{}, internal("Не удалось начать регистрацию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	companyID, userID := uuid.New(), uuid.New()
	now := s.now().UTC()
	var loginReservation *db.RegistrationLoginReservation
	if strings.TrimSpace(input.LoginReservationToken) != "" {
		loginReservationToken, tokenErr := normalizeLoginReservationToken(input.LoginReservationToken)
		if tokenErr != nil {
			return AuthResult{}, tokenErr
		}
		row, lookupErr := queries.GetRegistrationLoginReservationForUpdate(
			ctx, domainauth.HashOpaqueToken(loginReservationToken),
		)
		if isNoRows(lookupErr) {
			return AuthResult{}, loginReservationInvalid()
		}
		if lookupErr != nil {
			return AuthResult{}, internal("Не удалось проверить резервацию логина", lookupErr)
		}
		if stateErr := registrationLoginReservationError(row, now); stateErr != nil {
			return AuthResult{}, stateErr
		}
		loginReservation = &row
	}
	registrationToken := strings.TrimSpace(input.RegistrationToken)
	var registration *db.CompanyRegistrationToken
	if registrationToken != "" {
		if len(registrationToken) > 512 {
			return AuthResult{}, registrationTokenInvalid()
		}
		tokenHash := domainauth.HashOpaqueToken(registrationToken)
		candidate, lookupErr := queries.GetCompanyRegistrationTokenByHash(ctx, tokenHash)
		if isNoRows(lookupErr) {
			return AuthResult{}, registrationTokenInvalid()
		}
		if lookupErr != nil {
			return AuthResult{}, internal("Не удалось проверить токен регистрации", lookupErr)
		}
		if stateErr := companyRegistrationTokenError(candidate, s.now().UTC()); stateErr != nil {
			return AuthResult{}, stateErr
		}
		if lookupErr = queries.LockAmoAccount(ctx, db.LockAmoAccountParams{
			Provider: candidate.Provider, ExternalAccountID: candidate.ExternalAccountID,
		}); lookupErr != nil {
			return AuthResult{}, internal("Не удалось заблокировать аккаунт amoCRM", lookupErr)
		}
		row, lookupErr := queries.GetCompanyRegistrationTokenByHashForUpdate(ctx, tokenHash)
		if isNoRows(lookupErr) {
			return AuthResult{}, registrationTokenInvalid()
		}
		if lookupErr != nil {
			return AuthResult{}, internal("Не удалось заблокировать токен регистрации", lookupErr)
		}
		if stateErr := companyRegistrationTokenError(row, s.now().UTC()); stateErr != nil {
			return AuthResult{}, stateErr
		}
		companyID = row.CompanyID
		registration = &row
		if _, err = queries.CreateCompanyFromRegistrationToken(ctx, db.CreateCompanyFromRegistrationTokenParams{
			ID: companyID, Name: companyName,
			AmoAccountID: pgtype.Text{String: row.ExternalAccountID, Valid: true},
			CompletedAt:  pgTimestamp(s.now().UTC()),
		}); err != nil {
			if isUniqueViolation(err) {
				return AuthResult{}, coded(ErrorConflict, ErrorCodeAmoAccountAlreadyExists, "Этот аккаунт amoCRM уже используется")
			}
			return AuthResult{}, internal("Не удалось создать компанию", err)
		}
		if _, err = queries.CreateCompanyIntegration(ctx, db.CreateCompanyIntegrationParams{
			ID: uuid.New(), CompanyID: companyID, Provider: row.Provider,
			ExternalAccountID: row.ExternalAccountID, Entitlements: []string{},
			LastVerifiedAt: pgTimestamp(s.now().UTC()), Metadata: []byte(`{}`),
		}); err != nil {
			if isUniqueViolation(err) {
				return AuthResult{}, coded(ErrorConflict, ErrorCodeAmoAccountAlreadyExists, "Этот аккаунт amoCRM уже используется")
			}
			return AuthResult{}, internal("Не удалось связать компанию с amoCRM", err)
		}
	} else {
		_, err = queries.CreateCompany(ctx, db.CreateCompanyParams{ID: companyID, Name: companyName})
		if err != nil {
			return AuthResult{}, internal("Не удалось создать компанию", err)
		}
	}
	user, err := queries.CreateUser(ctx, db.CreateUserParams{
		ID: userID, CompanyID: companyID, Email: email, FirstName: firstName, LastName: pgText(&lastName),
		Role: "owner", Status: "active",
	})
	if isUniqueViolation(err) {
		return AuthResult{}, conflict("Пользователь с таким email уже существует")
	}
	if err != nil {
		return AuthResult{}, internal("Не удалось создать владельца", err)
	}
	if loginReservation != nil {
		if _, err = queries.ApplyReservedUserLogin(ctx, db.ApplyReservedUserLoginParams{
			Login: loginReservation.Login, CompanyID: companyID, UserID: userID,
		}); err != nil {
			return AuthResult{}, internal("Не удалось закрепить зарезервированный логин", err)
		}
		if _, err = queries.ConsumeRegistrationLoginReservation(ctx, db.ConsumeRegistrationLoginReservationParams{
			ConsumedAt: pgTimestamp(now), ID: loginReservation.ID,
		}); isNoRows(err) {
			return AuthResult{}, loginReservationConsumed()
		} else if err != nil {
			return AuthResult{}, internal("Не удалось погасить резервацию логина", err)
		}
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
	if err = s.emit(ctx, queries, companyID, userID, "teamos.company.company.created.v1", map[string]any{
		"companyId": companyID.String(), "name": companyName, "ownerUserId": userID.String(),
	}); err != nil {
		return AuthResult{}, err
	}
	registeredUser, err := userFromDBWithLogin(ctx, queries, user, nil)
	if err != nil {
		return AuthResult{}, err
	}
	if err = s.emit(ctx, queries, companyID, userID, "teamos.org.user.created.v1", map[string]any{
		"user": userEventSnapshot(registeredUser, nil),
	}); err != nil {
		return AuthResult{}, err
	}
	if registration != nil {
		if _, err = queries.ConsumeCompanyRegistrationToken(ctx, db.ConsumeCompanyRegistrationTokenParams{
			ConsumedAt: pgTimestamp(s.now().UTC()), ID: registration.ID,
		}); err != nil {
			return AuthResult{}, registrationTokenConsumed()
		}
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
	login, valid := normalizeLoginIdentifier(input.Login)
	if !valid {
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
	row, err := queries.GetUserForLogin(ctx, login)
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

func normalizeLoginIdentifier(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 9 || !strings.HasPrefix(value, "tm") {
		return "", false
	}
	for _, digit := range value[2:] {
		if digit < '0' || digit > '9' {
			return "", false
		}
	}
	return value, true
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

	user, findErr := queries.GetUserByEmailForUpdate(ctx, db.GetUserByEmailForUpdateParams{
		CompanyID: invite.CompanyID, Email: email,
	})
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
			ID: user.ID, FirstName: firstName, LastName: pgText(&lastName),
			Role: invite.Role, CompanyID: invite.CompanyID,
		})
		if err != nil {
			return AuthResult{}, internal("Не удалось активировать пользователя", err)
		}
	} else {
		user, err = queries.CreateUser(ctx, db.CreateUserParams{
			ID: uuid.New(), CompanyID: invite.CompanyID, Email: email,
			FirstName: firstName, LastName: pgText(&lastName), Role: invite.Role, Status: "active",
		})
		if isUniqueViolation(err) {
			return AuthResult{}, conflict("Пользователь с таким email уже существует или удалён из amoCRM")
		}
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
	sectionAccess := []string(nil)
	if user.Role == "employee" {
		sectionAccess = append([]string(nil), defaultEmployeeSections...)
		if err = replaceEmployeeSections(ctx, queries, user.CompanyID, user.ID, sectionAccess, nil); err != nil {
			return AuthResult{}, err
		}
	} else if err = replaceEmployeeSections(ctx, queries, user.CompanyID, user.ID, nil, nil); err != nil {
		return AuthResult{}, err
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
	acceptedUser, err := userFromDBWithLogin(ctx, queries, user, positionIDs)
	if err != nil {
		return AuthResult{}, err
	}
	acceptedUser.SectionAccess = normalizedEmployeeSections(sectionAccess)
	if err = s.emit(ctx, queries, user.CompanyID, user.ID, "teamos.org.user.updated.v1", map[string]any{
		"user":          userEventSnapshot(acceptedUser, departmentIDs),
		"changedFields": []string{"firstName", "lastName", "role", "status", "positionIds", "sectionAccess"},
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
	sectionAccess, err := queries.ListEmployeeSectionAccess(ctx, db.ListEmployeeSectionAccessParams{
		CompanyID: user.CompanyID, UserID: user.ID,
	})
	if err != nil {
		return AuthResult{}, internal("Не удалось получить доступ к разделам", err)
	}
	if user.Role != "employee" {
		sectionAccess = nil
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
		user.ID.String(), user.CompanyID.String(), user.Role, positions, departments, sectionAccess,
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
	accessMode, err := queries.GetUserAccessMode(ctx, db.GetUserAccessModeParams{
		CompanyID: user.CompanyID,
		UserID:    user.ID,
	})
	if err != nil {
		return AuthResult{}, internal("Не удалось получить способ доступа", err)
	}
	resultUser, err := userFromDBWithLogin(ctx, queries, user, positionIDs)
	if err != nil {
		return AuthResult{}, err
	}
	directDepartmentIDs, err := queries.GetUserDirectDepartmentIDs(ctx, db.GetUserDirectDepartmentIDsParams{
		CompanyID: user.CompanyID, UserID: user.ID,
	})
	if err != nil {
		return AuthResult{}, internal("Не удалось получить прямой отдел", err)
	}
	resultUser.DepartmentIDs = directDepartmentIDs
	resultUser.AccessMode = accessMode
	resultUser.SectionAccess = normalizedEmployeeSections(sectionAccess)
	return AuthResult{
		AccessToken: accessToken, AccessExpiresAt: accessExpiresAt,
		RefreshToken: refreshToken, RefreshExpiresAt: refreshExpiresAt,
		User: resultUser,
	}, nil
}
