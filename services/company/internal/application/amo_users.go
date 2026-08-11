package application

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func (s *Service) syncAmoUsers(ctx context.Context, actor Actor) error {
	if s.externalUsers == nil {
		return nil
	}
	unlock, ok := s.tryStartAmoSync(actor.CompanyID)
	if !ok {
		return nil
	}
	defer unlock()

	if err := s.syncAmoUsersNow(ctx, actor); err != nil {
		s.logger.WarnContext(ctx, "amoCRM user import failed; serving TeamOS users", "company_id", actor.CompanyID, "error", err)
	}
	return nil
}

func (s *Service) tryStartAmoSync(companyID uuid.UUID) (func(), bool) {
	s.amoSyncMu.Lock()
	state := s.amoSyncStates[companyID]
	if state == nil {
		state = &amoSyncState{}
		s.amoSyncStates[companyID] = state
	}
	s.amoSyncMu.Unlock()

	if !state.mu.TryLock() {
		return nil, false
	}
	now := s.now().UTC()
	if !state.lastAttempt.IsZero() && now.Sub(state.lastAttempt) < s.amoSyncTTL {
		state.mu.Unlock()
		return nil, false
	}
	// Throttle both successful attempts and failures so an unavailable upstream
	// cannot turn every org-tree read into a slow external request.
	state.lastAttempt = now
	return state.mu.Unlock, true
}

