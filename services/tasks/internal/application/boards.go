package application

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/storage/db"
)

func (s *Service) GetBoards(ctx context.Context, actor Actor) ([]Board, error) {
	rows, err := db.New(s.pool).ListBoards(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить доски", err)
	}
	boards := make([]Board, 0, len(rows))
	for _, row := range rows {
		boards = append(boards, boardFromDB(row))
	}
	return boards, nil
}

func (s *Service) GetColumns(ctx context.Context, actor Actor, boardID uuid.UUID) ([]TaskColumn, error) {
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
	name, err := requiredText(input.Name, "Укажите название колонки")
	if err != nil {
		return TaskColumn{}, err
	}
	if _, err = db.New(s.pool).GetBoard(ctx, db.GetBoardParams{
		CompanyID: actor.CompanyID, ID: input.BoardID,
	}); err != nil {
		if isNoRows(err) {
			return TaskColumn{}, notFound("Доска")
		}
		return TaskColumn{}, internal("Не удалось проверить доску", err)
	}
	count, err := db.New(s.pool).CountColumnsByBoard(ctx, input.BoardID)
	if err != nil {
		return TaskColumn{}, internal("Не удалось определить порядок колонки", err)
	}
	row, err := db.New(s.pool).CreateColumn(ctx, db.CreateColumnParams{
		ID: uuid.New(), BoardID: input.BoardID, Name: name,
		Color: nullableText(input.Color), Order: count,
	})
	if err != nil {
		return TaskColumn{}, internal("Не удалось создать колонку", err)
	}
	return columnFromDB(row), nil
}

type UpdateColumnInput struct {
	ID    uuid.UUID
	Name  *string
	Color *string
}

func (s *Service) UpdateColumn(ctx context.Context, actor Actor, input UpdateColumnInput) (TaskColumn, error) {
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