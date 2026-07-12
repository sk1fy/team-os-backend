package application

import (
	"testing"

	"github.com/google/uuid"
)

func TestPartnerTaskAccess(t *testing.T) {
	partnerID := uuid.New()
	positionID := uuid.New()
	base := Task{AuthorID: uuid.New()}
	tests := []struct {
		name  string
		actor Actor
		task  Task
		want  bool
	}{
		{name: "author", actor: Actor{UserID: partnerID, Role: "partner"}, task: Task{AuthorID: partnerID}, want: true},
		{name: "assignee", actor: Actor{UserID: partnerID, Role: "partner"}, task: Task{AuthorID: base.AuthorID, AssigneeIDs: []uuid.UUID{partnerID}}, want: true},
		{name: "watcher", actor: Actor{UserID: partnerID, Role: "partner"}, task: Task{AuthorID: base.AuthorID, WatcherIDs: []uuid.UUID{partnerID}}, want: true},
		{name: "position", actor: Actor{UserID: partnerID, Role: "partner", PositionIDs: []uuid.UUID{positionID}}, task: Task{AuthorID: base.AuthorID, AssigneePositionID: &positionID}, want: true},
		{name: "unrelated", actor: Actor{UserID: partnerID, Role: "partner"}, task: base, want: false},
		{name: "employee", actor: Actor{UserID: uuid.New(), Role: "employee"}, task: base, want: true},
		{name: "unknown role", actor: Actor{UserID: uuid.New(), Role: ""}, task: base, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canAccessTask(test.actor, test.task); got != test.want {
				t.Fatalf("canAccessTask() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestBoardStructureAuthorization(t *testing.T) {
	for _, role := range []string{"owner", "admin"} {
		if !canManageBoardStructure(Actor{Role: role}) {
			t.Fatalf("роль %q должна управлять структурой доски", role)
		}
	}
	for _, role := range []string{"employee", "partner", ""} {
		if canManageBoardStructure(Actor{Role: role}) {
			t.Fatalf("роль %q не должна управлять структурой доски", role)
		}
	}
}