func (s *Service) syncAmoUsersNow(ctx context.Context, actor Actor) error {
	if s.externalUsers == nil {
		return nil
	}
	company, err := db.New(s.pool).GetCompany(ctx, actor.CompanyID)
	if err != nil {
		return internal("Не удалось получить настройки amoCRM", err)
	}
	amoAccountID := textPointer(company.AmoAccountID)
	if amoAccountID == nil {
		return nil
	}
	employees, err := s.externalUsers.FetchAll(ctx, *amoAccountID)
	if err != nil {
		return upstream("Не удалось получить сотрудников из amoCRM", err)
	}
	normalized, err := normalizeExternalEmployees(actor.CompanyID, employees)
	if err != nil {
		return upstream("Внешний API вернул некорректных сотрудников", err)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return internal("Не удалось начать импорт сотрудников", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = queries.LockAmoUserSync(ctx, actor.CompanyID); err != nil {
		return internal("Не удалось заблокировать импорт сотрудников", err)
	}
	amoUsers, err := queries.ListAmoUsersForReconciliation(ctx, actor.CompanyID)
	if err != nil {
		return internal("Не удалось получить сотрудников amoCRM для сверки", err)
	}
	groupDepartments, err := ensureAmoGroupDepartments(ctx, queries, actor.CompanyID, normalized)
	if err != nil {
		return err
	}
	snapshotIDs := make(map[string]struct{}, len(normalized))
	for _, employee := range normalized {
		snapshotIDs[employee.ID] = struct{}{}
	}

	for _, employee := range normalized {
		departmentID := groupDepartments[employee.GroupID]
		existing, findErr := queries.FindUserForAmoSync(ctx, db.FindUserForAmoSyncParams{
			CompanyID: actor.CompanyID, ExternalID: pgtype.Text{String: employee.ID, Valid: true}, Email: employee.Email,
		})
		if findErr == nil {
			// Импорт не перезаписывает профиль, статус или доступ уже известного TeamOS-пользователя.
			restored := false
			current := existing
			if existing.Source == "amo" && existing.ExternalDeletedAt.Valid {
				restoredUser, restoreErr := queries.ClearAmoUserTombstone(ctx, db.ClearAmoUserTombstoneParams{
					CompanyID: actor.CompanyID, ID: existing.ID,
				})
				if restoreErr != nil {
					return internal("Не удалось восстановить сотрудника amoCRM", restoreErr)
				}
				current = restoredUser
				restored = true
			}
			departmentChanged, organizationErr := syncAmoUserDepartment(
				ctx, queries, actor.CompanyID, current, employee, departmentID,
			)
			if organizationErr != nil {
				return organizationErr
			}
			if restored || departmentChanged {
				sections, sectionsErr := queries.ListEmployeeSectionAccess(ctx, db.ListEmployeeSectionAccessParams{
					CompanyID: actor.CompanyID, UserID: existing.ID,
				})
				if sectionsErr != nil {
					return internal("Не удалось получить доступ к разделам сотрудника", sectionsErr)
				}
				positionIDs, positionsErr := queries.GetUserPositionIDs(ctx, db.GetUserPositionIDsParams{
					CompanyID: actor.CompanyID, UserID: existing.ID,
				})
				if positionsErr != nil {
					return internal("Не удалось получить должности сотрудника", positionsErr)
				}
				departmentIDs, departmentsErr := queries.GetUserDepartmentClaims(ctx, db.GetUserDepartmentClaimsParams{
					CompanyID: actor.CompanyID, UserID: existing.ID,
				})
				if departmentsErr != nil {
					return internal("Не удалось получить отделы сотрудника", departmentsErr)
				}
				updatedUser := userFromDB(current, positionIDs)
				if current.Role == "employee" {
					updatedUser.SectionAccess = normalizedEmployeeSections(sections)
				}
				changedFields := make([]string, 0, 3)
				if restored {
					changedFields = append(changedFields, "externalDeletedAt")
				}
				if departmentChanged {
					changedFields = append(changedFields, "departmentIds")
				}
				if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.org.user.updated.v1", map[string]any{
					"user": userEventSnapshot(updatedUser, departmentIDs), "changedFields": changedFields,
				}); err != nil {
					return err
				}
			}
			if restored {
				if err = createUserAdminAudit(
					ctx, queries, actor.CompanyID, &existing.ID, nil, "amo_sync", "external_restored",
					map[string]any{"externalPresent": false}, map[string]any{"externalPresent": true},
					actor.RequestID, s.now().UTC(),
				); err != nil {
					return err
				}
			}
			continue
		}
		if !isNoRows(findErr) {
			return internal("Не удалось сопоставить сотрудника amoCRM", findErr)
		}
		row, err := queries.CreateAmoUser(ctx, db.CreateAmoUserParams{
			ID: uuid.New(), CompanyID: actor.CompanyID, Email: employee.Email,
			FirstName: employee.FirstName, LastName: pgText(&employee.LastName),
			AvatarUrl: pgText(employee.AvatarURL), ExternalID: pgtype.Text{String: employee.ID, Valid: true},
			AvatarSource:      avatarSource(employee.AvatarURL),
			ExternalGroupID:   pgText(trimmedStringPointer(employee.GroupID)),
			ExternalGroupName: pgText(trimmedStringPointer(employee.GroupName)),
		})
		if isUniqueViolation(err) {
			return conflict("Не удалось сопоставить сотрудников amoCRM: email уже занят")
		}
		if err != nil {
			return internal("Не удалось сохранить сотрудника amoCRM", err)
		}
		if _, organizationErr := syncAmoUserDepartment(
			ctx, queries, actor.CompanyID, row, employee, departmentID,
		); organizationErr != nil {
			return organizationErr
		}
		sections := append([]string(nil), defaultEmployeeSections...)
		if err = replaceEmployeeSections(ctx, queries, actor.CompanyID, row.ID, sections, nil); err != nil {
			return err
		}
		departmentIDs := []uuid.UUID(nil)
		if departmentID != nil {
			departmentIDs = []uuid.UUID{*departmentID}
		}
		createdUser := userFromDB(row, nil)
		createdUser.DepartmentIDs = departmentIDs
		createdUser.SectionAccess = normalizedEmployeeSections(sections)
		if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.org.user.created.v1", map[string]any{
			"user": userEventSnapshot(createdUser, departmentIDs),
		}); err != nil {
			return err
		}
	}
	for _, amoUser := range amoUsers {
		if !amoUser.ExternalID.Valid {
			continue
		}
		if _, present := snapshotIDs[amoUser.ExternalID.String]; present || amoUser.ExternalDeletedAt.Valid {
			continue
		}
		if _, err = queries.MarkAmoUserExternallyDeleted(ctx, db.MarkAmoUserExternallyDeletedParams{
			DeletedAt: pgtype.Timestamptz{Time: s.now().UTC(), Valid: true},
			CompanyID: actor.CompanyID, ID: amoUser.ID,
		}); err != nil {
			return internal("Не удалось скрыть удалённого сотрудника amoCRM", err)
		}
		if err = revokeUserSessions(ctx, queries, amoUser.ID, s.now().UTC()); err != nil {
			return err
		}
		if err = queries.DeleteCredential(ctx, db.DeleteCredentialParams{
			CompanyID: actor.CompanyID, UserID: amoUser.ID,
		}); err != nil {
			return internal("Не удалось удалить пароль сотрудника amoCRM", err)
		}
		if err = queries.DeleteAccessLink(ctx, db.DeleteAccessLinkParams{
			CompanyID: actor.CompanyID, UserID: amoUser.ID,
		}); err != nil {
			return internal("Не удалось удалить ссылку доступа сотрудника amoCRM", err)
		}
		if err = queries.DisableUserInDistributionGroups(ctx, db.DisableUserInDistributionGroupsParams{
			UserID: amoUser.ID, CompanyID: actor.CompanyID,
		}); err != nil {
			return internal("Не удалось отключить сотрудника amoCRM в распределении", err)
		}
		if err = s.emit(ctx, queries, actor.CompanyID, actor.UserID, "teamos.org.user.deactivated.v1", map[string]any{
			"userId": amoUser.ID.String(),
		}); err != nil {
			return err
		}
		if err = createUserAdminAudit(
			ctx, queries, actor.CompanyID, &amoUser.ID, nil, "amo_sync", "external_removed",
			map[string]any{"externalPresent": true}, map[string]any{"externalPresent": false},
			actor.RequestID, s.now().UTC(),
		); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось завершить импорт сотрудников", err)
	}
	return nil
}

