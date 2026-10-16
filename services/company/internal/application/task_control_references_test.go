package application

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
)

func TestReferenceSelectionEligibilityUsesOnlyProvenIdentity(t *testing.T) {
	id := uuid.New()
	rows := []referenceEmployeeRow{
		{EmployeeID: id, Name: "Same Name", Active: true, CRMUserIDs: []string{}},
		{EmployeeID: uuid.New(), Active: true, CRMUserIDs: []string{"1"}},
		{EmployeeID: uuid.New(), Active: true, CRMUserIDs: []string{"2", "3"}},
		{EmployeeID: uuid.New(), Active: true, CRMUserIDs: []string{"4"}},
		{EmployeeID: uuid.New(), Active: true, CRMUserIDs: []string{"4"}},
		{EmployeeID: uuid.New(), Active: false, CRMUserIDs: []string{"5"}},
		{EmployeeID: uuid.New(), Active: true, CRMUserIDs: []string{"6"}},
		{EmployeeID: uuid.New(), Active: true, CRMUserIDs: []string{"7"}},
	}
	snap := &tc.ReferenceSnapshot{Users: []tc.ReferenceUser{{ID: "1", Name: "Same Name", IsActive: true}, {ID: "4", IsActive: true}, {ID: "5", IsActive: true}, {ID: "6", IsActive: false}}}
	out := referenceEmployees(rows, snap)
	want := []string{"mapping_missing", "", "mapping_ambiguous", "mapping_ambiguous", "mapping_ambiguous", "employee_inactive", "crm_user_inactive", "crm_user_missing"}
	for i, e := range out {
		if want[i] == "" {
			if !e.Eligible || e.IneligibleReason != nil {
				t.Fatalf("active proven identity ineligible: %+v", e)
			}
		} else if e.Eligible || e.IneligibleReason == nil || *e.IneligibleReason != want[i] {
			t.Fatalf("row%d: %+v", i, e)
		}
	}
	// Missing references cannot turn a proven mapping into an eligible employee.
	out = referenceEmployees(rows, nil)
	if out[1].Eligible || *out[1].IneligibleReason != "references_unavailable" {
		t.Fatal(out[1])
	}
}

func TestTaskControlReferenceViewResponseBound(t *testing.T) {
	view := TaskControlReferences{Employees: []TaskControlReferenceEmployee{{Name: strings.Repeat("x", taskControlReferenceViewMaxBytes)}}}
	var appErr *Error
	if e := taskControlReferenceViewSize(view); !errors.As(e, &appErr) || appErr.Kind != ErrorPayloadTooLarge || appErr.Code != "references_too_large" {
		t.Fatal("oversize selector accepted", e)
	}
	view.Employees[0].Name = "Допустимый сотрудник"
	if e := taskControlReferenceViewSize(view); e != nil {
		t.Fatal(e)
	}
}
