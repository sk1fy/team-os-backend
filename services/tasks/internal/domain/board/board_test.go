package board

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	boardID  = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	column1  = uuid.MustParse("00000000-0000-0000-0000-000000000011")
	column2  = uuid.MustParse("00000000-0000-0000-0000-000000000012")
	column3  = uuid.MustParse("00000000-0000-0000-0000-000000000013")
	authorID = uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
)

var now = time.Date(2026, 7, 6, 15, 30, 0, 0, time.UTC)

var columns = []Column{
	{ID: column1, BoardID: boardID, Name: "Бэклог", Order: 0},
	{ID: column2, BoardID: boardID, Name: "В работе", Order: 1},
	{ID: column3, BoardID: boardID, Name: "Готово", Order: 2},
}

func makeTask(id uuid.UUID, columnID uuid.UUID, input func(*Task)) Task {
	task := Task{
		ID: id, BoardID: boardID, ColumnID: columnID, Order: 0,
		Title: id.String(), AuthorID: authorID, Priority: "medium",
		CreatedAt: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
	}
	if input != nil {
		input(&task)
	}
	return task
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		parsed, err = time.Parse("2006-01-02T15:04:05", value)
		if err != nil {
			panic(err)
		}
	}
	return parsed
}

func TestIsDueTodayOrOverdue(t *testing.T) {
	t.Run("считает дедлайн до локального конца дня сегодняшним", func(t *testing.T) {
		start := parseTime("2026-07-06T00:00:00")
		end := parseTime("2026-07-06T23:59:59")
		if !IsDueTodayOrOverdue(&start, now) || !IsDueTodayOrOverdue(&end, now) {
			t.Fatal("expected today deadlines to match")
		}
	})
	t.Run("считает прошлые дедлайны просроченными, а будущие не включает", func(t *testing.T) {
		past := parseTime("2026-07-05T23:59:59")
		future := parseTime("2026-07-07T00:00:00")
		if !IsDueTodayOrOverdue(&past, now) {
			t.Fatal("expected past deadline")
		}
		if IsDueTodayOrOverdue(&future, now) {
			t.Fatal("expected future deadline to be excluded")
		}
	})
	t.Run("не срабатывает без даты", func(t *testing.T) {
		if IsDueTodayOrOverdue(nil, now) {
			t.Fatal("expected false without due date")
		}
	})
}

func TestIsOverdue(t *testing.T) {
	t.Run("считает незавершённую задачу с дедлайном вчера просроченной", func(t *testing.T) {
		due := parseTime("2026-07-05T12:00:00")
		if !IsOverdue(makeTask(uuid.New(), column1, func(task *Task) { task.DueDate = &due }), now) {
			t.Fatal("expected overdue task")
		}
	})
	t.Run("не считает будущий дедлайн просроченным", func(t *testing.T) {
		due := parseTime("2026-07-07T12:00:00")
		if IsOverdue(makeTask(uuid.New(), column1, func(task *Task) { task.DueDate = &due }), now) {
			t.Fatal("expected future task not overdue")
		}
	})
	t.Run("не считает завершённую задачу просроченной", func(t *testing.T) {
		due := parseTime("2026-07-05T12:00:00")
		completed := parseTime("2026-07-06T10:00:00")
		task := makeTask(uuid.New(), column1, func(task *Task) {
			task.DueDate = &due
			task.CompletedAt = &completed
		})
		if IsOverdue(task, now) {
			t.Fatal("expected completed task not overdue")
		}
	})
	t.Run("не срабатывает без срока", func(t *testing.T) {
		if IsOverdue(makeTask(uuid.New(), column1, nil), now) {
			t.Fatal("expected false without due date")
		}
	})
}

func TestWorkColumnIDs(t *testing.T) {
	t.Run("возвращает среднюю колонку из трёх", func(t *testing.T) {
		ids := WorkColumnIDs(columns)
		if len(ids) != 1 {
			t.Fatalf("len = %d, want 1", len(ids))
		}
		if _, ok := ids[column2]; !ok {
			t.Fatalf("expected %s", column2)
		}
	})
	t.Run("возвращает пустой набор для двух колонок", func(t *testing.T) {
		if len(WorkColumnIDs(columns[:2])) != 0 {
			t.Fatal("expected empty set for two columns")
		}
	})
	t.Run("учитывает сортировку по order", func(t *testing.T) {
		done := uuid.New()
		work := uuid.New()
		backlog := uuid.New()
		ids := WorkColumnIDs([]Column{
			{ID: done, BoardID: boardID, Name: "Готово", Order: 30},
			{ID: work, BoardID: boardID, Name: "В работе", Order: 20},
			{ID: backlog, BoardID: boardID, Name: "Бэклог", Order: 10},
		})
		if len(ids) != 1 {
			t.Fatalf("len = %d, want 1", len(ids))
		}
		if _, ok := ids[work]; !ok {
			t.Fatalf("expected %s", work)
		}
	})
}

