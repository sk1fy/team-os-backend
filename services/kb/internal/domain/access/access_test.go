package access

import (
	"testing"

	"github.com/google/uuid"
)

func TestEffectiveAccessInheritsCompanyScope(t *testing.T) {
	t.Parallel()
	root := uuid.New()
	child := uuid.New()
	sections := map[uuid.UUID]Section{
		root: {
			ID: root, ParentID: nil,
			Access: Settings{Scope: ScopeCompany},
		},
		child: {
			ID: child, ParentID: &root,
			Access: Settings{Scope: ScopeCompany},
		},
	}
	got := EffectiveAccess(sections[child], sections)
	if got.Scope != ScopeCompany {
		t.Fatalf("scope = %q, want company", got.Scope)
	}
}

func TestEffectiveAccessUsesCustomOverride(t *testing.T) {
	t.Parallel()
	root := uuid.New()
	child := uuid.New()
	departmentID := uuid.New()
	sections := map[uuid.UUID]Section{
		root: {ID: root, Access: Settings{Scope: ScopeCompany}},
		child: {
			ID: child, ParentID: &root,
			Access: Settings{
				Scope: ScopeCustom, DepartmentIDs: []uuid.UUID{departmentID},
			},
		},
	}
	got := EffectiveAccess(sections[child], sections)
	if got.Scope != ScopeCustom || len(got.DepartmentIDs) != 1 {
		t.Fatalf("unexpected effective access: %+v", got)
	}
}

func TestAllowedCompanyScope(t *testing.T) {
	t.Parallel()
	settings := Settings{Scope: ScopeCompany}
	if !Allowed(Subject{Role: "employee"}, settings) {
		t.Fatal("employee should access company scope")
	}
	if Allowed(Subject{Role: "partner"}, settings) {
		t.Fatal("partner should not access company scope")
	}
}

func TestAllowedCustomScopeByDepartment(t *testing.T) {
	t.Parallel()
	departmentID := uuid.New()
	settings := Settings{Scope: ScopeCustom, DepartmentIDs: []uuid.UUID{departmentID}}
	subject := Subject{
		Role: "employee", DepartmentIDs: []uuid.UUID{departmentID},
	}
	if !Allowed(subject, settings) {
		t.Fatal("expected department access")
	}
}

func TestCanManage(t *testing.T) {
	t.Parallel()
	if !CanManage(Subject{Role: "owner"}) {
		t.Fatal("owner should manage kb")
	}
	if CanManage(Subject{Role: "employee"}) {
		t.Fatal("employee should not manage kb")
	}
}