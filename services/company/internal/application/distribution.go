package application

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	domaindistribution "github.com/sk1fy/team-os-backend/services/company/internal/domain/distribution"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func (s *Service) ListDistributionGroups(ctx context.Context, actor Actor) ([]DistributionGroup, error) {
	if err := requireAdministrator(actor); err != nil {
		return nil, err
	}
	rows, err := db.New(s.pool).ListDistributionGroups(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить группы распределения", err)
	}
	result := make([]DistributionGroup, len(rows))
	for i, row := range rows {
		result[i] = distributionGroupFromDB(row)
	}
	return result, nil
}

func (s *Service) CreateDistributionGroup(ctx context.Context, actor Actor, input CreateDistributionGroupInput) (DistributionGroup, error) {
	if err := requireAdministrator(actor); err != nil {
		return DistributionGroup{}, err
	}
	name, err := requiredText(input.Name, "Укажите название группы")
	if err != nil {
		return DistributionGroup{}, err
	}
	members := uniqueUUIDs(input.MemberIDs)
	if len(members) == 0 {
		return DistributionGroup{}, validation("Добавьте в группу хотя бы одного сотрудника")
	}
	if err = s.validateDistributionMembers(ctx, actor.CompanyID, members); err != nil {
		return DistributionGroup{}, err
	}
	description := normalizeOptionalText(input.Description)
	row, err := db.New(s.pool).CreateDistributionGroup(ctx, db.CreateDistributionGroupParams{ID: uuid.New(), CompanyID: actor.CompanyID, Name: name, Description: pgText(description), MemberIds: members})
	if err != nil {
		return DistributionGroup{}, internal("Не удалось создать группу распределения", err)
	}
	return distributionGroupFromDB(row), nil
}

func (s *Service) UpdateDistributionGroup(ctx context.Context, actor Actor, input UpdateDistributionGroupInput) (DistributionGroup, error) {
	if err := requireAdministrator(actor); err != nil {
		return DistributionGroup{}, err
	}
	queries := db.New(s.pool)
	current, err := queries.GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: actor.CompanyID, ID: input.ID})
	if err != nil {
		if isNoRows(err) {
			return DistributionGroup{}, notFound("Группа распределения")
		}
		return DistributionGroup{}, internal("Не удалось получить группу распределения", err)
	}
	params := db.UpdateDistributionGroupParams{CompanyID: actor.CompanyID, ID: input.ID}
	if input.Name != nil {
		params.SetName = true
		params.Name, err = requiredText(*input.Name, "Укажите название группы")
		if err != nil {
			return DistributionGroup{}, err
		}
	}
	params.SetDescription, params.Description = input.SetDescription, pgText(normalizeOptionalText(input.Description))
	if input.Active != nil {
		params.SetActive, params.Active = true, *input.Active
	}
	if input.Algorithm != nil {
		if !validAlgorithm(*input.Algorithm) {
			return DistributionGroup{}, validation("Неизвестный алгоритм распределения")
		}
		params.SetAlgorithm, params.Algorithm = true, *input.Algorithm
	}
	members := current.MemberIds
	if input.SetMemberIDs {
		members = uniqueUUIDs(input.MemberIDs)
		if len(members) == 0 {
			return DistributionGroup{}, validation("Добавьте в группу хотя бы одного сотрудника")
		}
		if err = s.validateDistributionMembers(ctx, actor.CompanyID, members); err != nil {
			return DistributionGroup{}, err
		}
		params.SetMemberIds, params.MemberIds = true, members
	}
	disabled := current.DisabledMemberIds
	if input.SetDisabledMemberIDs {
		disabled = uniqueUUIDs(input.DisabledMemberIDs)
		params.SetDisabledMemberIds = true
	}
	disabled = intersection(disabled, members)
	if input.SetMemberIDs || input.SetDisabledMemberIDs {
		params.SetDisabledMemberIds, params.DisabledMemberIds = true, disabled
	}
	if input.Source != nil {
		params.SetSource, params.Source = true, strings.TrimSpace(*input.Source)
	}
	if input.DealLimit != nil {
		if *input.DealLimit < 1 {
			return DistributionGroup{}, validation("Лимит сделок должен быть не меньше 1")
		}
		params.SetDealLimit, params.DealLimit = true, *input.DealLimit
	}
	if input.UnclaimedMinutes != nil {
		if *input.UnclaimedMinutes < 1 {
			return DistributionGroup{}, validation("Время ожидания должно быть не меньше 1 минуты")
		}
		params.SetUnclaimedMinutes, params.UnclaimedMinutes = true, *input.UnclaimedMinutes
	}
	row, err := queries.UpdateDistributionGroup(ctx, params)
	if err != nil {
		return DistributionGroup{}, internal("Не удалось обновить группу распределения", err)
	}
	return distributionGroupFromDB(row), nil
}

func (s *Service) DeleteDistributionGroup(ctx context.Context, actor Actor, id uuid.UUID) error {
	if err := requireAdministrator(actor); err != nil {
		return err
	}
	rows, err := db.New(s.pool).DeleteDistributionGroup(ctx, db.DeleteDistributionGroupParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		return internal("Не удалось удалить группу распределения", err)
	}
	if rows == 0 {
		return notFound("Группа распределения")
	}
	return nil
}