func TestIsStuck(t *testing.T) {
	workIDs := WorkColumnIDs(columns)
	t.Run("считает задачу в рабочей колонке без обновлений 4 дня застрявшей", func(t *testing.T) {
		updated := parseTime("2026-07-02T15:30:00")
		task := makeTask(uuid.New(), column2, func(task *Task) { task.UpdatedAt = updated })
		if !IsStuck(task, workIDs, now, StuckThresholdDays) {
			t.Fatal("expected stuck task")
		}
	})
	t.Run("не считает задачу с обновлением день назад застрявшей", func(t *testing.T) {
		updated := parseTime("2026-07-05T15:30:00")
		task := makeTask(uuid.New(), column2, func(task *Task) { task.UpdatedAt = updated })
		if IsStuck(task, workIDs, now, StuckThresholdDays) {
			t.Fatal("expected fresh task not stuck")
		}
	})
	t.Run("включает границу ровно 3 дня", func(t *testing.T) {
		updated := parseTime("2026-07-03T15:30:00")
		task := makeTask(uuid.New(), column2, func(task *Task) { task.UpdatedAt = updated })
		if !IsStuck(task, workIDs, now, StuckThresholdDays) {
			t.Fatal("expected edge stuck task")
		}
	})
	t.Run("не срабатывает в бэклоге", func(t *testing.T) {
		updated := parseTime("2026-07-02T15:30:00")
		task := makeTask(uuid.New(), column1, func(task *Task) { task.UpdatedAt = updated })
		if IsStuck(task, workIDs, now, StuckThresholdDays) {
			t.Fatal("expected backlog task not stuck")
		}
	})
	t.Run("не срабатывает для завершённой задачи", func(t *testing.T) {
		updated := parseTime("2026-07-02T15:30:00")
		completed := parseTime("2026-07-05T12:00:00")
		task := makeTask(uuid.New(), column2, func(task *Task) {
			task.UpdatedAt = updated
			task.CompletedAt = &completed
		})
		if IsStuck(task, workIDs, now, StuckThresholdDays) {
			t.Fatal("expected completed task not stuck")
		}
	})
}

func TestBoardStats(t *testing.T) {
	t.Run("считает сводку по задачам доски", func(t *testing.T) {
		dueWork := parseTime("2026-07-08T12:00:00")
		dueToday := parseTime("2026-07-06T20:00:00")
		dueOverdue := parseTime("2026-07-05T12:00:00")
		done6 := parseTime("2026-06-30T15:30:00")
		done8 := parseTime("2026-06-28T15:30:00")
		doneWithOverdue := parseTime("2026-07-04T12:00:00")
		dueForDone := parseTime("2026-07-01T12:00:00")

		tasks := []Task{
			makeTask(uuid.New(), column2, func(task *Task) { task.DueDate = &dueWork }),
			makeTask(uuid.New(), column1, func(task *Task) { task.DueDate = &dueToday }),
			makeTask(uuid.New(), column2, func(task *Task) { task.DueDate = &dueOverdue }),
			makeTask(uuid.New(), column3, func(task *Task) { task.CompletedAt = &done6 }),
			makeTask(uuid.New(), column3, func(task *Task) { task.CompletedAt = &done8 }),
			makeTask(uuid.New(), column3, func(task *Task) {
				task.DueDate = &dueForDone
				task.CompletedAt = &doneWithOverdue
			}),
		}
		stats := BoardStats(tasks, columns, now)
		if stats != (Stats{InWork: 2, Today: 2, Overdue: 1, DoneLast7Days: 2}) {
			t.Fatalf("stats = %+v", stats)
		}
	})
}

