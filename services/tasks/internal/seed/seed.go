package seed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var fixtureNamespace = uuid.MustParse("7c29d63e-2954-5e23-9cba-7b950e8cc1a8")

type Summary struct {
	CompanyID string
	Boards    int
	Columns   int
	Tasks     int
	Labels    int
	Comments  int
}

type Fixtures struct {
	CompanyID    string
	Boards       []BoardFixture
	TaskColumns  []TaskColumnFixture
	Tasks        []TaskFixture
	Labels       []LabelFixture
	TaskComments []TaskCommentFixture
}

type BoardFixture struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	DepartmentID *string `json:"departmentId"`
	OwnerID      *string `json:"ownerId"`
	CreatedAt    string  `json:"createdAt"`
}

type TaskColumnFixture struct {
	ID      string  `json:"id"`
	BoardID string  `json:"boardId"`
	Name    string  `json:"name"`
	Order   int32   `json:"order"`
	Color   *string `json:"color"`
}

type TaskFixture struct {
	ID                 string          `json:"id"`
	BoardID            string          `json:"boardId"`
	ColumnID           string          `json:"columnId"`
	Order              int32           `json:"order"`
	Title              string          `json:"title"`
	Description        json.RawMessage `json:"description"`
	AuthorID           string          `json:"authorId"`
	AssigneeIDs        []string        `json:"assigneeIds"`
	AssigneePositionID *string         `json:"assigneePositionId"`
	WatcherIDs         []string        `json:"watcherIds"`
	DueDate            *string         `json:"dueDate"`
	Priority           string          `json:"priority"`
	LabelIDs           []string        `json:"labelIds"`
	Checklist          json.RawMessage `json:"checklist"`
	Attachments        json.RawMessage `json:"attachments"`
	Source             json.RawMessage `json:"source"`
	LinkedArticleIDs   []string        `json:"linkedArticleIds"`
	Recurrence         json.RawMessage `json:"recurrence"`
	CompletedAt        *string         `json:"completedAt"`
	CreatedAt          string          `json:"createdAt"`
	UpdatedAt          string          `json:"updatedAt"`
}

type LabelFixture struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type TaskCommentFixture struct {
	ID        string          `json:"id"`
	TaskID    string          `json:"taskId"`
	AuthorID  string          `json:"authorId"`
	Content   json.RawMessage `json:"content"`
	CreatedAt string          `json:"createdAt"`
}

func Run(ctx context.Context, pool *pgxpool.Pool, directory string) (Summary, error) {
	if pool == nil {
		return Summary{}, errors.New("соединение с PostgreSQL не задано")
	}
	fixtures, err := Load(directory)
	if err != nil {
		return Summary{}, err
	}
	dataset, err := Normalize(fixtures)
	if err != nil {
		return Summary{}, fmt.Errorf("проверить фикстуры: %w", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Summary{}, fmt.Errorf("начать seed-транзакцию: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := Apply(ctx, tx, dataset); err != nil {
		return Summary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Summary{}, fmt.Errorf("зафиксировать seed-транзакцию: %w", err)
	}
	return Summary{
		CompanyID: dataset.CompanyID.String(),
		Boards: len(dataset.Boards), Columns: len(dataset.Columns),
		Tasks: len(dataset.Tasks), Labels: len(dataset.Labels),
		Comments: len(dataset.Comments),
	}, nil
}

func Load(directory string) (Fixtures, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return Fixtures{}, errors.New("директория фикстур не задана")
	}
	var fixtures Fixtures
	if raw, err := os.ReadFile(filepath.Join(directory, "company.json")); err == nil {
		var payload struct{ ID string `json:"id"` }
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Fixtures{}, fmt.Errorf("company.json: %w", err)
		}
		fixtures.CompanyID = payload.ID
	}
	if fixtures.CompanyID == "" {
		if err := readWrapped(directory, []string{"fixtures.json", "seed.json", "manifest.json"}, func(key string, raw json.RawMessage) error {
			switch key {
			case "company":
				var payload struct{ ID string `json:"id"` }
				if err := json.Unmarshal(raw, &payload); err != nil {
					return err
				}
				fixtures.CompanyID = payload.ID
			case "boards":
				return json.Unmarshal(raw, &fixtures.Boards)
			case "taskColumns":
				return json.Unmarshal(raw, &fixtures.TaskColumns)
			case "tasks":
				return json.Unmarshal(raw, &fixtures.Tasks)
			case "labels":
				return json.Unmarshal(raw, &fixtures.Labels)
			case "taskComments":
				return json.Unmarshal(raw, &fixtures.TaskComments)
			}
			return nil
		}); err != nil {
			return Fixtures{}, err
		}
	}
	for _, name := range []struct {
		file string
		keys []string
		load func([]byte) error
	}{
		{"boards.json", []string{"boards"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.Boards) }},
		{"task-columns.json", []string{"taskColumns"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.TaskColumns) }},
		{"task_columns.json", []string{"taskColumns"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.TaskColumns) }},
		{"tasks.json", []string{"tasks"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.Tasks) }},
		{"labels.json", []string{"labels"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.Labels) }},
		{"task-comments.json", []string{"taskComments"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.TaskComments) }},
		{"task_comments.json", []string{"taskComments"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.TaskComments) }},
	} {
		_ = readEntityFile(filepath.Join(directory, name.file), name.keys, name.load)
	}
	if fixtures.CompanyID == "" {
		return Fixtures{}, errors.New("company.id не найден в фикстурах")
	}
	if len(fixtures.Boards) == 0 {
		return Fixtures{}, errors.New("boards не найдены в фикстурах")
	}
	return fixtures, nil
}

