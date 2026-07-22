package authorization

import (
	"testing"
	"time"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	coursetemplate "github.com/sk1fy/team-os-backend/services/academy/internal/domain/template"
)

func TestTemplateObjectPolicyMatrix(t *testing.T) {
	t.Parallel()

	companyTemplate := validTemplate(coursetemplate.TypeCompany)
	systemTemplate := validTemplate(coursetemplate.TypeSystem)
	tests := []struct {
		name    string
		actor   Actor
		target  coursetemplate.Snapshot
		view    bool
		edit    bool
		archive bool
	}{
		{name: "owner company", actor: actor(RoleOwner), target: companyTemplate, view: true, edit: true, archive: true},
		{name: "admin company", actor: actor(RoleAdmin), target: companyTemplate, view: true, edit: true, archive: true},
		{name: "partner company published catalogue", actor: actor(RolePartner), target: companyTemplate, view: true},
		{name: "employee denied", actor: actor(RoleEmployee), target: companyTemplate},
		{name: "owner system read only", actor: actor(RoleOwner), target: systemTemplate, view: true},
		{name: "partner system read only", actor: actor(RolePartner), target: systemTemplate, view: true},
		{name: "cross tenant denied", actor: func() Actor { value := actor(RoleOwner); value.CompanyID = "company-2"; return value }(), target: companyTemplate},
		{name: "partner archived hidden", actor: actor(RolePartner), target: archivedTemplate(companyTemplate)},
		{name: "owner archived visible", actor: actor(RoleOwner), target: archivedTemplate(companyTemplate), view: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := CanViewCourseTemplate(test.actor, test.target); got != test.view {
				t.Errorf("CanViewCourseTemplate() = %v, want %v", got, test.view)
			}
			if got := CanEditCompanyTemplate(test.actor, test.target); got != test.edit {
				t.Errorf("CanEditCompanyTemplate() = %v, want %v", got, test.edit)
			}
			if got := CanArchiveCompanyTemplate(test.actor, test.target); got != test.archive {
				t.Errorf("CanArchiveCompanyTemplate() = %v, want %v", got, test.archive)
			}
		})
	}
}

func TestTemplateInstantiationPolicyAndForcedOwner(t *testing.T) {
	t.Parallel()

	target := validTemplate(coursetemplate.TypeCompany)
	version := publishedTemplateVersion(target)
	tests := []struct {
		name      string
		actor     Actor
		mutate    func(*coursetemplate.Snapshot, *coursetemplate.VersionSnapshot)
		allowed   bool
		ownerType course.OwnerType
		ownerID   course.ID
	}{
		{name: "owner creates company", actor: actor(RoleOwner), allowed: true, ownerType: course.CourseOwnerCompany},
		{name: "admin creates company", actor: actor(RoleAdmin), allowed: true, ownerType: course.CourseOwnerCompany},
		{name: "partner creates own", actor: actor(RolePartner), allowed: true, ownerType: course.CourseOwnerPartner, ownerID: "partner-1"},
		{name: "employee denied", actor: actor(RoleEmployee)},
		{name: "archived denied", actor: actor(RoleOwner), mutate: func(root *coursetemplate.Snapshot, _ *coursetemplate.VersionSnapshot) {
			root.LifecycleStatus = coursetemplate.LifecycleArchived
		}},
		{name: "draft denied", actor: actor(RolePartner), mutate: func(_ *coursetemplate.Snapshot, value *coursetemplate.VersionSnapshot) {
			value.Status = coursetemplate.VersionDraft
		}},
		{name: "other template version denied", actor: actor(RoleOwner), mutate: func(_ *coursetemplate.Snapshot, value *coursetemplate.VersionSnapshot) { value.TemplateID = "other" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := target
			selected := version
			if test.mutate != nil {
				test.mutate(&root, &selected)
			}
			if got := CanInstantiateTemplateVersion(test.actor, root, selected); got != test.allowed {
				t.Fatalf("CanInstantiateTemplateVersion() = %v, want %v", got, test.allowed)
			}
			ownerType, ownerID, ok := TemplateInstantiationOwner(test.actor)
			if test.allowed {
				if !ok || ownerType != test.ownerType || (test.ownerID == "" && ownerID != nil) ||
					(test.ownerID != "" && (ownerID == nil || *ownerID != test.ownerID)) {
					t.Fatalf("TemplateInstantiationOwner() = %q, %#v, %v", ownerType, ownerID, ok)
				}
			} else if test.actor.Role == RoleEmployee && ok {
				t.Fatal("employee received destination owner")
			}
		})
	}
}

func validTemplate(templateType coursetemplate.Type) coursetemplate.Snapshot {
	publishedID := coursetemplate.ID("version-1")
	value := coursetemplate.Snapshot{
		ID: "template-1", CompanyID: "company-1", Type: templateType,
		LifecycleStatus:          coursetemplate.LifecycleActive,
		LatestPublishedVersionID: &publishedID,
		CreatedByID:              "owner-1", CreatedAt: time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC),
	}
	if templateType == coursetemplate.TypeSystem {
		key := "employee-onboarding"
		value.SystemTemplateKey = &key
	}
	return value
}

func archivedTemplate(value coursetemplate.Snapshot) coursetemplate.Snapshot {
	value.LifecycleStatus = coursetemplate.LifecycleArchived
	return value
}

func publishedTemplateVersion(root coursetemplate.Snapshot) coursetemplate.VersionSnapshot {
	publisher := coursetemplate.ID("owner-1")
	publishedAt := time.Date(2026, 7, 22, 14, 0, 0, 0, time.UTC)
	return coursetemplate.VersionSnapshot{
		ID: "version-1", CompanyID: root.CompanyID, TemplateID: root.ID,
		Number: 1, Status: coursetemplate.VersionPublished,
		CreatedByID: publisher, CreatedAt: publishedAt.Add(-time.Hour),
		PublishedByID: &publisher, PublishedAt: &publishedAt, ContentHash: "hash",
	}
}