func TestBuildBoardColumns(t *testing.T) {
	t.Run("вставляет виртуальную колонку после бэклога", func(t *testing.T) {
		views := BuildBoardColumns(columns, nil, now)
		if len(views) != 4 {
			t.Fatalf("len = %d, want 4", len(views))
		}
		if views[0].Column.ID != column1 || views[1].Virtual != true || views[2].Column.ID != column2 || views[3].Column.ID != column3 {
			t.Fatalf("unexpected column order: %+v", views)
		}
	})
	t.Run("переносит в На сегодня только незавершённые задачи бэклога с дедлайном сегодня или раньше", func(t *testing.T) {
		overdueID := uuid.New()
		todayID := uuid.New()
		doneID := uuid.New()
		workID := uuid.New()
		withoutDateID := uuid.New()
		futureID := uuid.New()

		overdueDue := parseTime("2026-07-03T12:00:00")
		todayDue := parseTime("2026-07-06T20:00:00")
		doneDue := parseTime("2026-07-06T12:00:00")
		workDue := parseTime("2026-07-06T12:00:00")
		futureDue := parseTime("2026-07-08T12:00:00")
		completed := parseTime("2026-07-06T13:00:00")

		tasks := []Task{
			makeTask(overdueID, column1, func(task *Task) { task.DueDate = &overdueDue }),
			makeTask(todayID, column1, func(task *Task) { task.DueDate = &todayDue }),
			makeTask(doneID, column1, func(task *Task) { task.DueDate = &doneDue; task.CompletedAt = &completed }),
			makeTask(workID, column2, func(task *Task) { task.DueDate = &workDue }),
			makeTask(withoutDateID, column1, nil),
			makeTask(futureID, column1, func(task *Task) { task.DueDate = &futureDue }),
		}
		views := BuildBoardColumns(columns, tasks, now)
		var todayTasks, backlogTasks, workTasks []uuid.UUID
		for _, view := range views {
			switch {
			case view.Virtual:
				for _, task := range view.Tasks {
					todayTasks = append(todayTasks, task.ID)
				}
			case view.Column.ID == column1:
				for _, task := range view.Tasks {
					backlogTasks = append(backlogTasks, task.ID)
				}
			case view.Column.ID == column2:
				for _, task := range view.Tasks {
					workTasks = append(workTasks, task.ID)
				}
			}
		}
		if len(todayTasks) != 2 || todayTasks[0] != overdueID || todayTasks[1] != todayID {
			t.Fatalf("today tasks = %v", todayTasks)
		}
		if len(backlogTasks) != 3 || backlogTasks[0] != doneID || backlogTasks[1] != withoutDateID || backlogTasks[2] != futureID {
			t.Fatalf("backlog tasks = %v", backlogTasks)
		}
		if len(workTasks) != 1 || workTasks[0] != workID {
			t.Fatalf("work tasks = %v", workTasks)
		}
	})
	t.Run("сортирует виртуальную колонку по дедлайну", func(t *testing.T) {
		firstID := uuid.New()
		secondID := uuid.New()
		firstDue := parseTime("2026-07-04T12:00:00")
		secondDue := parseTime("2026-07-06T12:00:00")
		tasks := []Task{
			makeTask(secondID, column1, func(task *Task) { task.DueDate = &secondDue }),
			makeTask(firstID, column1, func(task *Task) { task.DueDate = &firstDue }),
		}
		views := BuildBoardColumns(columns, tasks, now)
		for _, view := range views {
			if !view.Virtual {
				continue
			}
			if len(view.Tasks) != 2 || view.Tasks[0].ID != firstID || view.Tasks[1].ID != secondID {
				t.Fatalf("sorted today tasks = %+v", view.Tasks)
			}
		}
	})
	t.Run("возвращает пустой список для пустых колонок", func(t *testing.T) {
		if BuildBoardColumns(nil, []Task{makeTask(uuid.New(), column1, nil)}, now) != nil {
			t.Fatal("expected nil for empty columns")
		}
	})
}

func TestReorderAfterMove(t *testing.T) {
	movedID := uuid.New()
	otherA := uuid.New()
	otherB := uuid.New()
	oldColumn := column1
	newColumn := column2

	tasks := []MovableTask{
		{ID: movedID, ColumnID: oldColumn, Order: 0},
		{ID: otherA, ColumnID: oldColumn, Order: 1},
		{ID: otherB, ColumnID: oldColumn, Order: 2},
		{ID: uuid.New(), ColumnID: newColumn, Order: 0},
	}

	updates := ReorderAfterMove(movedID, oldColumn, newColumn, 1, tasks)
	orders := map[uuid.UUID]int32{}
	for _, update := range updates {
		orders[update.ID] = update.Order
	}
	if orders[movedID] != 1 {
		t.Fatalf("moved order = %d, want 1", orders[movedID])
	}
	if orders[otherA] != 0 {
		t.Fatalf("otherA order = %d, want 0", orders[otherA])
	}
	if orders[otherB] != 1 {
		t.Fatalf("otherB order = %d, want 1", orders[otherB])
	}
}