type normalizedExternalEmployee struct {
	ID, Email, FirstName, LastName, GroupID, GroupName string
	AvatarURL                                          *string
}

type normalizedAmoGroup struct {
	ID   string
	Name string
}

func ensureAmoGroupDepartments(
	ctx context.Context,
	queries *db.Queries,
	companyID uuid.UUID,
	employees []normalizedExternalEmployee,
) (map[string]*uuid.UUID, error) {
	groups, err := collectAmoGroups(employees)
	if err != nil {
		return nil, upstream("Внешний API вернул некорректные группы сотрудников", err)
	}
	result := make(map[string]*uuid.UUID, len(groups))
	for _, group := range groups {
		department, createErr := queries.UpsertAmoDepartment(ctx, db.UpsertAmoDepartmentParams{
			ID: uuid.New(), CompanyID: companyID, Name: group.Name, ExternalID: pgtype.Text{String: group.ID, Valid: true},
		})
		if createErr != nil {
			return nil, internal("Не удалось создать отдел из группы amoCRM", createErr)
		}
		departmentID := department.ID
		result[group.ID] = &departmentID
	}
	return result, nil
}

func collectAmoGroups(employees []normalizedExternalEmployee) ([]normalizedAmoGroup, error) {
	groups := make(map[string]string)
	for _, employee := range employees {
		groupID := strings.TrimSpace(employee.GroupID)
		groupName := strings.TrimSpace(employee.GroupName)
		if groupID == "" && groupName == "" {
			continue
		}
		if groupID == "" || groupName == "" {
			return nil, fmt.Errorf("сотрудник %s: группа должна содержать id и название", employee.ID)
		}
		if existing, ok := groups[groupID]; ok && existing != groupName {
			return nil, fmt.Errorf("группа %s имеет разные названия %q и %q", groupID, existing, groupName)
		}
		groups[groupID] = groupName
	}
	result := make([]normalizedAmoGroup, 0, len(groups))
	for id, name := range groups {
		result = append(result, normalizedAmoGroup{ID: id, Name: name})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].ID < result[right].ID })
	return result, nil
}

