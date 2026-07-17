package application

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/storage/db"
)

func (s *Service) GetBoards(ctx context.Context, actor Actor) ([]Board, error) {
	if actor.Role != "owner" && actor.Role != "admin" && actor.Role != "employee" && actor.Role != "partner" {
		return nil, forbidden("Недостаточно прав для просмотра досок")
	}
	if actor.Role != "partner" {
		if err := s.ensureDefaultBoard(ctx, actor.CompanyID); err != nil {
			return nil, err
		}
	}
	rows, err := db.New(s.pool).ListBoards(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить доски", err)
	}
	visibleBoardIDs := map[uuid.UUID]struct{}(nil)
	if actor.Role == "partner" {
		tasks, taskErr := s.GetTasks(ctx, actor, nil)
		if taskErr != nil {
			return nil, taskErr
		}
		visibleBoardIDs = make(map[uuid.UUID]struct{}, len(tasks))
		for _, task := range tasks {
			visibleBoardIDs[task.BoardID] = struct{}{}
		}
	}
	boards := make([]Board, 0, len(rows))
	for _, row := range rows {
		board := boardFromDB(row)
		if visibleBoardIDs != nil {
			if _, ok := visibleBoardIDs[board.ID]; !ok {
				continue
			}
		}
		boards = append(boards, board)
	}
	return boards, nil
}

func (s *Service) ensureDefaultBoard(ctx context.Context, companyID uuid.UUID) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return internal("Не удалось начать создание доски", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = queries.LockCompanyBoardBootstrap(ctx, companyID); err != nil {
		return internal("Не удалось заблокировать создание доски", err)
	}
	boards, err := queries.ListBoards(ctx, companyID)
	if err != nil {
		return internal("Не удалось проверить доски", err)
	}
	if len(boards) == 0 {
		boardID := uuid.New()
		if _, err = queries.CreateBoard(ctx, db.CreateBoardParams{
			ID: boardID, CompanyID: companyID, Name: "Задачи компании", Type: "project",
		}); err != nil {
			return internal("Не удалось создать стандартную доску", err)
		}
		columns := []struct {
			name  string
			color string
		}{
			{name: "Новые", color: "slate"},
			{name: "В работе", color: "sky"},
			{name: "Готово", color: "green"},
		}
		for index, column := range columns {
			if _, err = queries.CreateColumn(ctx, db.CreateColumnParams{
				ID: uuid.New(), BoardID: boardID, Name: column.name,
				Color: pgtype.Text{String: column.color, Valid: true}, Order: int32(index),
			}); err != nil {
				return internal("Не удалось создать стандартные колонки", err)
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return internal("Не удалось сохранить стандартную доску", err)
	}
	return nil
}

func (s *Service) GetColumns(ctx context.Context, actor Actor, boardID uuid.UUID) ([]TaskColumn, error) {
	if actor.Role == "partner" {
		tasks, err := s.GetTasks(ctx, actor, &boardID)
		if err != nil {
			return nil, err
		}
		if len(tasks) == 0 {
			return nil, forbidden("Недостаточно прав для просмотра доски")
		}
	} else if actor.Role != "owner" && actor.Role != "admin" && actor.Role != "employee" {
		return nil, forbidden("Недостаточно прав для просмотра доски")
	}
	if _, err := db.New(s.pool).GetBoard(ctx, db.GetBoardParams{
		CompanyID: actor.CompanyID, ID: boardID,
	}); err != nil {
		if isNoRows(err) {
			return nil, notFound("Доска")
		}
		return nil, internal("Не удалось проверить доску", err)
	}
	rows, err := db.New(s.pool).ListColumnsByBoard(ctx, boardID)
	if err != nil {
		return nil, internal("Не удалось получить колонки", err)
	}
	columns := make([]TaskColumn, 0, len(rows))
	for _, row := range rows {
		columns = append(columns, columnFromDB(row))
	}
	return columns, nil
}

type CreateColumnInput struct {
	BoardID uuid.UUID
	Name    string
	Color   *string
}

func (s *Service) CreateColumn(ctx context.Context, actor Actor, input CreateColumnInput) (TaskColumn, error) {
	if !canManageBoardStructure(actor) {
		return TaskColumn{}, forbidden("Недостаточно прав для изменения структуры доски")
	}
	name, err := requiredText(input.Name, "Укажите название колонки")
	if err != nil {
		return TaskColumn{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TaskColumn{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)
	if err = queries.LockBoardOrder(ctx, input.BoardID); err != nil {
		return TaskColumn{}, internal("Не удалось заблокировать порядок доски", err)
	}
	if _, err = queries.GetBoard(ctx, db.GetBoardParams{
		CompanyID: actor.CompanyID, ID: input.BoardID,
	}); err != nil {
		if isNoRows(err) {
			return TaskColumn{}, notFound("Доска")
		}
		return TaskColumn{}, internal("Не удалось проверить доску", err)
	}
	count, err := queries.CountColumnsByBoard(ctx, input.BoardID)
	if err != nil {
		return TaskColumn{}, internal("Не удалось определить порядок колонки", err)
	}
	row, err := queries.CreateColumn(ctx, db.CreateColumnParams{
		ID: uuid.New(), BoardID: input.BoardID, Name: name,
		Color: nullableText(input.Color), Order: count,
	})
	if err != nil {
		return TaskColumn{}, internal("Не удалось создать колонку", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return TaskColumn{}, internal("Не удалось сохранить колонку", err)
	}
	return columnFromDB(row), nil
}

type UpdateColumnInput struct {
	ID    uuid.UUID
	Name  *string
	Color *string
}

func (s *Service) UpdateColumn(ctx context.Context, actor Actor, input UpdateColumnInput) (TaskColumn, error) {
	if !canManageBoardStructure(actor) {
		return TaskColumn{}, forbidden("Недостаточно прав для изменения структуры доски")
	}
	if input.Name == nil && input.Color == nil {
		return TaskColumn{}, validation("Укажите хотя бы одно поле для обновления")
	}
	if _, err := db.New(s.pool).GetColumn(ctx, db.GetColumnParams{
		CompanyID: actor.CompanyID, ID: input.ID,
	}); err != nil {
		if isNoRows(err) {
			return TaskColumn{}, notFound("Колонка")
		}
		return TaskColumn{}, internal("Не удалось проверить колонку", err)
	}
	params := db.UpdateColumnParams{ID: input.ID}
	if input.Name != nil {
		name, err := requiredText(*input.Name, "Укажите название колонки")
		if err != nil {
			return TaskColumn{}, err
		}
		params.Name = pgtypeText(name)
	}
	if input.Color != nil {
		params.Color = nullableText(input.Color)
	}
	row, err := db.New(s.pool).UpdateColumn(ctx, params)
	if err != nil {
		return TaskColumn{}, internal("Не удалось обновить колонку", err)
	}
	return columnFromDB(row), nil
}

func pgtypeText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
}