func readEntityFile(path string, keys []string, load func([]byte) error) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var direct any
	if err := json.Unmarshal(raw, &direct); err != nil {
		return err
	}
	switch value := direct.(type) {
	case []any:
		return load(raw)
	case map[string]any:
		for _, key := range keys {
			if nested, ok := value[key]; ok {
				encoded, encodeErr := json.Marshal(nested)
				if encodeErr != nil {
					return encodeErr
				}
				return load(encoded)
			}
		}
	}
	return load(raw)
}

func readWrapped(directory string, names []string, handle func(string, json.RawMessage) error) error {
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			continue
		}
		var manifest map[string]json.RawMessage
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return fmt.Errorf("разобрать %s: %w", name, err)
		}
		for key, value := range manifest {
			if err := handle(key, value); err != nil {
				return fmt.Errorf("%s.%s: %w", name, key, err)
			}
		}
		return nil
	}
	return errors.New("manifest fixtures not found")
}

type Dataset struct {
	CompanyID uuid.UUID
	Boards    []boardRow
	Columns   []columnRow
	Tasks     []taskRow
	Labels    []labelRow
	Comments  []commentRow
}

type boardRow struct {
	ID, CompanyID     uuid.UUID
	Name, Type        string
	DepartmentID      *uuid.UUID
	OwnerID           *uuid.UUID
	CreatedAt         time.Time
}

type columnRow struct {
	ID, BoardID uuid.UUID
	Name        string
	Order       int32
	Color       *string
}

type taskRow struct {
	ID, CompanyID, BoardID, ColumnID, AuthorID uuid.UUID
	Order                                      int32
	Title, Priority                            string
	Description, Checklist, Attachments, Source, Recurrence []byte
	AssigneeIDs, WatcherIDs, LabelIDs, LinkedArticleIDs     []uuid.UUID
	AssigneePositionID                                      *uuid.UUID
	DueDate, CompletedAt                                    *time.Time
	CreatedAt, UpdatedAt                                    time.Time
}

type labelRow struct {
	ID, CompanyID uuid.UUID
	Name, Color   string
}

type commentRow struct {
	ID, TaskID, AuthorID uuid.UUID
	Content              []byte
	CreatedAt            time.Time
}

