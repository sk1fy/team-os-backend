package application

import (
	"testing"

	"github.com/google/uuid"
	domainaccess "github.com/sk1fy/team-os-backend/services/kb/internal/domain/access"
)

func TestReadableSectionsFiltersAndReparents(t *testing.T) {
	t.Parallel()
	departmentID := uuid.New()
	closedParentID := uuid.New()
	allowedChildID := uuid.New()
	companySectionID := uuid.New()
	sections := []Section{
		{ID: closedParentID, Name: "Закрытый", Access: AccessSettings{Scope: domainaccess.ScopeCustom}},
		{
			ID: allowedChildID, Name: "Доступный", ParentID: &closedParentID,
			Access: AccessSettings{Scope: domainaccess.ScopeCustom, DepartmentIDs: []uuid.UUID{departmentID}},
		},
		{ID: companySectionID, Name: "Общий", Access: AccessSettings{Scope: domainaccess.ScopeCompany}},
	}
	byID := sectionIndex(sections)

	got := readableSections(Actor{Role: "employee", DepartmentIDs: []uuid.UUID{departmentID}}, sections, byID)
	if len(got) != 2 {
		t.Fatalf("readableSections() len = %d, want 2", len(got))
	}
	if got[0].ID != allowedChildID || got[0].ParentID != nil {
		t.Fatalf("allowed orphan = %+v, want reparented child", got[0])
	}
	if got[1].ID != companySectionID {
		t.Fatalf("second section = %s, want company section", got[1].ID)
	}
}

func TestReadableSectionsManagerSeesAll(t *testing.T) {
	t.Parallel()
	sections := []Section{{ID: uuid.New(), Access: AccessSettings{Scope: domainaccess.ScopeCustom}}}
	got := readableSections(Actor{Role: "admin"}, sections, sectionIndex(sections))
	if len(got) != 1 {
		t.Fatalf("readableSections() len = %d, want 1", len(got))
	}
}
