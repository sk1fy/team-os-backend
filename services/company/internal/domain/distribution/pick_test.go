package distribution

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPickMember(t *testing.T) {
	groupID := uuid.New()
	u1, u2, u3 := uuid.New(), uuid.New(), uuid.New()
	group := Group{ID: groupID, Algorithm: RoundRobin, MemberIDs: []uuid.UUID{u1, u2, u3}}
	events := []Event{{GroupID: groupID, UserID: u1, Status: "accepted", CreatedAt: time.Now()}}
	if got, _ := PickMember(group, events); got != u2 {
		t.Fatalf("round robin = %s, want %s", got, u2)
	}
	group.Algorithm = LeastLoaded
	events = append(events, Event{GroupID: groupID, UserID: u2, Status: "accepted", CreatedAt: time.Now().Add(time.Second)})
	if got, _ := PickMember(group, events); got != u3 {
		t.Fatalf("least loaded = %s, want %s", got, u3)
	}
	group.Algorithm = Priority
	if got, _ := PickMember(group, nil); got != u1 {
		t.Fatalf("priority = %s, want %s", got, u1)
	}
	group.Algorithm, group.DisabledMemberIDs = RoundRobin, []uuid.UUID{u2}
	if got, _ := PickMember(group, []Event{{GroupID: groupID, UserID: u1, CreatedAt: time.Now()}}); got != u3 {
		t.Fatalf("disabled = %s, want %s", got, u3)
	}
}

func TestPickMemberIgnoresInactiveStatusesForLoad(t *testing.T) {
	groupID, u1, u2 := uuid.New(), uuid.New(), uuid.New()
	group := Group{ID: groupID, Algorithm: LeastLoaded, MemberIDs: []uuid.UUID{u1, u2}}
	events := []Event{{GroupID: groupID, UserID: u1, Status: "declined"}, {GroupID: groupID, UserID: u2, Status: "accepted"}}
	if got, _ := PickMember(group, events); got != u1 {
		t.Fatalf("got %s, want %s", got, u1)
	}
}
