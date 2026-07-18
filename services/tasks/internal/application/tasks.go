package application

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/pkg/richtext"
	domainboard "github.com/sk1fy/team-os-backend/services/tasks/internal/domain/board"
	domainrecurrence "github.com/sk1fy/team-os-backend/services/tasks/internal/domain/recurrence"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/storage/db"
)

func (s *Service) GetTasks(ctx context.Context, actor Actor, boardID *uuid.UUID) ([]Task, error) {
	if !canUseTasks(actor) {
		return nil, forbidden("Недостаточно прав для просмотра задач")
	}
	params := db.ListTasksParams{CompanyID: actor.CompanyID}
	if boardID != nil {
		params.BoardID = uuid.NullUUID{UUID: *boardID, Valid: true}
	}
	rows, err := db.New(s.pool).ListTasks(ctx, params)
	if err != nil {
		return nil, internal("Не удалось получить задачи", err)
	}
	tasks := make([]Task, 0, len(rows))
	for _, row := range rows {
		task, mapErr := taskFromDB(row)
		if mapErr != nil {
			return nil, mapErr
		}
		if canAccessTask(actor, task) {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func (s *Service) GetTask(ctx context.Context, actor Actor, id uuid.UUID) (Task, error) {
	if !canUseTasks(actor) {
		return Task{}, forbidden("Недостаточно прав для просмотра задачи")
	}
	row, err := db.New(s.pool).GetTask(ctx, db.GetTaskParams{CompanyID: actor.CompanyID, ID: id})
	if err != nil {
		if isNoRows(err) {
			return Task{}, notFound("Задача")
		}
		return Task{}, internal("Не удалось получить задачу", err)
	}
	task, err := taskFromDB(row)
	if err != nil {
		return Task{}, err
	}
	if !canAccessTask(actor, task) {
		return Task{}, forbidden("Недостаточно прав для просмотра задачи")
	}
	return task, nil
}

type CreateTaskInput struct {
	BoardID  uuid.UUID
	ColumnID uuid.UUID
	Title    string
	Priority string
}

func (s *Service) CreateTask(ctx context.Context, actor Actor, input CreateTaskInput) (Task, error) {
	if !canCreateTask(actor) {
		return Task{}, forbidden("Недостаточно прав для создания задачи")
	}
	title, err := requiredText(input.Title, "Укажите название задачи")
	if err != nil {
		return Task{}, err
	}
	priority := input.Priority
	if priority == "" {
		priority = "medium"
	}
	if err = validatePriority(priority); err != nil {
		return Task{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Task{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = queries.LockBoardOrder(ctx, input.BoardID); err != nil {
		return Task{}, internal("Не удалось заблокировать порядок доски", err)
	}
	column, err := queries.GetColumn(ctx, db.GetColumnParams{
		CompanyID: actor.CompanyID, ID: input.ColumnID,
	})
	if err != nil {
		if isNoRows(err) {
			return Task{}, notFound("Колонка")
		}
		return Task{}, internal("Не удалось проверить колонку", err)
	}
	if column.BoardID != input.BoardID {
		return Task{}, validation("Колонка не принадлежит указанной доске")
	}
	count, err := queries.CountTasksInColumn(ctx, input.ColumnID)
	if err != nil {
		return Task{}, internal("Не удалось определить порядок задачи", err)
	}

	checklistJSON, err := checklistToJSON(nil)
	if err != nil {
		return Task{}, err
	}
	attachmentsJSON, err := attachmentsToJSON(nil)
	if err != nil {
		return Task{}, err
	}

	row, err := queries.CreateTask(ctx, db.CreateTaskParams{
		ID: uuid.New(), CompanyID: actor.CompanyID, BoardID: input.BoardID,
		ColumnID: input.ColumnID, Order: count, Title: title,
		Description: nil, AuthorID: actor.UserID,
		AssigneeIds: []uuid.UUID{actor.UserID}, AssigneePositionID: uuid.NullUUID{},
		WatcherIds: []uuid.UUID{}, DueDate: pgtype.Timestamptz{},
		Priority: priority, LabelIds: []uuid.UUID{},
		Checklist: checklistJSON, Attachments: attachmentsJSON,
		Source: nil, LinkedArticleIds: []uuid.UUID{},
		Recurrence: nil, CompletedAt: pgtype.Timestamptz{},
	})
	if err != nil {
		return Task{}, internal("Не удалось создать задачу", err)
	}
	task, err := taskFromDB(row)
	if err != nil {
		return Task{}, err
	}
	if err = s.emitTaskAssigned(ctx, queries, actor, task); err != nil {
		return Task{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Task{}, internal("Не удалось сохранить задачу", err)
	}
	return task, nil
}

type UpdateTaskInput struct {
	ID                      uuid.UUID
	Title                   *string
	Description             json.RawMessage
	AssigneeIDs             []uuid.UUID
	AssigneeIDsSet          bool
	AssigneePositionID      *uuid.UUID
	ClearAssigneePositionID bool
	WatcherIDs              []uuid.UUID
	WatcherIDsSet           bool
	DueDate                 *time.Time
	ClearDueDate            bool
	Priority                *string
	LabelIDs                []uuid.UUID
	LabelIDsSet             bool
	Checklist               []ChecklistItem
	ChecklistSet            bool
	Attachments             []Attachment
	AttachmentsSet          bool
	LinkedArticleIDs        []uuid.UUID
	LinkedArticleIDsSet     bool
	Recurrence              *RecurrenceRule
	ClearRecurrence         bool
	CompletedAt             *time.Time
	ClearCompletedAt        bool
}

func (s *Service) UpdateTask(ctx context.Context, actor Actor, input UpdateTaskInput) (Task, error) {
	if !canUseTasks(actor) {
		return Task{}, forbidden("Недостаточно прав для изменения задачи")
	}
	if input.Title == nil && len(input.Description) == 0 && !input.AssigneeIDsSet &&
		input.AssigneePositionID == nil && !input.ClearAssigneePositionID && !input.WatcherIDsSet &&
		input.DueDate == nil && !input.ClearDueDate && input.Priority == nil && !input.LabelIDsSet &&
		!input.ChecklistSet && !input.AttachmentsSet && !input.LinkedArticleIDsSet &&
		input.Recurrence == nil && !input.ClearRecurrence && input.CompletedAt == nil && !input.ClearCompletedAt {
		return Task{}, validation("Укажите хотя бы одно поле для обновления")
	}
	if len(input.Description) > 0 && richtext.Validate(input.Description) != nil {
		return Task{}, validation("Некорректное описание задачи")
	}
	if input.AssigneeIDsSet {
		input.AssigneeIDs = normalizeUUIDs(input.AssigneeIDs)
	}
	if input.WatcherIDsSet {
		input.WatcherIDs = normalizeUUIDs(input.WatcherIDs)
	}
	if input.LabelIDsSet {
		input.LabelIDs = normalizeUUIDs(input.LabelIDs)
	}
	if input.LinkedArticleIDsSet {
		input.LinkedArticleIDs = normalizeUUIDs(input.LinkedArticleIDs)
	}
	if err := normalizeRecurrence(input.Recurrence); err != nil {
		return Task{}, err
	}
	if input.Priority != nil {
		if err := validatePriority(*input.Priority); err != nil {
			return Task{}, err
		}
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Task{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	currentRow, err := queries.GetTaskForUpdate(ctx, db.GetTaskForUpdateParams{
		CompanyID: actor.CompanyID, ID: input.ID,
	})
	if err != nil {
		if isNoRows(err) {
			return Task{}, notFound("Задача")
		}
		return Task{}, internal("Не удалось получить задачу", err)
	}
	current, err := taskFromDB(currentRow)
	if err != nil {
		return Task{}, err
	}
	if !canAccessTask(actor, current) {
		return Task{}, forbidden("Недостаточно прав для изменения задачи")
	}

	params := db.UpdateTaskParams{
		CompanyID: actor.CompanyID, ID: input.ID,
		ClearAssigneePositionID: pgtype.Bool{Bool: input.ClearAssigneePositionID, Valid: input.ClearAssigneePositionID},
		ClearDueDate:            pgtype.Bool{Bool: input.ClearDueDate, Valid: input.ClearDueDate},
		ClearRecurrence:         pgtype.Bool{Bool: input.ClearRecurrence, Valid: input.ClearRecurrence},
		ClearCompletedAt:        pgtype.Bool{Bool: input.ClearCompletedAt, Valid: input.ClearCompletedAt},
	}
	if input.Title != nil {
		title, titleErr := requiredText(*input.Title, "Укажите название задачи")
		if titleErr != nil {
			return Task{}, titleErr
		}
		params.Title = pgtypeText(title)
	}
	if len(input.Description) > 0 {
		params.Description = input.Description
	}
	if input.AssigneeIDsSet {
		params.AssigneeIds = input.AssigneeIDs
	}
	if input.AssigneePositionID != nil {
		params.AssigneePositionID = nullableUUID(input.AssigneePositionID)
	}
	if input.WatcherIDsSet {
		params.WatcherIds = input.WatcherIDs
	}
	if input.DueDate != nil {
		params.DueDate = nullableTime(input.DueDate)
	}
	if input.Priority != nil {
		params.Priority = pgtypeText(*input.Priority)
	}
	if input.LabelIDsSet {
		params.LabelIds = input.LabelIDs
	}
	if input.ChecklistSet {
		checklistJSON, checklistErr := checklistToJSON(input.Checklist)
		if checklistErr != nil {
			return Task{}, checklistErr
		}
		params.Checklist = checklistJSON
	}
	if input.AttachmentsSet {
		attachmentsJSON, attachmentsErr := attachmentsToJSON(input.Attachments)
		if attachmentsErr != nil {
			return Task{}, attachmentsErr
		}
		params.Attachments = attachmentsJSON
	}
	if input.LinkedArticleIDsSet {
		params.LinkedArticleIds = input.LinkedArticleIDs
	}
	if input.Recurrence != nil {
		recurrenceJSON, recurrenceErr := recurrenceToJSON(input.Recurrence)
		if recurrenceErr != nil {
			return Task{}, recurrenceErr
		}
		params.Recurrence = recurrenceJSON
	}
	if input.CompletedAt != nil {
		params.CompletedAt = nullableTime(input.CompletedAt)
	}

	updatedRow, err := queries.UpdateTask(ctx, params)
	if err != nil {
		return Task{}, internal("Не удалось обновить задачу", err)
	}
	task, err := taskFromDB(updatedRow)
	if err != nil {
		return Task{}, err
	}

	addedAssignees := addedUUIDs(current.AssigneeIDs, task.AssigneeIDs)
	var addedPosition *uuid.UUID
	if task.AssigneePositionID != nil &&
		(current.AssigneePositionID == nil || *current.AssigneePositionID != *task.AssigneePositionID) {
		addedPosition = task.AssigneePositionID
	}
	if len(addedAssignees) > 0 || addedPosition != nil {
		if err = s.emitTaskAssignedTo(ctx, queries, actor, task, addedAssignees, addedPosition); err != nil {
			return Task{}, err
		}
	}
	if len(input.Description) > 0 {
		if err = s.emitTaskMentions(ctx, queries, actor, task, task.Description); err != nil {
			return Task{}, err
		}
	}

	completedNow := current.CompletedAt == nil && task.CompletedAt != nil
	if completedNow && task.Recurrence != nil && s.recurrenceEnqueuer != nil {
		if err = s.recurrenceEnqueuer.EnqueueRecurrenceTx(ctx, tx, actor.CompanyID, task.ID); err != nil {
			return Task{}, internal("Не удалось поставить задачу повторения в очередь", err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return Task{}, internal("Не удалось сохранить задачу", err)
	}
	return task, nil
}

type MoveTaskInput struct {
	TaskID   uuid.UUID
	ColumnID uuid.UUID
	Order    int32
}

func (s *Service) MoveTask(ctx context.Context, actor Actor, input MoveTaskInput) (Task, error) {
	if !canUseTasks(actor) {
		return Task{}, forbidden("Недостаточно прав для перемещения задачи")
	}
	if input.Order < 0 {
		return Task{}, validation("Некорректный порядок задачи")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Task{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	initialRow, err := queries.GetTask(ctx, db.GetTaskParams{
		CompanyID: actor.CompanyID, ID: input.TaskID,
	})
	if err != nil {
		if isNoRows(err) {
			return Task{}, notFound("Задача")
		}
		return Task{}, internal("Не удалось получить задачу", err)
	}
	initial, err := taskFromDB(initialRow)
	if err != nil {
		return Task{}, err
	}
	if !canAccessTask(actor, initial) {
		return Task{}, forbidden("Недостаточно прав для перемещения задачи")
	}
	if err = queries.LockBoardOrder(ctx, initial.BoardID); err != nil {
		return Task{}, internal("Не удалось заблокировать порядок доски", err)
	}
	currentRow, err := queries.GetTaskForUpdate(ctx, db.GetTaskForUpdateParams{
		CompanyID: actor.CompanyID, ID: input.TaskID,
	})
	if err != nil {
		if isNoRows(err) {
			return Task{}, notFound("Задача")
		}
		return Task{}, internal("Не удалось получить задачу", err)
	}
	current, err := taskFromDB(currentRow)
	if err != nil {
		return Task{}, err
	}
	if !canAccessTask(actor, current) {
		return Task{}, forbidden("Недостаточно прав для перемещения задачи")
	}

	targetColumn, err := queries.GetColumn(ctx, db.GetColumnParams{
		CompanyID: actor.CompanyID, ID: input.ColumnID,
	})
	if err != nil {
		if isNoRows(err) {
			return Task{}, notFound("Колонка")
		}
		return Task{}, internal("Не удалось проверить колонку", err)
	}
	if targetColumn.BoardID != current.BoardID {
		return Task{}, validation("Колонка не принадлежит доске задачи")
	}

	columnIDs := []uuid.UUID{current.ColumnID}
	if input.ColumnID != current.ColumnID {
		columnIDs = append(columnIDs, input.ColumnID)
	}
	lockedRows, err := queries.ListTasksInColumnsForUpdate(ctx, db.ListTasksInColumnsForUpdateParams{
		CompanyID: actor.CompanyID, ColumnIds: columnIDs,
	})
	if err != nil {
		return Task{}, internal("Не удалось заблокировать задачи колонки", err)
	}

	movable := make([]domainboard.MovableTask, 0, len(lockedRows))
	for _, row := range lockedRows {
		movable = append(movable, domainboard.MovableTask{
			ID: row.ID, ColumnID: row.ColumnID, Order: row.Order,
		})
	}
	updates := domainboard.ReorderAfterMove(
		input.TaskID, current.ColumnID, input.ColumnID, input.Order, movable,
	)
	for _, update := range updates {
		if update.ID == input.TaskID {
			if err = queries.UpdateTaskPosition(ctx, db.UpdateTaskPositionParams{
				CompanyID: actor.CompanyID, ID: update.ID,
				ColumnID: update.ColumnID, Order: update.Order,
			}); err != nil {
				return Task{}, internal("Не удалось переместить задачу", err)
			}
			continue
		}
		if err = queries.UpdateTaskOrder(ctx, db.UpdateTaskOrderParams{
			CompanyID: actor.CompanyID, ID: update.ID, Order: update.Order,
		}); err != nil {
			return Task{}, internal("Не удалось обновить порядок задач", err)
		}
	}

	updatedRow, err := queries.GetTask(ctx, db.GetTaskParams{
		CompanyID: actor.CompanyID, ID: input.TaskID,
	})
	if err != nil {
		return Task{}, internal("Не удалось получить задачу", err)
	}
	task, err := taskFromDB(updatedRow)
	if err != nil {
		return Task{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Task{}, internal("Не удалось сохранить перемещение", err)
	}
	return task, nil
}

func (s *Service) ProcessRecurrence(ctx context.Context, companyID, taskID uuid.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	initialRow, err := queries.GetTask(ctx, db.GetTaskParams{CompanyID: companyID, ID: taskID})
	if err != nil {
		if isNoRows(err) {
			return notFound("Задача")
		}
		return internal("Не удалось получить задачу", err)
	}
	initial, err := taskFromDB(initialRow)
	if err != nil {
		return err
	}
	if err = queries.LockBoardOrder(ctx, initial.BoardID); err != nil {
		return internal("Не удалось заблокировать порядок доски", err)
	}
	row, err := queries.GetTaskForUpdate(ctx, db.GetTaskForUpdateParams{CompanyID: companyID, ID: taskID})
	if err != nil {
		if isNoRows(err) {
			return notFound("Задача")
		}
		return internal("Не удалось получить задачу", err)
	}
	task, err := taskFromDB(row)
	if err != nil {
		return err
	}
	if task.CompletedAt == nil || task.Recurrence == nil {
		return nil
	}
	generated, err := queries.IsRecurrenceGenerated(ctx, db.IsRecurrenceGeneratedParams{
		CompanyID: companyID, ID: taskID,
	})
	if err != nil {
		return internal("Не удалось проверить повтор задачи", err)
	}
	if generated {
		return nil
	}

	base := task.DueDate
	if base == nil {
		now := s.now().UTC()
		base = &now
	}
	nextDue, err := domainrecurrence.NextDueDate(domainrecurrence.Rule{
		Frequency: task.Recurrence.Frequency,
		Interval:  task.Recurrence.Interval,
		Weekdays:  task.Recurrence.Weekdays,
	}, *base)
	if err != nil {
		return validation(err.Error())
	}

	count, err := queries.CountTasksInColumn(ctx, task.ColumnID)
	if err != nil {
		return internal("Не удалось определить порядок задачи", err)
	}
	description := append(json.RawMessage(nil), task.Description...)
	checklistJSON, err := checklistToJSON(task.Checklist)
	if err != nil {
		return err
	}
	attachmentsJSON, err := attachmentsToJSON(task.Attachments)
	if err != nil {
		return err
	}
	sourceJSON, err := sourceToJSON(task.Source)
	if err != nil {
		return err
	}
	recurrenceJSON, err := recurrenceToJSON(task.Recurrence)
	if err != nil {
		return err
	}

	createdRow, err := queries.CreateTask(ctx, db.CreateTaskParams{
		ID: uuid.New(), CompanyID: task.CompanyID, BoardID: task.BoardID,
		ColumnID: task.ColumnID, Order: count, Title: task.Title,
		Description: description, AuthorID: task.AuthorID,
		AssigneeIds:        cloneUUIDs(task.AssigneeIDs),
		AssigneePositionID: nullableUUID(task.AssigneePositionID),
		WatcherIds:         cloneUUIDs(task.WatcherIDs),
		DueDate:            nullableTime(&nextDue), Priority: task.Priority,
		LabelIds:  cloneUUIDs(task.LabelIDs),
		Checklist: checklistJSON, Attachments: attachmentsJSON,
		Source: sourceJSON, LinkedArticleIds: cloneUUIDs(task.LinkedArticleIDs),
		Recurrence: recurrenceJSON, CompletedAt: pgtype.Timestamptz{},
	})
	if err != nil {
		return internal("Не удалось создать повторяющуюся задачу", err)
	}
	created, err := taskFromDB(createdRow)
	if err != nil {
		return err
	}
	actor := Actor{CompanyID: companyID, UserID: task.AuthorID, Role: "employee"}
	if err = s.emitTaskAssigned(ctx, queries, actor, created); err != nil {
		return err
	}
	if err = queries.MarkRecurrenceGenerated(ctx, db.MarkRecurrenceGeneratedParams{
		CompanyID: companyID, ID: taskID,
	}); err != nil {
		return internal("Не удалось отметить созданный повтор задачи", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось сохранить повторяющуюся задачу", err)
	}
	return nil
}

func (s *Service) ProcessDueSoon(ctx context.Context) error {
	companies, err := s.listCompanyIDs(ctx)
	if err != nil {
		return err
	}
	for _, companyID := range companies {
		if err = s.processDueSoonForCompany(ctx, companyID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) listCompanyIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT company_id FROM tasks`)
	if err != nil {
		return nil, internal("Не удалось получить компании", err)
	}
	defer rows.Close()
	companyIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var companyID uuid.UUID
		if err = rows.Scan(&companyID); err != nil {
			return nil, internal("Не удалось прочитать компании", err)
		}
		companyIDs = append(companyIDs, companyID)
	}
	if err = rows.Err(); err != nil {
		return nil, internal("Не удалось прочитать компании", err)
	}
	return companyIDs, nil
}

func (s *Service) processDueSoonForCompany(ctx context.Context, companyID uuid.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	rows, err := queries.ListTasksDueSoon(ctx, companyID)
	if err != nil {
		return internal("Не удалось получить задачи с приближающимся сроком", err)
	}
	for _, row := range rows {
		task, mapErr := taskFromDB(row)
		if mapErr != nil {
			return mapErr
		}
		if err = s.emitTaskDueSoon(ctx, queries, task); err != nil {
			return err
		}
		if err = queries.MarkDueSoonSent(ctx, db.MarkDueSoonSentParams{
			CompanyID: companyID, ID: task.ID,
		}); err != nil {
			return internal("Не удалось отметить уведомление о сроке", err)
		}
	}
	return tx.Commit(ctx)
}
