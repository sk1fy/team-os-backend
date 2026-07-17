package application

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	domainorg "github.com/sk1fy/team-os-backend/services/company/internal/domain/org"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

var validRoles = map[string]struct{}{
	"owner": {}, "admin": {}, "employee": {}, "partner": {},
}

var validStatuses = map[string]struct{}{
	"active": {}, "invited": {}, "deactivated": {},
}

func (s *Service) ListDepartments(ctx context.Context, actor Actor) ([]Department, error) {
	rows, err := db.New(s.pool).ListDepartments(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить отделы", err)
	}
	result := make([]Department, len(rows))
	for index, row := range rows {
		result[index] = departmentFromDB(row)
	}
	return result, nil
}

func (s *Service) CreateDepartment(ctx context.Context, actor Actor, input CreateDepartmentInput) (Department, error) {
	if err := requireAdministrator(actor); err != nil {
		return Department{}, err
	}
	name, err := requiredText(input.Name, "Укажите название отдела")
	if err != nil {
		return Department{}, err
	}
	queries := db.New(s.pool)
	if input.ParentID != nil {
		if _, err = queries.GetDepartment(ctx, db.GetDepartmentParams{CompanyID: actor.CompanyID, ID: *input.ParentID}); isNoRows(err) {
			return Department{}, notFound("Родительский отдел")
		} else if err != nil {
			return Department{}, internal("Не удалось проверить родительский отдел", err)
		}
	}
	if input.HeadUserID != nil {
		if _, err = queries.GetUser(ctx, db.GetUserParams{CompanyID: actor.CompanyID, ID: *input.HeadUserID}); isNoRows(err) {
			return Department{}, notFound("Сотрудник")
		} else if err != nil {
			return Department{}, internal("Не удалось проверить руководителя", err)
		}
	}
	vfp := trimmedOptional(input.ValuableFinalProduct)
	row, err := queries.CreateDepartment(ctx, db.CreateDepartmentParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, Name: name,
		ParentID: nullableUUID(input.ParentID), HeadUserID: nullableUUID(input.HeadUserID),
		ValuableFinalProduct: pgText(vfp),
	})
	if err != nil {
		return Department{}, internal("Не удалось создать отдел", err)
	}
	return departmentFromDB(row), nil
}

func (s *Service) UpdateDepartment(ctx context.Context, actor Actor, input UpdateDepartmentInput) (Department, error) {
	if err := requireAdministrator(actor); err != nil {
		return Department{}, err
	}
	if input.Name != nil {
		name, err := requiredText(*input.Name, "Укажите название отдела")
		if err != nil {
			return Department{}, err
		}
		input.Name = &name
	}
	queries := db.New(s.pool)
	if input.SetHeadUserID && input.HeadUserID != nil {
		if _, err := queries.GetUser(ctx, db.GetUserParams{CompanyID: actor.CompanyID, ID: *input.HeadUserID}); isNoRows(err) {
			return Department{}, notFound("Сотрудник")
		} else if err != nil {
			return Department{}, internal("Не удалось проверить руководителя", err)
		}
	}
	vfp := trimmedOptional(input.ValuableFinalProduct)
	row, err := queries.UpdateDepartment(ctx, db.UpdateDepartmentParams{
		Name: pgText(input.Name), SetHead: input.SetHeadUserID, HeadUserID: nullableUUID(input.HeadUserID),
		SetVfp: input.SetValuableFinalProduct, ValuableFinalProduct: pgText(vfp),
		CompanyID: actor.CompanyID, ID: input.ID,
	})
	if isNoRows(err) {
		return Department{}, notFound("Отдел")
	}
	if err != nil {
		return Department{}, internal("Не удалось обновить отдел", err)
	}
	return departmentFromDB(row), nil
}

func (s *Service) DeleteDepartment(ctx context.Context, actor Actor, id uuid.UUID) error {
	if err := requireAdministrator(actor); err != nil {
		return err
	}
	queries := db.New(s.pool)
	children, err := queries.CountDepartmentChildren(ctx, db.CountDepartmentChildrenParams{
		CompanyID: actor.CompanyID, ParentID: uuid.NullUUID{UUID: id, Valid: true},
	})
	if err != nil {
		return internal("Не удалось проверить отдел", err)
	}
	positions, err := queries.CountDepartmentPositions(ctx, db.CountDepartmentPositionsParams{
		CompanyID: actor.CompanyID, DepartmentID: id,
	})
	if err != nil {
		return internal("Не удалось проверить отдел", err)
	}
	if children > 0 || positions > 0 {
		return validation("Нельзя удалить отдел с вложенными отделами или должностями. Сначала переместите их.")
	}
	rows, err := queries.DeleteDepartment(ctx, db.DeleteDepartmentParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		return internal("Не удалось удалить отдел", err)
	}
	if rows == 0 {
		return notFound("Отдел")
	}
	return nil
}

