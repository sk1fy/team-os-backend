package authorization

import (
	"testing"
	"time"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/personalaccess"
)

func TestCreatePersonalAccessPolicy(t *testing.T) {
	t.Parallel()

	partnerID := course.ID("partner-1")
	target := course.Course{
		ID: "course-1", CompanyID: "company-1", OwnerType: course.CourseOwnerPartner,
		OwnerUserID: &partnerID, LifecycleStatus: course.CourseActive,
		DistributionStatus: course.DistributionActive,
	}
	version := publishedCourseVersion()
	tests := []struct {
		name    string
		actor   Actor
		mutate  func(*course.Course, *courseversion.Snapshot)
		allowed bool
	}{
		{name: "own partner published course", actor: actor(RolePartner), allowed: true},
		{name: "owner cannot issue partner personal link", actor: actor(RoleOwner)},
		{name: "admin cannot issue partner personal link", actor: actor(RoleAdmin)},
		{name: "employee denied", actor: actor(RoleEmployee)},
		{name: "other partner", actor: func() Actor { value := actor(RolePartner); value.UserID = "partner-2"; return value }()},
		{name: "cross tenant", actor: func() Actor { value := actor(RolePartner); value.CompanyID = "company-2"; return value }()},
		{name: "company course", actor: actor(RolePartner), mutate: func(c *course.Course, _ *courseversion.Snapshot) {
			c.OwnerType = course.CourseOwnerCompany
			c.OwnerUserID = nil
		}},
		{name: "archived course", actor: actor(RolePartner), mutate: func(c *course.Course, _ *courseversion.Snapshot) { c.LifecycleStatus = course.CourseArchived }},
		{name: "paused course", actor: actor(RolePartner), mutate: func(c *course.Course, _ *courseversion.Snapshot) { c.DistributionStatus = course.DistributionPaused }},
		{name: "blocked course", actor: actor(RolePartner), mutate: func(c *course.Course, _ *courseversion.Snapshot) { c.DistributionStatus = course.DistributionBlocked }},
		{name: "draft version", actor: actor(RolePartner), mutate: func(_ *course.Course, v *courseversion.Snapshot) {
			v.Status = courseversion.StatusDraft
			v.PublishedAt = nil
			v.PublishedByID = nil
			v.ContentHash = ""
		}},
		{name: "other course version", actor: actor(RolePartner), mutate: func(_ *course.Course, v *courseversion.Snapshot) { v.CourseID = "course-2" }},
		{name: "other company version", actor: actor(RolePartner), mutate: func(_ *course.Course, v *courseversion.Snapshot) { v.CompanyID = "company-2" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			courseValue := target
			versionValue := version
			if test.mutate != nil {
				test.mutate(&courseValue, &versionValue)
			}
			if got := CanCreatePersonalAccess(test.actor, courseValue, versionValue); got != test.allowed {
				t.Fatalf("CanCreatePersonalAccess() = %v, want %v", got, test.allowed)
			}
		})
	}
}

func TestPersonalAccessManageAndReadOnlyReportPolicy(t *testing.T) {
	t.Parallel()

	target := validPersonalAccess()
	tests := []struct {
		name   string
		actor  Actor
		manage bool
		view   bool
	}{
		{name: "owner read only", actor: actor(RoleOwner), view: true},
		{name: "admin read only", actor: actor(RoleAdmin), view: true},
		{name: "partner owns", actor: actor(RolePartner), manage: true, view: true},
		{name: "other partner", actor: func() Actor { value := actor(RolePartner); value.UserID = "partner-2"; return value }()},
		{name: "employee", actor: actor(RoleEmployee)},
		{name: "cross tenant manager", actor: func() Actor { value := actor(RoleOwner); value.CompanyID = "company-2"; return value }()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := CanManagePersonalAccess(test.actor, target); got != test.manage {
				t.Errorf("CanManagePersonalAccess() = %v, want %v", got, test.manage)
			}
			if got := CanViewPersonalAccess(test.actor, target); got != test.view {
				t.Errorf("CanViewPersonalAccess() = %v, want %v", got, test.view)
			}
		})
	}
}

func publishedCourseVersion() courseversion.Snapshot {
	publisher := courseversion.ID("partner-1")
	publishedAt := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	return courseversion.Snapshot{
		ID: "version-1", CompanyID: "company-1", CourseID: "course-1", Number: 1,
		Status: courseversion.StatusPublished, CreatedByID: publisher,
		CreatedAt: publishedAt.Add(-time.Hour), PublishedByID: &publisher,
		PublishedAt: &publishedAt, ContentHash: "hash",
	}
}

func validPersonalAccess() personalaccess.Snapshot {
	return personalaccess.Snapshot{
		ID: "access-1", CompanyID: "company-1", CourseID: "course-1", CourseVersionID: "version-1",
		PartnerOwnerID: "partner-1", ExpectedEmail: "ivan@example.com", NormalizedExpectedEmail: "ivan@example.com", DeadlineDays: 3,
		Status: personalaccess.StatusIssued, TokenHash: make([]byte, 32), TokenPrefix: "prefix",
		RootAccessID: "access-1", AttemptNumber: 1, IssuanceIdempotencyKey: "issue-1",
		IssuedByID: "partner-1", IssuedAt: time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC),
	}
}