func syncAmoUserDepartment(
	ctx context.Context,
	queries *db.Queries,
	companyID uuid.UUID,
	user db.User,
	employee normalizedExternalEmployee,
	departmentID *uuid.UUID,
) (bool, error) {
	if user.Source != "amo" {
		return false, nil
	}
	groupID := pgText(trimmedStringPointer(employee.GroupID))
	groupName := pgText(trimmedStringPointer(employee.GroupName))
	if !sameNullableText(user.ExternalGroupID, groupID) || !sameNullableText(user.ExternalGroupName, groupName) {
		if _, err := queries.UpdateAmoUserGroup(ctx, db.UpdateAmoUserGroupParams{
			ExternalGroupID: groupID, ExternalGroupName: groupName, CompanyID: companyID, ID: user.ID,
		}); err != nil {
			return false, internal("Не удалось обновить группу сотрудника amoCRM", err)
		}
	}
	if departmentID != nil {
		changed, err := queries.AssignAmoUserDepartment(ctx, db.AssignAmoUserDepartmentParams{
			CompanyID: companyID, UserID: user.ID, DepartmentID: *departmentID,
		})
		if err != nil {
			return false, internal("Не удалось назначить сотрудника в отдел amoCRM", err)
		}
		return changed > 0, nil
	}
	if !user.ExternalGroupID.Valid && !user.ExternalGroupName.Valid {
		return false, nil
	}
	changed, err := queries.ClearAmoUserDepartment(ctx, db.ClearAmoUserDepartmentParams{
		CompanyID: companyID, UserID: user.ID,
	})
	if err != nil {
		return false, internal("Не удалось убрать сотрудника из отдела amoCRM", err)
	}
	return changed > 0, nil
}

func sameNullableText(left, right pgtype.Text) bool {
	return left.Valid == right.Valid && (!left.Valid || left.String == right.String)
}

func normalizeExternalEmployees(companyID uuid.UUID, values []ExternalEmployee) ([]normalizedExternalEmployee, error) {
	result := make([]normalizedExternalEmployee, 0, len(values))
	ids := make(map[string]struct{}, len(values))
	emails := make(map[string]struct{}, len(values))
	for index, value := range values {
		id := strings.TrimSpace(value.ID)
		if id == "" {
			return nil, fmt.Errorf("сотрудник %d: отсутствует id", index+1)
		}
		if _, exists := ids[id]; exists {
			return nil, fmt.Errorf("сотрудник %d: повторяется id %q", index+1, id)
		}
		ids[id] = struct{}{}
		firstName, lastName, _ := splitEmployeeName(value.Name)
		email := fallbackAmoEmail(companyID, id)
		if value.Email != nil && strings.TrimSpace(*value.Email) != "" {
			candidate := strings.ToLower(strings.TrimSpace(*value.Email))
			address, parseErr := mail.ParseAddress(candidate)
			if parseErr != nil || address.Address != candidate {
				return nil, fmt.Errorf("сотрудник %s: некорректный email", id)
			}
			email = candidate
		}
		if _, exists := emails[email]; exists {
			return nil, fmt.Errorf("сотрудник %s: повторяется email %q", id, email)
		}
		emails[email] = struct{}{}
		result = append(result, normalizedExternalEmployee{
			ID: id, Email: email, FirstName: firstName, LastName: lastName,
			AvatarURL: trimmedStringPointerValue(value.AvatarURL),
			GroupID:   strings.TrimSpace(value.GroupID), GroupName: strings.TrimSpace(value.GroupName),
		})
	}
	return result, nil
}

func splitEmployeeName(value string) (string, string, bool) {
	parts := strings.FieldsFunc(strings.TrimSpace(value), unicode.IsSpace)
	if len(parts) == 0 {
		return "Сотрудник", "", false
	}
	if len(parts) == 1 {
		return parts[0], "", false
	}
	return parts[0], strings.Join(parts[1:], " "), true
}

func fallbackAmoEmail(companyID uuid.UUID, externalID string) string {
	digest := sha256.Sum256([]byte(externalID))
	return fmt.Sprintf("amo-%s-%x@users.invalid", companyID.String(), digest[:8])
}

func trimmedStringPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func trimmedStringPointerValue(value *string) *string {
	if value == nil {
		return nil
	}
	return trimmedStringPointer(*value)
}

func avatarSource(avatarURL *string) pgtype.Text {
	if avatarURL == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: "amo", Valid: true}
}
