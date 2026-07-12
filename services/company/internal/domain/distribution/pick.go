package distribution

import (
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
)

var ErrNoEnabledMembers = errors.New("в группе нет сотрудников")

type Algorithm string

const (
	RoundRobin  Algorithm = "round_robin"
	LeastLoaded Algorithm = "least_loaded"
	Priority    Algorithm = "priority"
)

type Group struct {
	ID                uuid.UUID
	Algorithm         Algorithm
	MemberIDs         []uuid.UUID
	DisabledMemberIDs []uuid.UUID
}

type Event struct {
	GroupID   uuid.UUID
	UserID    uuid.UUID
	Status    string
	CreatedAt time.Time
}

func PickMember(group Group, events []Event) (uuid.UUID, error) {
	disabled := make(map[uuid.UUID]struct{}, len(group.DisabledMemberIDs))
	for _, id := range group.DisabledMemberIDs {
		disabled[id] = struct{}{}
	}
	enabled := make([]uuid.UUID, 0, len(group.MemberIDs))
	for _, id := range group.MemberIDs {
		if _, skip := disabled[id]; !skip {
			enabled = append(enabled, id)
		}
	}
	if len(enabled) == 0 {
		return uuid.Nil, ErrNoEnabledMembers
	}
	if group.Algorithm == Priority {
		return enabled[0], nil
	}
	if group.Algorithm == RoundRobin {
		var last *Event
		for index := range events {
			event := &events[index]
			if event.GroupID == group.ID && (last == nil || event.CreatedAt.After(last.CreatedAt)) {
				last = event
			}
		}
		lastIndex := -1
		if last != nil {
			for index, id := range enabled {
				if id == last.UserID {
					lastIndex = index
					break
				}
			}
		}
		return enabled[(lastIndex+1)%len(enabled)], nil
	}
	load := make(map[uuid.UUID]int, len(enabled))
	for _, id := range enabled {
		load[id] = 0
	}
	for _, event := range events {
		if event.GroupID != group.ID || event.Status == "declined" || event.Status == "reassigned" {
			continue
		}
		if _, exists := load[event.UserID]; exists {
			load[event.UserID]++
		}
	}
	sort.SliceStable(enabled, func(i, j int) bool { return load[enabled[i]] < load[enabled[j]] })
	return enabled[0], nil
}
