package seed

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMapID(t *testing.T) {
	knownID := uuid.New()

	tests := []struct {
		name    string
		value   string
		wantErr bool
		want    uuid.UUID
	}{
		{name: "uuid сохраняется", value: knownID.String(), want: knownID},
		{name: "строковый ID детерминированно отображается в UUID", value: "course-demo"},
		{name: "пустой ID отклоняется", value: "  ", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := MapID(test.value)
			if (err != nil) != test.wantErr {
				t.Fatalf("MapID(%q) error = %v, wantErr %v", test.value, err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if test.want != uuid.Nil && got != test.want {
				t.Fatalf("MapID(%q) = %s, want %s", test.value, got, test.want)
			}
			if test.value == "course-demo" {
				again, err := MapID(test.value)
				if err != nil || again != got {
					t.Fatalf("повторный MapID(%q) = %s, %v; want %s, nil", test.value, again, err, got)
				}
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name     string
		fixtures Fixtures
		wantErr  string
	}{
		{
			name: "нормализует не UUID идентификаторы и пустой контент урока",
			fixtures: Fixtures{
				CompanyID: "company-demo",
				Courses: []CourseFixture{{
					ID: "course-demo", Title: "Курс", Status: "draft", AuthorID: "author-demo",
					CreatedAt: "2026-01-02T03:04:05Z", UpdatedAt: "2026-01-02T03:04:05Z",
				}},
				CourseSections: []CourseSectionFixture{{ID: "section-demo", CourseID: "course-demo", Title: "Раздел"}},
				Lessons:        []LessonFixture{{ID: "lesson-demo", CourseID: "course-demo", SectionID: "section-demo", Title: "Урок"}},
				Assignments: []AssignmentFixture{{
					ID: "assignment-demo", CourseID: "course-demo", AssigneeType: "user", AssigneeID: stringPointer("student-demo"),
					AssignedByID: "author-demo", CreatedAt: "2026-01-02T03:04:05Z",
				}},
			},
		},
		{
			name: "отклоняет неизвестный статус курса",
			fixtures: Fixtures{
				CompanyID: "company-demo",
				Courses: []CourseFixture{{
					ID: "course-demo", Title: "Курс", Status: "archived", AuthorID: "author-demo",
					CreatedAt: "2026-01-02T03:04:05Z", UpdatedAt: "2026-01-02T03:04:05Z",
				}},
			},
			wantErr: "некорректный статус",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataset, err := Normalize(test.fixtures)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Normalize() error = %v, want contains %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(dataset.Lessons) != 1 || string(dataset.Lessons[0].Content) != `{"type":"doc"}` {
				t.Fatalf("unexpected lessons: %#v", dataset.Lessons)
			}
			if len(dataset.Assignments) != 1 || len(dataset.Assignments[0].ResolvedUserIDs) != 1 {
				t.Fatalf("unexpected assignments: %#v", dataset.Assignments)
			}
			if len(dataset.Courses) != 1 || dataset.Courses[0].VersionID == uuid.Nil {
				t.Fatalf("unexpected courses: %#v", dataset.Courses)
			}
			if dataset.Courses[0].VersionID == dataset.Courses[0].ID {
				t.Fatalf("version ID %s совпал с course ID", dataset.Courses[0].VersionID)
			}
			again, err := Normalize(test.fixtures)
			if err != nil {
				t.Fatalf("повторный Normalize(): %v", err)
			}
			if again.Courses[0].VersionID != dataset.Courses[0].VersionID {
				t.Fatalf("version ID недетерминирован: %s, затем %s",
					dataset.Courses[0].VersionID, again.Courses[0].VersionID)
			}
		})
	}
}

func TestTenantCleanupStatementsAreCompanyScopedAndOrdered(t *testing.T) {
	if len(tenantCleanupStatements) == 0 {
		t.Fatal("список tenant cleanup пуст")
	}
	positions := make(map[string]int, len(tenantCleanupStatements))
	for index, statement := range tenantCleanupStatements {
		positions[statement.entity] = index
		if !strings.Contains(statement.query, "company_id") ||
			!strings.Contains(statement.query, "$1") {
			t.Errorf("cleanup %q не ограничен company_id: %s", statement.entity, statement.query)
		}
	}
	for _, dependency := range []struct {
		child  string
		parent string
	}{
		{child: "агрегаты воронки кампаний", parent: "внешние кампании"},
		{child: "аналитические события кампаний", parent: "внешние кампании"},
		{child: "история кампаний", parent: "внешние кампании"},
		{child: "история персональных доступов", parent: "персональные доступы"},
		{child: "история состояний прохождений", parent: "прохождения"},
		{child: "идемпотентность внешних команд", parent: "прохождения"},
		{child: "идемпотентность внешних команд", parent: "внешние ученики"},
		{child: "попытки тестов", parent: "прохождения"},
		{child: "попытки тестов", parent: "тесты версий"},
		{child: "прогресс уроков", parent: "прохождения"},
		{child: "персональные доступы", parent: "прохождения"},
		{child: "персональные доступы", parent: "внешние ученики"},
		{child: "проверки внешнего email", parent: "внешние ученики"},
		{child: "внешние сессии", parent: "внешние ученики"},
		{child: "прохождения", parent: "назначения"},
		{child: "прохождения", parent: "версии курсов"},
		{child: "назначения", parent: "версии курсов"},
		{child: "идемпотентность создания из шаблона", parent: "происхождение курсов"},
		{child: "идемпотентность создания из шаблона", parent: "версии курсов"},
		{child: "идемпотентность копирования курса", parent: "происхождение курсов"},
		{child: "идемпотентность копирования курса", parent: "версии курсов"},
		{child: "происхождение курсов", parent: "курсы"},
		{child: "ограничения курсов", parent: "курсы"},
		{child: "идемпотентность публикаций", parent: "версии курсов"},
		{child: "элементы заданий копирования файлов", parent: "задания копирования файлов"},
		{child: "тесты версий", parent: "версии курсов"},
		{child: "уроки версий", parent: "версии курсов"},
		{child: "разделы версий", parent: "версии курсов"},
		{child: "версии курсов", parent: "курсы"},
		{child: "legacy-тесты", parent: "legacy-уроки"},
		{child: "legacy-уроки", parent: "legacy-разделы"},
		{child: "legacy-разделы", parent: "курсы"},
	} {
		child, childOK := positions[dependency.child]
		parent, parentOK := positions[dependency.parent]
		if !childOK || !parentOK {
			t.Fatalf("cleanup dependency отсутствует: %#v", dependency)
		}
		if child >= parent {
			t.Errorf("cleanup %q должен идти до %q", dependency.child, dependency.parent)
		}
	}
}

func TestAssignmentEnrollmentProjectionExcludesExternalAssignments(t *testing.T) {
	if !strings.Contains(assignmentEnrollmentsSQL,
		"assignment.assignee_type IN ('user', 'position', 'department')") {
		t.Fatal("проекция назначений не ограничена внутренними типами получателей")
	}
	if strings.Contains(assignmentEnrollmentsSQL, "'external'") {
		t.Fatal("проекция назначений содержит universal external expansion")
	}
}

func TestCleanupTenantExplainsRequiredDevPrivilege(t *testing.T) {
	wantErr := errors.New("permission denied")
	err := cleanupTenant(context.Background(), failingCommandExecutor{err: wantErr}, uuid.New())
	if !errors.Is(err, wantErr) {
		t.Fatalf("cleanupTenant() error = %v, want wrapped %v", err, wantErr)
	}
	if !strings.Contains(err.Error(), "dev-only") ||
		!strings.Contains(err.Error(), "session_replication_role") {
		t.Fatalf("cleanupTenant() error не объясняет требуемое право: %v", err)
	}
}

type failingCommandExecutor struct{ err error }

func (executor failingCommandExecutor) Exec(
	context.Context,
	string,
	...any,
) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, executor.err
}

func stringPointer(value string) *string { return &value }
