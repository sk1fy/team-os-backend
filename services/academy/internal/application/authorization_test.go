package application

import (
	"testing"

	"github.com/google/uuid"
)

func TestCanReadAcademy(t *testing.T) {
	for _, role := range []string{"owner", "admin", "employee", "partner"} {
		if !canReadAcademy(Actor{Role: role}) {
			t.Fatalf("роль %q должна иметь доступ к академии", role)
		}
	}
	for _, role := range []string{"", "unknown"} {
		if canReadAcademy(Actor{Role: role}) {
			t.Fatalf("роль %q не должна иметь доступ к академии", role)
		}
	}
}

func TestVisibleCourseMatrix(t *testing.T) {
	t.Parallel()
	assignedID := uuid.New()
	assigned := map[uuid.UUID]struct{}{assignedID: {}}
	tests := []struct {
		name   string
		actor  Actor
		course Course
		want   bool
	}{
		{name: "owner sees draft", actor: Actor{Role: "owner"}, course: Course{Status: "draft", Visibility: "restricted"}, want: true},
		{name: "employee cannot see draft", actor: Actor{Role: "employee"}, course: Course{Status: "draft", Visibility: "public"}},
		{name: "employee sees public", actor: Actor{Role: "employee"}, course: Course{Status: "published", Visibility: "public"}, want: true},
		{name: "employee sees company", actor: Actor{Role: "employee"}, course: Course{Status: "published", Visibility: "company"}, want: true},
		{name: "employee sees assigned restricted", actor: Actor{Role: "employee"}, course: Course{ID: assignedID, Status: "published", Visibility: "restricted"}, want: true},
		{name: "employee cannot see unassigned restricted", actor: Actor{Role: "employee"}, course: Course{ID: uuid.New(), Status: "published", Visibility: "restricted"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := visibleCourse(test.actor, test.course, assigned); got != test.want {
				t.Fatalf("visibleCourse() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestAssigneeTypeToEvent(t *testing.T) {
	for _, value := range []string{"user", "position", "department", "external"} {
		if assigneeTypeToEvent(value) == 0 {
			t.Fatalf("тип назначения %q не преобразован", value)
		}
	}
	if assigneeTypeToEvent("unknown") != 0 {
		t.Fatal("неизвестный тип назначения должен оставаться unspecified")
	}
}