func Normalize(fixtures Fixtures) (Dataset, error) {
	companyID, err := MapID(fixtures.CompanyID)
	if err != nil {
		return Dataset{}, fmt.Errorf("company.id: %w", err)
	}
	dataset := Dataset{CompanyID: companyID}

	for _, fixture := range fixtures.Boards {
		id, mapErr := MapID(fixture.ID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("board %s: %w", fixture.ID, mapErr)
		}
		var departmentID, ownerID *uuid.UUID
		if fixture.DepartmentID != nil {
			parsed, parseErr := MapID(*fixture.DepartmentID)
			if parseErr != nil {
				return Dataset{}, fmt.Errorf("board %s departmentId: %w", fixture.ID, parseErr)
			}
			departmentID = &parsed
		}
		if fixture.OwnerID != nil {
			parsed, parseErr := MapID(*fixture.OwnerID)
			if parseErr != nil {
				return Dataset{}, fmt.Errorf("board %s ownerId: %w", fixture.ID, parseErr)
			}
			ownerID = &parsed
		}
		createdAt, mapErr := parseTimestamp(fixture.CreatedAt)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("board %s createdAt: %w", fixture.ID, mapErr)
		}
		dataset.Boards = append(dataset.Boards, boardRow{
			ID: id, CompanyID: companyID, Name: fixture.Name, Type: fixture.Type,
			DepartmentID: departmentID, OwnerID: ownerID, CreatedAt: createdAt,
		})
	}
	for _, fixture := range fixtures.TaskColumns {
		id, mapErr := MapID(fixture.ID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("taskColumn %s: %w", fixture.ID, mapErr)
		}
		boardID, mapErr := MapID(fixture.BoardID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("taskColumn %s boardId: %w", fixture.ID, mapErr)
		}
		dataset.Columns = append(dataset.Columns, columnRow{
			ID: id, BoardID: boardID, Name: fixture.Name, Order: fixture.Order, Color: fixture.Color,
		})
	}
	for _, fixture := range fixtures.Labels {
		id, mapErr := MapID(fixture.ID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("label %s: %w", fixture.ID, mapErr)
		}
		dataset.Labels = append(dataset.Labels, labelRow{
			ID: id, CompanyID: companyID, Name: fixture.Name, Color: fixture.Color,
		})
	}
	for _, fixture := range fixtures.Tasks {
		row, mapErr := normalizeTask(fixture, companyID)
		if mapErr != nil {
			return Dataset{}, mapErr
		}
		dataset.Tasks = append(dataset.Tasks, row)
	}
	for _, fixture := range fixtures.TaskComments {
		id, mapErr := MapID(fixture.ID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("taskComment %s: %w", fixture.ID, mapErr)
		}
		taskID, mapErr := MapID(fixture.TaskID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("taskComment %s taskId: %w", fixture.ID, mapErr)
		}
		authorID, mapErr := MapID(fixture.AuthorID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("taskComment %s authorId: %w", fixture.ID, mapErr)
		}
		createdAt, mapErr := parseTimestamp(fixture.CreatedAt)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("taskComment %s createdAt: %w", fixture.ID, mapErr)
		}
		dataset.Comments = append(dataset.Comments, commentRow{
			ID: id, TaskID: taskID, AuthorID: authorID,
			Content: fixture.Content, CreatedAt: createdAt,
		})
	}
	return dataset, nil
}

