package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	dto "github.com/sk1fy/team-os-backend/services/company/internal/activitycontract"
)

type activityCoreFake struct {
	ActivityCore
	calls int
	scope dto.ActivityScope
}

func (c *activityCoreFake) Verify(_ context.Context, s dto.ActivityScope) error {
	c.calls++
	c.scope = s
	return nil
}
func TestActivityRechecksRoleAndCompany(t *testing.T) {
	for _, tc := range []struct {
		name, role, status string
		foreign, allowed   bool
	}{{"owner", "owner", "active", false, true}, {"admin", "admin", "active", false, true}, {"stale JWT owner", "employee", "active", false, false}, {"disabled member", "owner", "deactivated", false, false}, {"foreign account", "owner", "active", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatal(err)
			}
			defer mock.Close()
			company, user := uuid.New(), uuid.New()
			now := time.Now()
			core := &activityCoreFake{}
			scope := dto.ActivityScope{CompanyId: company, InstallationId: uuid.New(), IntegrationId: uuid.New(), AccountId: "31355990"}
			s := &Service{pool: mock}
			WithActivity(core, []dto.ActivityScope{scope})(s)
			expectAmoSessionCompany(mock, company, "active", now)
			expectAmoSessionUser(mock, company, user, tc.role, tc.status, now)
			if tc.role != "employee" && tc.status == "active" {
				accountCompany := company
				if tc.foreign {
					accountCompany = uuid.New()
				}
				expectAmoSessionIntegration(mock, accountCompany, "active", now)
			}
			out, err := s.ActivityConnection(context.Background(), Actor{CompanyID: company, UserID: user, Role: "owner"})
			if tc.allowed {
				if err != nil || core.calls != 1 || core.scope != scope {
					t.Fatalf("allowed=%+v %v calls %d", out, err, core.calls)
				}
			} else {
				var app *Error
				if !errors.As(err, &app) || app.Kind != ErrorForbidden || core.calls != 0 {
					t.Fatalf("denial=%v calls %d", err, core.calls)
				}
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestActivityRebindingRejectsStaleTaskPanelBeforeCore(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()
	company, user := uuid.New(), uuid.New()
	now := time.Now()
	core := &activityCoreFake{}
	original := dto.ActivityScope{CompanyId: company, InstallationId: uuid.New(), IntegrationId: uuid.New(), AccountId: "31355990"}
	replacement := original
	replacement.InstallationId = uuid.New()
	s := &Service{pool: mock}
	WithActivity(core, []dto.ActivityScope{replacement})(s)
	expectAmoSessionCompany(mock, company, "active", now)
	expectAmoSessionUser(mock, company, user, "owner", "active", now)
	_, err = s.SaveActivityTaskPanel(context.Background(), Actor{CompanyID: company, UserID: user}, activityBindingID(original), dto.ActivityTaskPanel{Revision: 1, EmployeeIds: []int64{}})
	var app *Error
	if !errors.As(err, &app) || app.Kind != ErrorConflict || app.Code != "activity_binding_changed" {
		t.Fatalf("stale scope accepted: %v", err)
	}
	if core.calls != 0 {
		t.Fatalf("stale draft reached replacement Core scope %d times", core.calls)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestActivityBindingIdentityIncludesEveryAuthorityField(t *testing.T) {
	original := dto.ActivityScope{CompanyId: uuid.New(), InstallationId: uuid.New(), IntegrationId: uuid.New(), AccountId: "42"}
	for _, change := range []func(*dto.ActivityScope){func(s *dto.ActivityScope) { s.CompanyId = uuid.New() }, func(s *dto.ActivityScope) { s.InstallationId = uuid.New() }, func(s *dto.ActivityScope) { s.IntegrationId = uuid.New() }, func(s *dto.ActivityScope) { s.AccountId = "43" }} {
		changed := original
		change(&changed)
		if activityBindingID(changed) == activityBindingID(original) {
			t.Fatalf("scope change retained binding: %+v", changed)
		}
	}
}
