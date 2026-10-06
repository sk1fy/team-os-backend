package application

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Shift-scope regression: distribution is bound to the member's *current shift*
// (not just a working day). New-stage-entry decisions only; confirmed
// assignments live elsewhere and are never recomputed here.
func TestChooseDistributionPlanShiftScope(t *testing.T) {
	anna, boris, viktor := uuid.New(), uuid.New(), uuid.New()
	annaCRM, borisCRM, viktorCRM := "101", "102", "103"
	ten := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	tomorrow := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)

	t.Run("keep refused when initial responsible is off-shift, other member on shift now", func(t *testing.T) {
		// 07:10 lead on Виктор; Виктор's shift today 10:00. Анна works now.
		avail := []DistributionAvailability{
			{EmployeeID: viktor, CRMUserID: &viktorCRM, Available: false, Reason: "outside_shift", NextShift: &ten},
			{EmployeeID: anna, CRMUserID: &annaCRM, Available: true, Reason: "available"},
		}
		p := chooseDistributionPlan(viktorCRM, true, avail, []uuid.UUID{viktor, anna}, []uuid.UUID{viktor, anna}, 0)
		if p.Kind != "assign" || p.Employee != anna {
			t.Fatalf("expected immediate assign to Anna, got %+v", p)
		}
	})

	t.Run("only today's on-shift member is chosen", func(t *testing.T) {
		// Борис works today, Анна/Виктор only tomorrow.
		avail := []DistributionAvailability{
			{EmployeeID: anna, CRMUserID: &annaCRM, Available: false, Reason: "outside_shift", NextShift: &tomorrow},
			{EmployeeID: boris, CRMUserID: &borisCRM, Available: true, Reason: "available"},
			{EmployeeID: viktor, CRMUserID: &viktorCRM, Available: false, Reason: "outside_shift", NextShift: &tomorrow},
		}
		p := chooseDistributionPlan(annaCRM, true, avail, []uuid.UUID{anna, boris, viktor}, []uuid.UUID{anna, boris, viktor}, 0)
		if p.Kind != "assign" || p.Employee != boris {
			t.Fatalf("expected Boris, got %+v", p)
		}
	})

	t.Run("keep allowed when owner is on shift", func(t *testing.T) {
		avail := []DistributionAvailability{
			{EmployeeID: viktor, CRMUserID: &viktorCRM, Available: true, Reason: "available"},
			{EmployeeID: anna, CRMUserID: &annaCRM, Available: true, Reason: "available"},
		}
		p := chooseDistributionPlan(viktorCRM, true, avail, []uuid.UUID{viktor, anna}, []uuid.UUID{viktor, anna}, 0)
		if p.Kind != "keep" || p.Employee != viktor {
			t.Fatalf("expected keep Viktor, got %+v", p)
		}
	})

	t.Run("nobody on shift -> wait until earliest shift start", func(t *testing.T) {
		earlier := ten
		later := tomorrow
		avail := []DistributionAvailability{
			{EmployeeID: anna, CRMUserID: &annaCRM, Available: false, Reason: "outside_shift", NextShift: &later},
			{EmployeeID: boris, CRMUserID: &borisCRM, Available: false, Reason: "outside_shift", NextShift: &earlier},
		}
		p := chooseDistributionPlan(viktorCRM, true, avail, []uuid.UUID{anna, boris}, nil, 0)
		if p.Kind != "wait" || p.Next == nil || !p.Next.Equal(earlier) {
			t.Fatalf("expected wait until earliest shift, got %+v", p)
		}
	})

	t.Run("nobody on shift and no schedule -> requires_configuration (fail-closed)", func(t *testing.T) {
		avail := []DistributionAvailability{
			{EmployeeID: anna, CRMUserID: nil, Available: false, Reason: "schedule_missing"},
			{EmployeeID: boris, CRMUserID: nil, Available: false, Reason: "schedule_missing"},
		}
		p := chooseDistributionPlan(viktorCRM, true, avail, []uuid.UUID{anna, boris}, nil, 0)
		if p.Kind != "requires_configuration" {
			t.Fatalf("expected fail-closed requires_configuration, got %+v", p)
		}
	})

	t.Run("initial responsible off-shift but not a member -> never kept, assigns member", func(t *testing.T) {
		avail := []DistributionAvailability{
			{EmployeeID: anna, CRMUserID: &annaCRM, Available: true, Reason: "available"},
		}
		// owner CRM id is unknown to the group (not a member).
		p := chooseDistributionPlan(viktorCRM, true, avail, []uuid.UUID{anna}, []uuid.UUID{anna}, 0)
		if p.Kind == "keep" || p.Employee != anna {
			t.Fatalf("expected assign Anna, got %+v", p)
		}
	})
}
