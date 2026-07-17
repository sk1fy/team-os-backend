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

func TestDistributionReadsRequireAdministrator(t *testing.T) {
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

func assertForbidden(t *testing.T, err error) {
	t.Helper()
	var applicationErr *Error
	if !errors.As(err, &applicationErr) || applicationErr.Kind != ErrorForbidden {
		t.Fatalf("error = %v, want forbidden", err)
	}
}