func normalizeTask(fixture TaskFixture, companyID uuid.UUID) (taskRow, error) {
	id, err := MapID(fixture.ID)
	if err != nil {
		return taskRow{}, fmt.Errorf("task %s: %w", fixture.ID, err)
	}
	boardID, err := MapID(fixture.BoardID)
	if err != nil {
		return taskRow{}, fmt.Errorf("task %s boardId: %w", fixture.ID, err)
	}
	columnID, err := MapID(fixture.ColumnID)
	if err != nil {
		return taskRow{}, fmt.Errorf("task %s columnId: %w", fixture.ID, err)
	}
	authorID, err := MapID(fixture.AuthorID)
	if err != nil {
		return taskRow{}, fmt.Errorf("task %s authorId: %w", fixture.ID, err)
	}
	assigneeIDs, err := mapIDList(fixture.AssigneeIDs)
	if err != nil {
		return taskRow{}, fmt.Errorf("task %s assigneeIds: %w", fixture.ID, err)
	}
	watcherIDs, err := mapIDList(fixture.WatcherIDs)
	if err != nil {
		return taskRow{}, fmt.Errorf("task %s watcherIds: %w", fixture.ID, err)
	}
	labelIDs, err := mapIDList(fixture.LabelIDs)
	if err != nil {
		return taskRow{}, fmt.Errorf("task %s labelIds: %w", fixture.ID, err)
	}
	linkedArticleIDs, err := mapIDList(fixture.LinkedArticleIDs)
	if err != nil {
		return taskRow{}, fmt.Errorf("task %s linkedArticleIds: %w", fixture.ID, err)
	}
	var assigneePositionID *uuid.UUID
	if fixture.AssigneePositionID != nil {
		parsed, parseErr := MapID(*fixture.AssigneePositionID)
		if parseErr != nil {
			return taskRow{}, fmt.Errorf("task %s assigneePositionId: %w", fixture.ID, parseErr)
		}
		assigneePositionID = &parsed
	}
	var dueDate, completedAt *time.Time
	if fixture.DueDate != nil && strings.TrimSpace(*fixture.DueDate) != "" {
		parsed, parseErr := parseTimestamp(*fixture.DueDate)
		if parseErr != nil {
			return taskRow{}, fmt.Errorf("task %s dueDate: %w", fixture.ID, parseErr)
		}
		dueDate = &parsed
	}
	if fixture.CompletedAt != nil && strings.TrimSpace(*fixture.CompletedAt) != "" {
		parsed, parseErr := parseTimestamp(*fixture.CompletedAt)
		if parseErr != nil {
			return taskRow{}, fmt.Errorf("task %s completedAt: %w", fixture.ID, parseErr)
		}
		completedAt = &parsed
	}
	createdAt, err := parseTimestamp(fixture.CreatedAt)
	if err != nil {
		return taskRow{}, fmt.Errorf("task %s createdAt: %w", fixture.ID, err)
	}
	updatedAt, err := parseTimestamp(fixture.UpdatedAt)
	if err != nil {
		return taskRow{}, fmt.Errorf("task %s updatedAt: %w", fixture.ID, err)
	}
	priority := fixture.Priority
	if priority == "" {
		priority = "medium"
	}
	checklist := fixture.Checklist
	if len(checklist) == 0 {
		checklist = []byte("[]")
	}
	attachments := fixture.Attachments
	if len(attachments) == 0 {
		attachments = []byte("[]")
	}
	return taskRow{
		ID: id, CompanyID: companyID, BoardID: boardID, ColumnID: columnID,
		Order: fixture.Order, Title: fixture.Title, Priority: priority,
		Description: fixture.Description, AuthorID: authorID,
		AssigneeIDs: assigneeIDs, AssigneePositionID: assigneePositionID,
		WatcherIDs: watcherIDs, DueDate: dueDate, LabelIDs: labelIDs,
		Checklist: checklist, Attachments: attachments, Source: fixture.Source,
		LinkedArticleIDs: linkedArticleIDs, Recurrence: fixture.Recurrence,
		CompletedAt: completedAt, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

func mapIDList(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		parsed, err := MapID(value)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

func MapID(value string) (uuid.UUID, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return uuid.Nil, errors.New("пустой ID")
	}
	if parsed, err := uuid.Parse(normalized); err == nil {
		return parsed, nil
	}
	return uuid.NewSHA1(fixtureNamespace, []byte(normalized)), nil
}

func parseTimestamp(value string) (time.Time, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return time.Time{}, errors.New("пустая дата")
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z", "2006-01-02"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, normalized); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("неподдерживаемый формат даты %q", value)
}

func Apply(ctx context.Context, tx pgx.Tx, dataset Dataset) error {
	if _, err := tx.Exec(ctx, `DELETE FROM comments WHERE task_id IN (SELECT id FROM tasks WHERE company_id = $1)`, dataset.CompanyID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tasks WHERE company_id = $1`, dataset.CompanyID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM columns WHERE board_id IN (SELECT id FROM boards WHERE company_id = $1)`, dataset.CompanyID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM boards WHERE company_id = $1`, dataset.CompanyID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM labels WHERE company_id = $1`, dataset.CompanyID); err != nil {
		return err
	}
	for _, board := range dataset.Boards {
		if _, err := tx.Exec(ctx, `
			INSERT INTO boards (id, company_id, name, type, department_id, owner_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			board.ID, board.CompanyID, board.Name, board.Type,
			board.DepartmentID, board.OwnerID, board.CreatedAt,
		); err != nil {
			return fmt.Errorf("вставить board %s: %w", board.ID, err)
		}
	}
	for _, column := range dataset.Columns {
		if _, err := tx.Exec(ctx, `
			INSERT INTO columns (id, board_id, name, color, "order")
			VALUES ($1, $2, $3, $4, $5)`,
			column.ID, column.BoardID, column.Name, column.Color, column.Order,
		); err != nil {
			return fmt.Errorf("вставить column %s: %w", column.ID, err)
		}
	}
	for _, label := range dataset.Labels {
		if _, err := tx.Exec(ctx, `
			INSERT INTO labels (id, company_id, name, color)
			VALUES ($1, $2, $3, $4)`,
			label.ID, label.CompanyID, label.Name, label.Color,
		); err != nil {
			return fmt.Errorf("вставить label %s: %w", label.ID, err)
		}
	}
	for _, task := range dataset.Tasks {
		if _, err := tx.Exec(ctx, `
			INSERT INTO tasks (
				id, company_id, board_id, column_id, "order", title, description, author_id,
				assignee_ids, assignee_position_id, watcher_ids, due_date, priority, label_ids,
				checklist, attachments, source, linked_article_ids, recurrence, completed_at,
				created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`,
			task.ID, task.CompanyID, task.BoardID, task.ColumnID, task.Order, task.Title,
			nullBytes(task.Description), task.AuthorID, task.AssigneeIDs,
			task.AssigneePositionID, task.WatcherIDs, task.DueDate, task.Priority,
			task.LabelIDs, task.Checklist, task.Attachments, nullBytes(task.Source),
			task.LinkedArticleIDs, nullBytes(task.Recurrence), task.CompletedAt,
			task.CreatedAt, task.UpdatedAt,
		); err != nil {
			return fmt.Errorf("вставить task %s: %w", task.ID, err)
		}
	}
	for _, comment := range dataset.Comments {
		if _, err := tx.Exec(ctx, `
			INSERT INTO comments (id, task_id, author_id, content, created_at)
			VALUES ($1, $2, $3, $4, $5)`,
			comment.ID, comment.TaskID, comment.AuthorID, comment.Content, comment.CreatedAt,
		); err != nil {
			return fmt.Errorf("вставить comment %s: %w", comment.ID, err)
		}
	}
	return nil
}

func nullBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}