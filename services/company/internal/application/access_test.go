package application

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestRequireOwnerForEmployeeAccessManagement(t *testing.T) {
	if err := requireOwner(Actor{Role: "owner"}); err != nil {
		t.Fatalf("owner rejected: %v", err)
	}
	for _, role := range []string{"admin", "employee", "partner"} {
		t.Run(role, func(t *testing.T) {
			err := requireOwner(Actor{Role: role})
			var applicationError *Error
			if !errors.As(err, &applicationError) || applicationError.Kind != ErrorForbidden {
				t.Fatalf("error = %#v, want forbidden", err)
			}
			if applicationError.Message != "Управлять доступом сотрудников может только владелец" {
				t.Fatalf("message = %q", applicationError.Message)
			}
		})
	}
}

func TestValidateAccessTarget(t *testing.T) {
	active := db.User{ID: uuid.New(), Role: "employee", Status: "active"}
	if got, err := validateAccessTarget(active, nil); err != nil || got.ID != active.ID {
		t.Fatalf("active target = %#v, %v", got, err)
	}

	for _, test := range []struct {
		name string
		user db.User
	}{
		{name: "owner", user: db.User{Role: "owner", Status: "active"}},
		{name: "inactive", user: db.User{Role: "employee", Status: "deactivated"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateAccessTarget(test.user, nil)
			var applicationError *Error
			if !errors.As(err, &applicationError) || applicationError.Kind != ErrorValidation {
				t.Fatalf("error = %#v, want validation", err)
			}
		})
	}
}
