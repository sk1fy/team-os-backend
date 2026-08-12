package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestEmployeeCannotUpdateCurrentUser(t *testing.T) {
	service := &Service{}

	_, err := service.UpdateCurrentUser(context.Background(), Actor{Role: "employee"}, UpdateCurrentUserInput{})
	assertForbidden(t, err)
}

func TestDistributionReadsRejectRolesWithoutGrant(t *testing.T) {
	service := &Service{}
	for _, role := range []string{"employee", "partner", ""} {
		t.Run(role, func(t *testing.T) {
			actor := Actor{Role: role}
			_, groupsErr := service.ListDistributionGroups(context.Background(), actor)
			assertForbidden(t, groupsErr)

			_, eventsErr := service.ListDistributionEvents(context.Background(), actor, uuid.New())
			assertForbidden(t, eventsErr)
		})
	}
}

func TestScheduleReadCapabilities(t *testing.T) {
	if !canReadSchedule(Actor{Role: "owner"}) || !canReadSchedule(Actor{Role: "admin"}) {
		t.Fatal("administrator schedule read rejected")
	}
	if !canReadSchedule(Actor{Role: "employee", SectionAccess: []string{"schedule"}}) {
		t.Fatal("employee schedule grant rejected")
	}
	if canReadSchedule(Actor{Role: "employee", SectionAccess: []string{"academy"}}) {
		t.Fatal("employee without schedule grant allowed")
	}
	if canReadSchedule(Actor{Role: "partner"}) {
		t.Fatal("partner schedule read allowed")
	}
}

func TestEmployeeCanReadCoworkerScheduleRows(t *testing.T) {
	actor := Actor{Role: "employee", UserID: uuid.New(), SectionAccess: []string{"schedule"}}
	coworkerID := uuid.New()

	if !canReadScheduleRow(actor, coworkerID) {
		t.Fatal("employee with schedule grant cannot read coworker schedule")
	}
	if canReadScheduleRow(Actor{Role: "employee", SectionAccess: []string{"academy"}}, coworkerID) {
		t.Fatal("employee without schedule grant can read coworker schedule")
	}
}

func assertForbidden(t *testing.T, err error) {
	t.Helper()
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Kind != ErrorForbidden {
		t.Fatalf("error = %v, want forbidden", err)
	}
}