func (s *Service) MoveDepartment(ctx context.Context, actor Actor, id uuid.UUID, parentID *uuid.UUID) (Department, error) {
	if err := requireAdministrator(actor); err != nil {
		return Department{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Department{}, internal("Не удалось переместить отдел", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	rows, err := queries.ListDepartments(ctx, actor.CompanyID)
	if err != nil {
		return Department{}, internal("Не удалось получить дерево отделов", err)
	}
	domainDepartments := make([]domainorg.Department, len(rows))
	for index, row := range rows {
		var parent *domainorg.ID
		if row.ParentID.Valid {
			value := domainorg.ID(row.ParentID.UUID.String())
			parent = &value
		}
		domainDepartments[index] = domainorg.Department{ID: domainorg.ID(row.ID.String()), ParentID: parent}
	}
	var target *domainorg.ID
	if parentID != nil {
		value := domainorg.ID(parentID.String())
		target = &value
	}
	move := domainorg.CanMoveDepartment(domainDepartments, domainorg.ID(id.String()), target)
	if !move.Allowed {
		if move.Reason == "" {
			move.Reason = "Перемещение невозможно"
		}
		return Department{}, validation(move.Reason)
	}
	affectedUserIDs, err := queries.ResolveDepartmentUserIDs(ctx, db.ResolveDepartmentUserIDsParams{
		CompanyID: actor.CompanyID, DepartmentID: id, IncludeDescendants: true,
	})
	if err != nil {
		return Department{}, internal("Не удалось получить сотрудников отдела", err)
	}
	row, err := queries.MoveDepartment(ctx, db.MoveDepartmentParams{
		CompanyID: actor.CompanyID, ID: id, ParentID: nullableUUID(parentID),
	})
	if err != nil {
		return Department{}, internal("Не удалось переместить отдел", err)
	}
	for _, userID := range affectedUserIDs {
		userRow, userErr := queries.GetUserWithPositions(ctx, db.GetUserWithPositionsParams{
			CompanyID: actor.CompanyID, ID: userID,
		})
		if userErr != nil {
			return Department{}, internal("Не удалось получить сотрудника отдела", userErr)
		}
		departments, departmentsErr := queries.GetUserDepartmentClaims(ctx, db.GetUserDepartmentClaimsParams{
			CompanyID: actor.CompanyID, UserID: userID,
		})
		if departmentsErr != nil {
			return Department{}, internal("Не удалось получить отделы сотрудника", departmentsErr)
		}
		if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.org.user.updated.v1", map[string]any{
			"user":          userEventSnapshot(userFromJoinedRow(userRow), departments),
			"changedFields": []string{"departmentIds"},
		}); err != nil {
			return Department{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Department{}, internal("Не удалось переместить отдел", err)
	}
	return departmentFromDB(row), nil
}

func (s *Service) ListPositions(ctx context.Context, actor Actor) ([]Position, error) {
	rows, err := db.New(s.pool).ListPositions(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить должности", err)
	}
	result := make([]Position, len(rows))
	for index, row := range rows {
		result[index] = positionFromDB(row)
	}
	return result, nil
}

func (s *Service) GetPosition(ctx context.Context, actor Actor, id uuid.UUID) (Position, error) {
	row, err := db.New(s.pool).GetPosition(ctx, db.GetPositionParams{CompanyID: actor.CompanyID, ID: id})
	if isNoRows(err) {
		return Position{}, notFound("Должность")
	}
	if err != nil {
		return Position{}, internal("Не удалось получить должность", err)
	}
	return positionFromDB(row), nil
}

func (s *Service) CreatePosition(ctx context.Context, actor Actor, input CreatePositionInput) (Position, error) {
	if err := requireAdministrator(actor); err != nil {
		return Position{}, err
	}
	name, err := requiredText(input.Name, "Укажите название должности")
	if err != nil {
		return Position{}, err
	}
	level := int16(0)
	if input.Level != nil {
		level = *input.Level
	}
	if level < 0 || level > 4 {
		return Position{}, validation("Уровень должности должен быть от 0 до 4")
	}
	queries := db.New(s.pool)
	if _, err = queries.GetDepartment(ctx, db.GetDepartmentParams{CompanyID: actor.CompanyID, ID: input.DepartmentID}); isNoRows(err) {
		return Position{}, notFound("Отдел")
	} else if err != nil {
		return Position{}, internal("Не удалось проверить отдел", err)
	}
	description := trimmedOptional(input.Description)
	row, err := queries.CreatePosition(ctx, db.CreatePositionParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, Name: name,
		DepartmentID: input.DepartmentID, Level: level, Description: pgText(description),
	})
	if err != nil {
		return Position{}, internal("Не удалось создать должность", err)
	}
	return positionFromDB(row), nil
}

func (s *Service) UpdatePosition(ctx context.Context, actor Actor, input UpdatePositionInput) (Position, error) {
	if err := requireAdministrator(actor); err != nil {
		return Position{}, err
	}
	if input.Name != nil {
		name, err := requiredText(*input.Name, "Укажите название должности")
		if err != nil {
			return Position{}, err
		}
		input.Name = &name
	}
	if input.Level != nil && (*input.Level < 0 || *input.Level > 4) {
		return Position{}, validation("Уровень должности должен быть от 0 до 4")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Position{}, internal("Не удалось обновить должность", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if input.DepartmentID != nil {
		if _, err = queries.GetDepartment(ctx, db.GetDepartmentParams{CompanyID: actor.CompanyID, ID: *input.DepartmentID}); isNoRows(err) {
			return Position{}, notFound("Отдел")
		} else if err != nil {
			return Position{}, internal("Не удалось проверить отдел", err)
		}
	}
	affectedUserIDs := []uuid.UUID{}
	if input.DepartmentID != nil {
		affectedUserIDs, err = queries.GetPositionUserIDs(ctx, db.GetPositionUserIDsParams{
			CompanyID: actor.CompanyID, PositionID: input.ID,
		})
		if err != nil {
			return Position{}, internal("Не удалось получить сотрудников должности", err)
		}
	}
	description := trimmedOptional(input.Description)
	row, err := queries.UpdatePosition(ctx, db.UpdatePositionParams{
		Name: pgText(input.Name), DepartmentID: nullableUUID(input.DepartmentID),
		Level: optionalInt2(input.Level), SetDescription: input.SetDescription,
		Description: pgText(description), CompanyID: actor.CompanyID, ID: input.ID,
	})
	if isNoRows(err) {
		return Position{}, notFound("Должность")
	}
	if err != nil {
		return Position{}, internal("Не удалось обновить должность", err)
	}
	for _, userID := range affectedUserIDs {
		userRow, userErr := queries.GetUserWithPositions(ctx, db.GetUserWithPositionsParams{
			CompanyID: actor.CompanyID, ID: userID,
		})
		if userErr != nil {
			return Position{}, internal("Не удалось получить сотрудника должности", userErr)
		}
		departments, departmentsErr := queries.GetUserDepartmentClaims(ctx, db.GetUserDepartmentClaimsParams{
			CompanyID: actor.CompanyID, UserID: userID,
		})
		if departmentsErr != nil {
			return Position{}, internal("Не удалось получить отделы сотрудника", departmentsErr)
		}
		if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.org.user.updated.v1", map[string]any{
			"user":          userEventSnapshot(userFromJoinedRow(userRow), departments),
			"changedFields": []string{"departmentIds"},
		}); err != nil {
			return Position{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Position{}, internal("Не удалось обновить должность", err)
	}
	return positionFromDB(row), nil
}

func (s *Service) DeletePosition(ctx context.Context, actor Actor, id uuid.UUID) error {
	if err := requireAdministrator(actor); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return internal("Не удалось удалить должность", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	affectedUserIDs, err := queries.GetPositionUserIDs(ctx, db.GetPositionUserIDsParams{
		CompanyID: actor.CompanyID, PositionID: id,
	})
	if err != nil {
		return internal("Не удалось получить сотрудников должности", err)
	}
	rows, err := queries.DeletePosition(ctx, db.DeletePositionParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		return internal("Не удалось удалить должность", err)
	}
	if rows == 0 {
		return notFound("Должность")
	}
	if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.org.position.deleted.v1", map[string]any{
		"positionId": id.String(), "affectedUserIds": stringsFromUUIDs(affectedUserIDs),
	}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось удалить должность", err)
	}
	return nil
}

func (s *Service) MovePosition(ctx context.Context, actor Actor, id, departmentID uuid.UUID) (Position, error) {
	return s.UpdatePosition(ctx, actor, UpdatePositionInput{ID: id, DepartmentID: &departmentID})
}

func (s *Service) ListUsers(ctx context.Context, actor Actor) ([]User, error) {
	if err := s.syncAmoUsers(ctx, actor); err != nil {
		return nil, err
	}
	rows, err := db.New(s.pool).ListUsers(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить сотрудников", err)
	}
	result := make([]User, len(rows))
	for index, row := range rows {
		result[index] = userFromListRow(row)
	}
	return result, nil
}

func (s *Service) DeleteUser(ctx context.Context, actor Actor, id uuid.UUID) error {
	if err := requireAdministrator(actor); err != nil {
		return err
	}
	if id == actor.UserID {
		return validation("Нельзя удалить собственную учётную запись")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return internal("Не удалось удалить сотрудника", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	company, err := queries.GetCompany(ctx, actor.CompanyID)
	if err != nil {
		return internal("Не удалось получить компанию", err)
	}
	if company.OwnerID.Valid && company.OwnerID.UUID == id {
		return validation("Нельзя удалить владельца компании")
	}
	current, err := queries.GetUserWithPositions(ctx, db.GetUserWithPositionsParams{CompanyID: actor.CompanyID, ID: id})
	if isNoRows(err) {
		return notFound("Сотрудник")
	}
	if err != nil {
		return internal("Не удалось получить сотрудника", err)
	}
	if current.Source != "local" {
		return conflict("Сотрудников amoCRM нельзя удалять в TeamOS")
	}
	if err = queries.ReassignUserInvites(ctx, db.ReassignUserInvitesParams{
		ReplacementUserID: actor.UserID, CompanyID: actor.CompanyID, DeletedUserID: id,
	}); err != nil {
		return internal("Не удалось переназначить приглашения сотрудника", err)
	}
	if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.org.user.deactivated.v1", map[string]any{
		"userId": id.String(),
	}); err != nil {
		return err
	}
	rows, err := queries.DeleteLocalUser(ctx, db.DeleteLocalUserParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		return internal("Не удалось удалить сотрудника", err)
	}
	if rows == 0 {
		return conflict("Сотрудников amoCRM нельзя удалять в TeamOS")
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось удалить сотрудника", err)
	}
	return nil
}

func (s *Service) GetUser(ctx context.Context, actor Actor, id uuid.UUID) (User, error) {
	row, err := db.New(s.pool).GetUserWithPositions(ctx, db.GetUserWithPositionsParams{
		CompanyID: actor.CompanyID, ID: id,
	})
	if isNoRows(err) {
		return User{}, notFound("Сотрудник")
	}
	if err != nil {
		return User{}, internal("Не удалось получить сотрудника", err)
	}
	return userFromJoinedRow(row), nil
}

func (s *Service) CreateUser(ctx context.Context, actor Actor, input CreateUserInput) (User, error) {
	if err := requireAdministrator(actor); err != nil {
		return User{}, err
	}
	if err := validateRole(input.Role, false); err != nil {
		return User{}, err
	}
	positionIDs := toDomainIDs(input.PositionIDs)
	if err := domainorg.ValidatePositionAssignment(positionIDs); err != nil {
		return User{}, validation(err.Error())
	}
	firstName, err := requiredText(input.FirstName, "Укажите имя пользователя")
	if err != nil {
		return User{}, err
	}
	lastName, err := requiredText(input.LastName, "Укажите фамилию пользователя")
	if err != nil {
		return User{}, err
	}
	email, err := normalizeEmail(input.Email)
	if err != nil {
		return User{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, internal("Не удалось создать сотрудника", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = ensurePositions(ctx, queries, actor.CompanyID, input.PositionIDs); err != nil {
		return User{}, err
	}
	phone, err := normalizePhone(input.Phone)
	if err != nil {
		return User{}, err
	}
	row, err := queries.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, Email: email,
		FirstName: firstName, LastName: lastName, Phone: pgText(phone),
		Role: input.Role, Status: "active",
	})
	if isUniqueViolation(err) {
		return User{}, validation("Пользователь с таким email уже существует")
	}
	if err != nil {
		return User{}, internal("Не удалось создать сотрудника", err)
	}
	if len(input.PositionIDs) == 1 {
		if err = queries.AssignUserPosition(ctx, db.AssignUserPositionParams{
			CompanyID: actor.CompanyID, UserID: row.ID, PositionID: input.PositionIDs[0],
		}); err != nil {
			return User{}, internal("Не удалось назначить должность", err)
		}
	}
	departmentIDs, err := queries.GetUserDepartmentClaims(ctx, db.GetUserDepartmentClaimsParams{CompanyID: actor.CompanyID, UserID: row.ID})
	if err != nil {
		return User{}, internal("Не удалось получить отделы пользователя", err)
	}
	if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.org.user.created.v1", map[string]any{
		"user": userEventSnapshot(userFromDB(row, input.PositionIDs), departmentIDs),
	}); err != nil {
		return User{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return User{}, internal("Не удалось создать сотрудника", err)
	}
	return userFromDB(row, input.PositionIDs), nil
}

func (s *Service) UpdateUser(ctx context.Context, actor Actor, input UpdateUserInput) (User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, internal("Не удалось обновить сотрудника", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	user, err := s.updateUser(ctx, actor, input, db.New(tx))
	if err != nil {
		return User{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return User{}, internal("Не удалось обновить сотрудника", err)
	}
	return user, nil
}

func (s *Service) updateUser(ctx context.Context, actor Actor, input UpdateUserInput, queries *db.Queries) (User, error) {
	if err := requireAdministrator(actor); err != nil {
		return User{}, err
	}
	if input.Role != nil {
		if err := validateRole(*input.Role, true); err != nil {
			return User{}, err
		}
	}
	if input.Status != nil {
		if _, ok := validStatuses[*input.Status]; !ok {
			return User{}, validation("Некорректный статус пользователя")
		}
	}
	if input.SetPositionIDs {
		if err := domainorg.ValidatePositionAssignment(toDomainIDs(input.PositionIDs)); err != nil {
			return User{}, validation(err.Error())
		}
	}
	if input.FirstName != nil {
		value, err := requiredText(*input.FirstName, "Укажите имя пользователя")
		if err != nil {
			return User{}, err
		}
		input.FirstName = &value
	}
	if input.LastName != nil {
		value, err := requiredText(*input.LastName, "Укажите фамилию пользователя")
		if err != nil {
			return User{}, err
		}
		input.LastName = &value
	}
	if input.SetPhone {
		phone, phoneErr := normalizePhone(input.Phone)
		if phoneErr != nil {
			return User{}, phoneErr
		}
		input.Phone = phone
	}
	birthDate, err := optionalDate(input.SetBirthDate, input.BirthDate)
	if err != nil {
		return User{}, validation("Некорректная дата рождения")
	}
	hiredAt, err := optionalDate(input.SetHiredAt, input.HiredAt)
	if err != nil {
		return User{}, validation("Некорректная дата выхода на работу")
	}
	if input.SetVacationAllowance && input.VacationAllowance != nil && (*input.VacationAllowance < 0 || *input.VacationAllowance > 366) {
		return User{}, validation("Норма отпуска должна быть от 0 до 366 дней")
	}

	company, err := queries.GetCompany(ctx, actor.CompanyID)
	if err != nil {
		return User{}, internal("Не удалось получить компанию", err)
	}
	if input.Role != nil && *input.Role == "owner" && (!company.OwnerID.Valid || input.ID != company.OwnerID.UUID) {
		return User{}, validation("Роль владельца можно назначить только при передаче владения компанией")
	}
	current, err := queries.GetUserWithPositions(ctx, db.GetUserWithPositionsParams{
		CompanyID: actor.CompanyID, ID: input.ID,
	})
	if isNoRows(err) {
		return User{}, notFound("Сотрудник")
	}
	if err != nil {
		return User{}, internal("Не удалось получить сотрудника", err)
	}
	var nextRole *domainorg.UserRole
	if input.Role != nil {
		value := domainorg.UserRole(*input.Role)
		nextRole = &value
	}
	var nextStatus *domainorg.UserStatus
	if input.Status != nil {
		value := domainorg.UserStatus(*input.Status)
		nextStatus = &value
	}
	guardErr := domainorg.ValidateUserUpdate(
		domainorg.User{ID: domainorg.ID(current.ID.String()), Role: domainorg.UserRole(current.Role), Status: domainorg.UserStatus(current.Status)},
		domainorg.UserUpdateInput{Role: nextRole, Status: nextStatus},
		domainorg.UserUpdateContext{OwnerID: domainorg.ID(company.OwnerID.UUID.String()), CurrentUserID: domainorg.ID(actor.UserID.String())},
	)
	if guardErr != nil {
		return User{}, validation(guardErr.Error())
	}
	if input.SetPositionIDs {
		if err = ensurePositions(ctx, queries, actor.CompanyID, input.PositionIDs); err != nil {
			return User{}, err
		}
	}
	row, err := queries.UpdateUser(ctx, db.UpdateUserParams{
		FirstName: pgText(input.FirstName), LastName: pgText(input.LastName),
		SetPhone: input.SetPhone, Phone: pgText(input.Phone),
		SetBirthDate: input.SetBirthDate, BirthDate: birthDate,
		SetHiredAt: input.SetHiredAt, HiredAt: hiredAt,
		SetVacation: input.SetVacationAllowance, VacationAllowance: optionalInt2(input.VacationAllowance),
		Role: pgText(input.Role), Status: pgText(input.Status),
		CompanyID: actor.CompanyID, ID: input.ID,
	})
	if err != nil {
		return User{}, internal("Не удалось обновить сотрудника", err)
	}
	positions := current.PositionIds
	if input.SetPositionIDs {
		if err = queries.DeleteUserPositions(ctx, db.DeleteUserPositionsParams{CompanyID: actor.CompanyID, UserID: input.ID}); err != nil {
			return User{}, internal("Не удалось обновить должность", err)
		}
		if len(input.PositionIDs) == 1 {
			if err = queries.AssignUserPosition(ctx, db.AssignUserPositionParams{
				CompanyID: actor.CompanyID, UserID: input.ID, PositionID: input.PositionIDs[0],
			}); err != nil {
				return User{}, internal("Не удалось назначить должность", err)
			}
		}
		positions = input.PositionIDs
	}
	subject := "teamos.org.user.updated.v1"
	if row.Status == "deactivated" && current.Status != "deactivated" {
		subject = "teamos.org.user.deactivated.v1"
		if err = queries.RevokeAllUserSessions(ctx, db.RevokeAllUserSessionsParams{
			UserID: row.ID, RevokedAt: pgtype.Timestamptz{Time: s.now().UTC(), Valid: true},
		}); err != nil {
			return User{}, internal("Не удалось отозвать сессии пользователя", err)
		}
	}
	var eventPayload any
	if subject == "teamos.org.user.deactivated.v1" {
		eventPayload = map[string]any{"userId": row.ID.String()}
	} else {
		departments, claimErr := queries.GetUserDepartmentClaims(ctx, db.GetUserDepartmentClaimsParams{CompanyID: actor.CompanyID, UserID: row.ID})
		if claimErr != nil {
			return User{}, internal("Не удалось получить отделы пользователя", claimErr)
		}
		eventPayload = map[string]any{
			"user":          userEventSnapshot(userFromDB(row, positions), departments),
			"changedFields": changedUserFields(input),
		}
	}
	if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, subject, eventPayload); err != nil {
		return User{}, err
	}
	return userFromDB(row, positions), nil
}

func (s *Service) ListInvites(ctx context.Context, actor Actor) ([]Invite, error) {
	if err := requireAdministrator(actor); err != nil {
		return nil, err
	}
	rows, err := db.New(s.pool).ListInvites(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить приглашения", err)
	}
	result := make([]Invite, len(rows))
	for index, row := range rows {
		result[index] = inviteFromDB(row)
	}
	return result, nil
}

func (s *Service) InviteUser(ctx context.Context, actor Actor, input InviteUserInput) (Invite, error) {
	if err := requireAdministrator(actor); err != nil {
		return Invite{}, err
	}
	if err := validateRole(input.Role, true); err != nil {
		return Invite{}, err
	}
	if input.Role == "owner" {
		return Invite{}, validation("Нельзя назначить роль владельца через приглашение")
	}
	var email *string
	if input.Email != nil && strings.TrimSpace(*input.Email) != "" {
		normalized, err := normalizeEmail(*input.Email)
		if err != nil {
			return Invite{}, err
		}
		email = &normalized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Invite{}, internal("Не удалось создать приглашение", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = ensurePositions(ctx, queries, actor.CompanyID, optionalUUIDSlice(input.PositionID)); err != nil {
		return Invite{}, err
	}
	if input.DepartmentID != nil {
		if _, err = queries.GetDepartment(ctx, db.GetDepartmentParams{CompanyID: actor.CompanyID, ID: *input.DepartmentID}); isNoRows(err) {
			return Invite{}, notFound("Отдел")
		} else if err != nil {
			return Invite{}, internal("Не удалось проверить отдел", err)
		}
	}
	if email != nil {
		users, listErr := queries.ListUsers(ctx, actor.CompanyID)
		if listErr != nil {
			return Invite{}, internal("Не удалось проверить email", listErr)
		}
		domainUsers := make([]domainorg.User, len(users))
		for index, user := range users {
			domainUsers[index] = domainorg.User{ID: domainorg.ID(user.ID.String()), Email: user.Email, Status: domainorg.UserStatus(user.Status)}
		}
		if err = domainorg.ValidateInviteEmail(*email, domainUsers); err != nil {
			return Invite{}, validation(err.Error())
		}
		existing, findErr := queries.GetUserByEmailForUpdate(ctx, *email)
		if findErr == nil {
			role, status := input.Role, "invited"
			_, err = queries.UpdateUser(ctx, db.UpdateUserParams{
				Role: pgtype.Text{String: role, Valid: true}, Status: pgtype.Text{String: status, Valid: true},
				CompanyID: actor.CompanyID, ID: existing.ID,
			})
			if err != nil {
				return Invite{}, internal("Не удалось подготовить пользователя", err)
			}
			if err = replacePosition(ctx, queries, actor.CompanyID, existing.ID, input.PositionID); err != nil {
				return Invite{}, err
			}
		} else if isNoRows(findErr) {
			firstName := inviteFirstName(*email)
			created, createErr := queries.CreateUser(ctx, db.CreateUserParams{
				ID: uuid.New(), CompanyID: actor.CompanyID, Email: *email,
				FirstName: firstName, LastName: "Сотрудник", Role: input.Role, Status: "invited",
			})
			if createErr != nil {
				return Invite{}, internal("Не удалось подготовить пользователя", createErr)
			}
			if err = replacePosition(ctx, queries, actor.CompanyID, created.ID, input.PositionID); err != nil {
				return Invite{}, err
			}
		} else {
			return Invite{}, internal("Не удалось проверить пользователя", findErr)
		}
	}
	now := s.now().UTC()
	row, err := queries.CreateInvite(ctx, db.CreateInviteParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, Email: pgText(email), Token: uuid.NewString(),
		Role: input.Role, PositionID: nullableUUID(input.PositionID), DepartmentID: nullableUUID(input.DepartmentID),
		InvitedByID: actor.UserID, ExpiresAt: now.Add(s.inviteTTL),
	})
	if err != nil {
		return Invite{}, internal("Не удалось создать приглашение", err)
	}
	if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.org.invite.created.v1", map[string]any{
		"inviteId": row.ID.String(), "email": email, "token": row.Token,
		"role": orgRoleEventValue(row.Role), "positionId": uuidStringPointer(input.PositionID),
		"departmentId": uuidStringPointer(input.DepartmentID), "invitedById": actor.UserID.String(),
	}); err != nil {
		return Invite{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Invite{}, internal("Не удалось создать приглашение", err)
	}
	return inviteFromDB(row), nil
}

func (s *Service) ResendInvite(ctx context.Context, actor Actor, id uuid.UUID) (Invite, error) {
	if err := requireAdministrator(actor); err != nil {
		return Invite{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Invite{}, internal("Не удалось повторить приглашение", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	current, err := queries.GetInvite(ctx, db.GetInviteParams{CompanyID: actor.CompanyID, ID: id})
	if isNoRows(err) {
		return Invite{}, notFound("Приглашение")
	}
	if err != nil {
		return Invite{}, internal("Не удалось получить приглашение", err)
	}
	if current.Status != "pending" {
		return Invite{}, validation("Повторно отправить можно только ожидающие приглашения")
	}
	now := s.now().UTC()
	row, err := queries.ResendInvite(ctx, db.ResendInviteParams{
		CompanyID: actor.CompanyID, ID: id, CreatedAt: now, ExpiresAt: now.Add(s.inviteTTL),
	})
	if err != nil {
		return Invite{}, internal("Не удалось повторить приглашение", err)
	}
	if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.org.invite.created.v1", map[string]any{
		"inviteId": row.ID.String(), "email": textPointer(row.Email), "token": row.Token,
		"role": orgRoleEventValue(row.Role), "positionId": uuidPointerString(row.PositionID),
		"departmentId": uuidPointerString(row.DepartmentID), "invitedById": row.InvitedByID.String(),
	}); err != nil {
		return Invite{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Invite{}, internal("Не удалось повторить приглашение", err)
	}
	return inviteFromDB(row), nil
}

func (s *Service) RevokeInvite(ctx context.Context, actor Actor, id uuid.UUID) error {
	if err := requireAdministrator(actor); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return internal("Не удалось отозвать приглашение", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	current, err := queries.GetInvite(ctx, db.GetInviteParams{CompanyID: actor.CompanyID, ID: id})
	if isNoRows(err) {
		return notFound("Приглашение")
	}
	if err != nil {
		return internal("Не удалось получить приглашение", err)
	}
	if current.Status == "accepted" {
		return validation("Нельзя отозвать принятое приглашение")
	}
	if _, err = queries.RevokeInvite(ctx, db.RevokeInviteParams{CompanyID: actor.CompanyID, ID: id}); err != nil {
		return internal("Не удалось отозвать приглашение", err)
	}
	if current.Email.Valid {
		user, findErr := queries.GetUserByEmailForUpdate(ctx, current.Email.String)
		if findErr == nil && user.CompanyID == actor.CompanyID && user.Status == "invited" {
			status := "deactivated"
			_, err = queries.UpdateUser(ctx, db.UpdateUserParams{
				Status: pgtype.Text{String: status, Valid: true}, CompanyID: actor.CompanyID, ID: user.ID,
			})
			if err != nil {
				return internal("Не удалось деактивировать пользователя", err)
			}
		} else if findErr != nil && !isNoRows(findErr) {
			return internal("Не удалось проверить пользователя", findErr)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось отозвать приглашение", err)
	}
	return nil
}

func departmentFromDB(row db.Department) Department {
	return Department{ID: row.ID, Name: row.Name, ParentID: uuidPointer(row.ParentID), HeadUserID: uuidPointer(row.HeadUserID), ValuableFinalProduct: textPointer(row.ValuableFinalProduct), Order: row.Order}
}

func positionFromDB(row db.Position) Position {
	return Position{ID: row.ID, Name: row.Name, DepartmentID: row.DepartmentID, Level: row.Level, Description: textPointer(row.Description), ArticleIDs: append([]uuid.UUID(nil), row.ArticleIds...), RequiredCourseIDs: append([]uuid.UUID(nil), row.RequiredCourseIds...)}
}

func userFromJoinedRow(row db.GetUserWithPositionsRow) User {
	return User{ID: row.ID, CompanyID: row.CompanyID, Email: row.Email, FirstName: row.FirstName, LastName: row.LastName, AvatarURL: textPointer(row.AvatarUrl), Phone: textPointer(row.Phone), Role: row.Role, Status: row.Status, PositionIDs: append([]uuid.UUID(nil), row.PositionIds...), BirthDate: datePointer(row.BirthDate), HiredAt: datePointer(row.HiredAt), VacationAllowance: int16Pointer(row.VacationAllowance), CreatedAt: row.CreatedAt, Source: row.Source, AccessMode: row.AccessMode}
}

func userFromListRow(row db.ListUsersRow) User {
	return User{ID: row.ID, CompanyID: row.CompanyID, Email: row.Email, FirstName: row.FirstName, LastName: row.LastName, AvatarURL: textPointer(row.AvatarUrl), Phone: textPointer(row.Phone), Role: row.Role, Status: row.Status, PositionIDs: append([]uuid.UUID(nil), row.PositionIds...), BirthDate: datePointer(row.BirthDate), HiredAt: datePointer(row.HiredAt), VacationAllowance: int16Pointer(row.VacationAllowance), CreatedAt: row.CreatedAt, Source: row.Source, AccessMode: row.AccessMode}
}

func trimmedOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalInt2(value *int16) pgtype.Int2 {
	if value == nil {
		return pgtype.Int2{}
	}
	return pgtype.Int2{Int16: *value, Valid: true}
}

func optionalDate(set bool, value *string) (pgtype.Date, error) {
	if !set || value == nil || strings.TrimSpace(*value) == "" {
		return pgtype.Date{}, nil
	}
	parsed, err := time.Parse(time.DateOnly, *value)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, *value)
	}
	if err != nil {
		return pgtype.Date{}, err
	}
	return pgtype.Date{Time: parsed, Valid: true}, nil
}

func validateRole(role string, allowOwner bool) error {
	if _, ok := validRoles[role]; !ok || (!allowOwner && role == "owner") {
		return validation("Некорректная роль пользователя")
	}
	return nil
}

func toDomainIDs(values []uuid.UUID) []domainorg.ID {
	result := make([]domainorg.ID, len(values))
	for index, value := range values {
		result[index] = domainorg.ID(value.String())
	}
	return result
}

func ensurePositions(ctx context.Context, queries *db.Queries, companyID uuid.UUID, ids []uuid.UUID) error {
	for _, id := range ids {
		if _, err := queries.GetPosition(ctx, db.GetPositionParams{CompanyID: companyID, ID: id}); isNoRows(err) {
			return notFound("Должность")
		} else if err != nil {
			return internal("Не удалось проверить должность", err)
		}
	}
	return nil
}

func replacePosition(ctx context.Context, queries *db.Queries, companyID, userID uuid.UUID, positionID *uuid.UUID) error {
	if err := queries.DeleteUserPositions(ctx, db.DeleteUserPositionsParams{CompanyID: companyID, UserID: userID}); err != nil {
		return internal("Не удалось обновить должность", err)
	}
	if positionID != nil {
		if err := queries.AssignUserPosition(ctx, db.AssignUserPositionParams{CompanyID: companyID, UserID: userID, PositionID: *positionID}); err != nil {
			return internal("Не удалось назначить должность", err)
		}
	}
	return nil
}

func optionalUUIDSlice(value *uuid.UUID) []uuid.UUID {
	if value == nil {
		return nil
	}
	return []uuid.UUID{*value}
}

func inviteFirstName(email string) string {
	local := strings.SplitN(email, "@", 2)[0]
	parts := strings.FieldsFunc(local, func(r rune) bool { return r == '.' || r == '_' || r == '-' })
	if len(parts) == 0 || parts[0] == "" {
		return "Новый"
	}
	name := []rune(parts[0])
	upper := []rune(strings.ToUpper(string(name[0])))
	if len(upper) > 0 {
		name[0] = upper[0]
	}
	return string(name)
}

func changedUserFields(input UpdateUserInput) []string {
	fields := make([]string, 0, 9)
	if input.FirstName != nil {
		fields = append(fields, "firstName")
	}
	if input.LastName != nil {
		fields = append(fields, "lastName")
	}
	if input.SetPhone {
		fields = append(fields, "phone")
	}
	if input.SetBirthDate {
		fields = append(fields, "birthDate")
	}
	if input.SetHiredAt {
		fields = append(fields, "hiredAt")
	}
	if input.SetVacationAllowance {
		fields = append(fields, "vacationAllowance")
	}
	if input.Role != nil {
		fields = append(fields, "role")
	}
	if input.Status != nil {
		fields = append(fields, "status")
	}
	if input.SetPositionIDs {
		fields = append(fields, "positionIds")
	}
	return fields
}

func uuidStringPointer(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	result := value.String()
	return &result
}

func uuidPointerString(value uuid.NullUUID) *string {
	if !value.Valid {
		return nil
	}
	result := value.UUID.String()
	return &result
}
