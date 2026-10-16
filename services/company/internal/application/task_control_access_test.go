package application

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
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

func TestTaskControlChangedInstallationRevokesRuntimeCache(t *testing.T) {
	now := time.Now()
	id, integration := uuid.New(), uuid.New()
	st := tc.Installation{CompanyID: uuid.New(), AccountID: "123", InstallationID: &id, IntegrationID: &integration, Product: tc.Product, State: "installed", Generation: 2, Revision: 2, CheckedAt: now, FreshUntil: now.Add(time.Minute)}
	var err *Error
	if !errors.As(taskControlInstalled(st, id, integration, 1, now), &err) || err.Kind != ErrorForbidden || err.Code != "task_control_installation_changed" {
		t.Fatal("must return 403 so existing widget discards revoked cache", err)
	}
}
