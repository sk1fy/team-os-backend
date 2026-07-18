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

func TestArticleVisibilityMatrix(t *testing.T) {
	t.Parallel()
	service := &Service{}
	publicID, companyID := uuid.New(), uuid.New()
	sections := map[uuid.UUID]Section{
		publicID:  {ID: publicID, Visibility: "public", Access: AccessSettings{Scope: domainaccess.ScopeCustom}},
		companyID: {ID: companyID, Visibility: "company", Access: AccessSettings{Scope: domainaccess.ScopeCompany}},
	}
	employee := Actor{Role: "employee", UserID: uuid.New()}
	if !service.canReadArticle(employee, Article{SectionID: publicID, Status: "published"}, sections) {
		t.Fatal("published article in public section must be readable")
	}
	if service.canReadArticle(employee, Article{SectionID: publicID, Status: "draft"}, sections) {
		t.Fatal("draft in public section must not be readable")
	}
	if !service.canReadArticle(employee, Article{SectionID: companyID, Status: "published"}, sections) {
		t.Fatal("published company article must be readable by employee")
	}
	if !service.canReadArticle(Actor{Role: "admin"}, Article{SectionID: companyID, Status: "draft"}, sections) {
		t.Fatal("manager must be able to read drafts")
	}
}
