package application

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestChooseDistributionPlanSharedKeepOrderAndWaiting(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	one, two := "1", "2"
	available := []DistributionAvailability{{EmployeeID: a, CRMUserID: &one, Available: true}, {EmployeeID: b, CRMUserID: &two, Available: true}}
	if p := chooseDistributionPlan(one, true, available, []uuid.UUID{a, b}, []uuid.UUID{a, b}, 1); p.Employee != a || p.Kind != "keep" {
		t.Fatal(p)
	}
	if p := chooseDistributionPlan(one, false, available, []uuid.UUID{a, b}, []uuid.UUID{a, b}, 1); p.Employee != b || p.Kind != "assign" {
		t.Fatal(p)
	}
	shift := time.Now().Add(time.Hour)
	available[0].Available = false
	available[1].Available = false
	available[1].NextShift = &shift
	if p := chooseDistributionPlan(one, true, available, []uuid.UUID{a, b}, nil, 0); p.Kind != "wait" || p.Next == nil {
		t.Fatal(p)
	}
	available[1].NextShift = nil
	if p := chooseDistributionPlan(one, true, available, []uuid.UUID{a, b}, nil, 0); p.Kind != "requires_configuration" {
		t.Fatal(p)
	}
}