func (s *Service) ListDistributionEvents(ctx context.Context, actor Actor, groupID uuid.UUID) ([]DistributionEvent, error) {
	if err := requireAdministrator(actor); err != nil {
		return nil, err
	}
	queries := db.New(s.pool)
	if _, err := queries.GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: actor.CompanyID, ID: groupID}); err != nil {
		if isNoRows(err) {
			return nil, notFound("Группа распределения")
		}
		return nil, internal("Не удалось получить группу распределения", err)
	}
	rows, err := queries.ListDistributionEvents(ctx, db.ListDistributionEventsParams{CompanyID: actor.CompanyID, GroupID: groupID})
	if err != nil {
		return nil, internal("Не удалось получить события распределения", err)
	}
	result := make([]DistributionEvent, len(rows))
	for i, row := range rows {
		result[i] = distributionEventFromDB(row)
	}
	return result, nil
}

func (s *Service) SimulateDistributionDeal(ctx context.Context, actor Actor, groupID uuid.UUID) (DistributionEvent, error) {
	if err := requireAdministrator(actor); err != nil {
		return DistributionEvent{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return DistributionEvent{}, internal("Не удалось начать распределение сделки", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	group, err := queries.GetDistributionGroupForUpdate(ctx, db.GetDistributionGroupForUpdateParams{CompanyID: actor.CompanyID, ID: groupID})
	if err != nil {
		if isNoRows(err) {
			return DistributionEvent{}, notFound("Группа распределения")
		}
		return DistributionEvent{}, internal("Не удалось получить группу распределения", err)
	}
	if !group.Active {
		return DistributionEvent{}, validation("Группа приостановлена")
	}
	eventRows, err := queries.ListDistributionEvents(ctx, db.ListDistributionEventsParams{CompanyID: actor.CompanyID, GroupID: groupID})
	if err != nil {
		return DistributionEvent{}, internal("Не удалось получить историю распределения", err)
	}
	events := make([]domaindistribution.Event, len(eventRows))
	for i, row := range eventRows {
		events[i] = domaindistribution.Event{GroupID: row.GroupID, UserID: row.UserID, Status: row.Status, CreatedAt: row.CreatedAt}
	}
	userID, err := domaindistribution.PickMember(domaindistribution.Group{ID: group.ID, Algorithm: domaindistribution.Algorithm(group.Algorithm), MemberIDs: group.MemberIds, DisabledMemberIDs: group.DisabledMemberIds}, events)
	if errors.Is(err, domaindistribution.ErrNoEnabledMembers) {
		return DistributionEvent{}, validation("В группе нет сотрудников")
	}
	if err != nil {
		return DistributionEvent{}, internal("Не удалось выбрать сотрудника", err)
	}
	row, err := queries.CreateDistributionEvent(ctx, db.CreateDistributionEventParams{ID: uuid.New(), CompanyID: actor.CompanyID, GroupID: groupID, UserID: userID})
	if err != nil {
		return DistributionEvent{}, internal("Не удалось сохранить распределение сделки", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return DistributionEvent{}, internal("Не удалось завершить распределение сделки", err)
	}
	return distributionEventFromDB(row), nil
}

func (s *Service) ResetDistributionEvents(ctx context.Context, actor Actor, groupID uuid.UUID) error {
	if err := requireAdministrator(actor); err != nil {
		return err
	}
	queries := db.New(s.pool)
	if _, err := queries.GetDistributionGroup(ctx, db.GetDistributionGroupParams{CompanyID: actor.CompanyID, ID: groupID}); err != nil {
		if isNoRows(err) {
			return notFound("Группа распределения")
		}
		return internal("Не удалось получить группу распределения", err)
	}
	if _, err := queries.ResetDistributionEvents(ctx, db.ResetDistributionEventsParams{CompanyID: actor.CompanyID, GroupID: groupID}); err != nil {
		return internal("Не удалось очистить события распределения", err)
	}
	return nil
}

func (s *Service) validateDistributionMembers(ctx context.Context, companyID uuid.UUID, ids []uuid.UUID) error {
	queries := db.New(s.pool)
	for _, id := range ids {
		if _, err := queries.GetUserWithPositions(ctx, db.GetUserWithPositionsParams{CompanyID: companyID, ID: id}); err != nil {
			if isNoRows(err) {
				return notFound("Сотрудник")
			}
			return internal("Не удалось проверить сотрудника", err)
		}
	}
	return nil
}
func distributionGroupFromDB(row db.DistributionGroup) DistributionGroup {
	return DistributionGroup{ID: row.ID, Name: row.Name, Description: textPointer(row.Description), Active: row.Active, Algorithm: row.Algorithm, MemberIDs: append([]uuid.UUID(nil), row.MemberIds...), DisabledMemberIDs: append([]uuid.UUID(nil), row.DisabledMemberIds...), Source: row.Source, DealLimit: row.DealLimit, UnclaimedMinutes: row.UnclaimedMinutes, CreatedAt: row.CreatedAt}
}
func distributionEventFromDB(row db.DistributionEvent) DistributionEvent {
	return DistributionEvent{ID: row.ID, GroupID: row.GroupID, DealNumber: row.DealNumber, UserID: row.UserID, Status: row.Status, CreatedAt: row.CreatedAt}
}
func uniqueUUIDs(values []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]struct{}{}
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		if value == uuid.Nil {
			continue
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}
func intersection(values, allowed []uuid.UUID) []uuid.UUID {
	set := map[uuid.UUID]struct{}{}
	for _, value := range allowed {
		set[value] = struct{}{}
	}
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range uniqueUUIDs(values) {
		if _, ok := set[value]; ok {
			result = append(result, value)
		}
	}
	return result
}
func normalizeOptionalText(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}
func validAlgorithm(value string) bool {
	return value == "round_robin" || value == "least_loaded" || value == "priority"
}
