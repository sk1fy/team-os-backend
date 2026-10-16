package application

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestTaskControlStableErrors(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{forbidden("denied"), "task_control_access_denied"},
		{notFound("scope"), "task_control_not_found"},
		{conflict("revision"), "task_control_revision_conflict"},
		{coded(ErrorConflict, "task_control_idempotency_conflict", "key"), "task_control_idempotency_conflict"},
	} {
		var got *Error
		if !errors.As(TaskControlError(test.err), &got) || got.Code != test.code {
			t.Fatalf("%v", got)
		}
	}
	c := db.TaskControlConnection{ID: uuid.New(), ModeEpoch: 2, BindingRevision: 3}
	var err *Error
	if !errors.As(tcFence(c, 3, 1), &err) || err.Code != "task_control_epoch_conflict" {
		t.Fatal(err)
	}
	if !errors.As(tcFence(c, 1, 2), &err) || err.Code != "task_control_revision_conflict" {
		t.Fatal(err)
	}
}
