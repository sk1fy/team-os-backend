package application

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sk1fy/team-os-backend/pkg/richtext"
	"github.com/sk1fy/team-os-backend/services/tasks/internal/storage/db"
)

func (s *Service) GetComments(ctx context.Context, actor Actor, taskID uuid.UUID) ([]Comment, error) {
	row, err := db.New(s.pool).GetTask(ctx, db.GetTaskParams{
		CompanyID: actor.CompanyID, ID: taskID,
	})
	if err != nil {
		if isNoRows(err) {
			return nil, notFound("Задача")
		}
		return nil, internal("Не удалось проверить задачу", err)
	}
	task, err := taskFromDB(row)
	if err != nil {
		return nil, err
	}
	if !canAccessTask(actor, task) {
		return nil, forbidden("Недостаточно прав для просмотра комментариев")
	}
	rows, err := db.New(s.pool).ListCommentsByTask(ctx, taskID)
	if err != nil {
		return nil, internal("Не удалось получить комментарии", err)
	}
	comments := make([]Comment, 0, len(rows))
	for _, row := range rows {
		comments = append(comments, commentFromDB(row))
	}
	return comments, nil
}

type AddCommentInput struct {
	TaskID  uuid.UUID
	Content json.RawMessage
}

func (s *Service) AddComment(ctx context.Context, actor Actor, input AddCommentInput) (Comment, error) {
	if richtext.Validate(input.Content) != nil {
		return Comment{}, validation("Некорректное содержимое комментария")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Comment{}, internal("Не удалось начать транзакцию", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	taskRow, err := queries.GetTask(ctx, db.GetTaskParams{
		CompanyID: actor.CompanyID, ID: input.TaskID,
	})
	if err != nil {
		if isNoRows(err) {
			return Comment{}, notFound("Задача")
		}
		return Comment{}, internal("Не удалось проверить задачу", err)
	}
	task, err := taskFromDB(taskRow)
	if err != nil {
		return Comment{}, err
	}
	if !canAccessTask(actor, task) {
		return Comment{}, forbidden("Недостаточно прав для добавления комментария")
	}

	row, err := queries.CreateComment(ctx, db.CreateCommentParams{
		ID: uuid.New(), TaskID: input.TaskID,
		AuthorID: actor.UserID, Content: input.Content,
	})
	if err != nil {
		return Comment{}, internal("Не удалось добавить комментарий", err)
	}
	comment := commentFromDB(row)

	if err = s.emitCommentAdded(ctx, queries, actor, task, comment); err != nil {
		return Comment{}, err
	}
	if err = s.emitMentions(ctx, queries, actor, task, comment, input.Content); err != nil {
		return Comment{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Comment{}, internal("Не удалось сохранить комментарий", err)
	}
	return comment, nil
}
