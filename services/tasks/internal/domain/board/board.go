package board

import (
	"sort"
	"time"

	"github.com/google/uuid"
)

const (
	TodayColumnID      = "virtual-today"
	TodayColumnName    = "На сегодня"
	StuckThresholdDays = 3
	DoneWindowDays     = 7
)

type Column struct {
	ID      uuid.UUID
	BoardID uuid.UUID
	Name    string
	Order   int32
	Color   *string
}

type Task struct {
	ID          uuid.UUID
	BoardID     uuid.UUID
	ColumnID    uuid.UUID
	Order       int32
	Title       string
	AuthorID    uuid.UUID
	DueDate     *time.Time
	Priority    string
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ColumnView struct {
	Column  Column
	Tasks   []Task
	Virtual bool
}

type Stats struct {
	InWork         int
	Today          int
	Overdue        int
	DoneLast7Days  int
}

type MovableTask struct {
	ID       uuid.UUID
	ColumnID uuid.UUID
	Order    int32
}

type OrderUpdate struct {
	ID       uuid.UUID
	ColumnID uuid.UUID
	Order    int32
}

func IsDueTodayOrOverdue(dueDate *time.Time, now time.Time) bool {
	if dueDate == nil {
		return false
	}
	endOfToday := time.Date(
		now.Year(), now.Month(), now.Day(),
		23, 59, 59, 999_999_999, now.Location(),
	)
	return !dueDate.After(endOfToday)
}

func IsOverdue(task Task, now time.Time) bool {
	if task.DueDate == nil || task.CompletedAt != nil {
		return false
	}
	return task.DueDate.Before(now)
}

func WorkColumnIDs(columns []Column) map[uuid.UUID]struct{} {
	ordered := append([]Column(nil), columns...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Order < ordered[j].Order })
	if len(ordered) <= 2 {
		return map[uuid.UUID]struct{}{}
	}
	result := make(map[uuid.UUID]struct{}, len(ordered)-2)
	for _, column := range ordered[1 : len(ordered)-1] {
		result[column.ID] = struct{}{}
	}
	return result
}

func IsStuck(task Task, workIDs map[uuid.UUID]struct{}, now time.Time, thresholdDays int) bool {
	if thresholdDays <= 0 {
		thresholdDays = StuckThresholdDays
	}
	if task.CompletedAt != nil {
		return false
	}
	if _, ok := workIDs[task.ColumnID]; !ok {
		return false
	}
	threshold := now.Add(-time.Duration(thresholdDays) * 24 * time.Hour)
	return !task.UpdatedAt.After(threshold)
}

func BoardStats(tasks []Task, columns []Column, now time.Time) Stats {
	workIDs := WorkColumnIDs(columns)
	doneWindowStartedAt := now.Add(-DoneWindowDays * 24 * time.Hour)
	stats := Stats{}
	for _, task := range tasks {
		if task.CompletedAt == nil {
			if _, ok := workIDs[task.ColumnID]; ok {
				stats.InWork++
			}
			if IsDueTodayOrOverdue(task.DueDate, now) {
				stats.Today++
			}
		}
		if IsOverdue(task, now) {
			stats.Overdue++
		}
		if task.CompletedAt != nil && !task.CompletedAt.Before(doneWindowStartedAt) && !task.CompletedAt.After(now) {
			stats.DoneLast7Days++
		}
	}
	return stats
}

func BuildBoardColumns(columns []Column, tasks []Task, now time.Time) []ColumnView {
	if len(columns) == 0 {
		return nil
	}
	ordered := append([]Column(nil), columns...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Order < ordered[j].Order })
	backlog := ordered[0]

	todayTasks := make([]Task, 0)
	for _, task := range tasks {
		if task.ColumnID == backlog.ID && task.CompletedAt == nil && IsDueTodayOrOverdue(task.DueDate, now) {
			todayTasks = append(todayTasks, task)
		}
	}
	sort.Slice(todayTasks, func(i, j int) bool {
		left := ""
		if todayTasks[i].DueDate != nil {
			left = todayTasks[i].DueDate.Format(time.RFC3339Nano)
		}
		right := ""
		if todayTasks[j].DueDate != nil {
			right = todayTasks[j].DueDate.Format(time.RFC3339Nano)
		}
		return left < right
	})
	todayTaskIDs := make(map[uuid.UUID]struct{}, len(todayTasks))
	for _, task := range todayTasks {
		todayTaskIDs[task.ID] = struct{}{}
	}

	tasksByColumn := make(map[uuid.UUID][]Task, len(ordered))
	for _, column := range ordered {
		tasksByColumn[column.ID] = nil
	}
	for _, task := range tasks {
		if _, skip := todayTaskIDs[task.ID]; skip {
			continue
		}
		tasksByColumn[task.ColumnID] = append(tasksByColumn[task.ColumnID], task)
	}
	for columnID := range tasksByColumn {
		sort.Slice(tasksByColumn[columnID], func(i, j int) bool {
			return tasksByColumn[columnID][i].Order < tasksByColumn[columnID][j].Order
		})
	}

	virtualColor := "amber"
	virtualOrder := backlog.Order + 1
	virtualColumn := Column{
		ID:      uuid.Nil,
		BoardID: backlog.BoardID,
		Name:    TodayColumnName,
		Order:   virtualOrder,
		Color:   &virtualColor,
	}

	views := make([]ColumnView, 0, len(ordered)+1)
	for index, column := range ordered {
		views = append(views, ColumnView{
			Column: column, Tasks: tasksByColumn[column.ID], Virtual: false,
		})
		if index == 0 {
			views = append(views, ColumnView{
				Column: virtualColumn, Tasks: todayTasks, Virtual: true,
			})
		}
	}
	return views
}

// ReorderAfterMove ports the frontend moveTask sibling renumbering algorithm.
func ReorderAfterMove(
	movedID uuid.UUID,
	oldColumnID, newColumnID uuid.UUID,
	newOrder int32,
	tasks []MovableTask,
) []OrderUpdate {
	updates := []OrderUpdate{{ID: movedID, ColumnID: newColumnID, Order: newOrder}}

	columnIDs := []uuid.UUID{oldColumnID}
	if newColumnID != oldColumnID {
		columnIDs = append(columnIDs, newColumnID)
	}
	seen := make(map[uuid.UUID]struct{}, len(columnIDs))
	for _, columnID := range columnIDs {
		if _, ok := seen[columnID]; ok {
			continue
		}
		seen[columnID] = struct{}{}

		siblings := make([]MovableTask, 0)
		for _, task := range tasks {
			if task.ID == movedID || task.ColumnID != columnID {
				continue
			}
			siblings = append(siblings, task)
		}
		sort.Slice(siblings, func(i, j int) bool { return siblings[i].Order < siblings[j].Order })

		for index, sibling := range siblings {
			order := int32(index)
			if columnID == newColumnID && int32(index) >= newOrder {
				order = int32(index) + 1
			}
			if sibling.Order != order {
				updates = append(updates, OrderUpdate{
					ID: sibling.ID, ColumnID: columnID, Order: order,
				})
			}
		}
	}
	return updates
}