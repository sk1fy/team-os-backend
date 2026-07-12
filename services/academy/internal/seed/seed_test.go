package seed

import (
	"strings"
	"testing"

	"github.com/google/uuid"
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
		})
	}
}

func stringPointer(value string) *string { return &value }
