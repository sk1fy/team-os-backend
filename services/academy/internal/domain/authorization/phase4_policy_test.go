package authorization

import (
	"testing"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
)

func TestAdministrativeRestrictionPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		actor Actor
		root  course.Course
		check func(Actor, course.Course) bool
		want  bool
	}{
		{name: "owner pauses active partner course", actor: actor(RoleOwner), root: partnerCourse("partner-2"), check: CanPausePartnerDistribution, want: true},
		{name: "admin blocks active partner course", actor: actor(RoleAdmin), root: partnerCourse("partner-2"), check: CanBlockPartnerCourse, want: true},
		{name: "admin block overrides pause", actor: actor(RoleAdmin), root: withDistribution(partnerCourse("partner-2"), course.DistributionPaused), check: CanBlockPartnerCourse, want: true},
		{name: "owner resolves pause", actor: actor(RoleOwner), root: withDistribution(partnerCourse("partner-2"), course.DistributionPaused), check: CanResolvePartnerRestriction, want: true},
		{name: "admin resolves block", actor: actor(RoleAdmin), root: withDistribution(partnerCourse("partner-2"), course.DistributionBlocked), check: CanResolvePartnerRestriction, want: true},
		{name: "partner cannot pause own original", actor: actor(RolePartner), root: partnerCourse("partner-1"), check: CanPausePartnerDistribution},
		{name: "partner cannot block own original", actor: actor(RolePartner), root: partnerCourse("partner-1"), check: CanBlockPartnerCourse},
		{name: "partner cannot resolve restriction", actor: actor(RolePartner), root: withDistribution(partnerCourse("partner-1"), course.DistributionPaused), check: CanResolvePartnerRestriction},
		{name: "employee cannot restrict", actor: actor(RoleEmployee), root: partnerCourse("partner-2"), check: CanPausePartnerDistribution},
		{name: "manager cannot restrict company course", actor: actor(RoleOwner), root: companyCourse(), check: CanPausePartnerDistribution},
		{name: "manager cannot resolve absent restriction", actor: actor(RoleOwner), root: partnerCourse("partner-2"), check: CanResolvePartnerRestriction},
		{name: "manager cannot resolve deleted course", actor: actor(RoleOwner), root: withLifecycle(withDistribution(partnerCourse("partner-2"), course.DistributionBlocked), course.CourseDeleted), check: CanResolvePartnerRestriction},
		{name: "cross tenant manager cannot restrict", actor: actor(RoleOwner), root: func() course.Course { value := partnerCourse("partner-2"); value.CompanyID = "company-2"; return value }(), check: CanBlockPartnerCourse},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.check(test.actor, test.root); got != test.want {
				t.Errorf("policy = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPartnerOriginalOwnershipBoundaries(t *testing.T) {
	t.Parallel()

	root := partnerCourse("partner-1")
	tests := []struct {
		name  string
		actor Actor
		check func(Actor, course.Course) bool
		want  bool
	}{
		{name: "owner reads", actor: actor(RoleOwner), check: CanViewPartnerCourse, want: true},
		{name: "admin previews", actor: actor(RoleAdmin), check: CanPreviewPartnerCourse, want: true},
		{name: "owner reports", actor: actor(RoleOwner), check: CanViewEnrollmentReport, want: true},
		{name: "owner cannot edit", actor: actor(RoleOwner), check: CanEditPartnerCourse},
		{name: "admin cannot publish", actor: actor(RoleAdmin), check: CanPublishPartnerCourse},
		{name: "owner cannot archive", actor: actor(RoleOwner), check: CanArchiveCourse},
		{name: "admin cannot delete", actor: actor(RoleAdmin), check: CanDeleteCourse},
		{name: "owning partner edits", actor: actor(RolePartner), check: CanEditPartnerCourse, want: true},
		{name: "owning partner publishes", actor: actor(RolePartner), check: CanPublishPartnerCourse, want: true},
		{name: "other partner cannot read", actor: func() Actor { value := actor(RolePartner); value.UserID = "partner-2"; return value }(), check: CanViewPartnerCourse},
		{name: "employee cannot read", actor: actor(RoleEmployee), check: CanViewPartnerCourse},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.check(test.actor, root); got != test.want {
				t.Errorf("policy = %v, want %v", got, test.want)
			}
		})
	}
}

func TestAdminPreviewSurvivesRestrictionButNotDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		lifecycle    course.LifecycleStatus
		distribution course.DistributionStatus
		want         bool
	}{
		{name: "active", lifecycle: course.CourseActive, distribution: course.DistributionActive, want: true},
		{name: "paused", lifecycle: course.CourseActive, distribution: course.DistributionPaused, want: true},
		{name: "blocked", lifecycle: course.CourseActive, distribution: course.DistributionBlocked, want: true},
		{name: "archived", lifecycle: course.CourseArchived, distribution: course.DistributionActive, want: true},
		{name: "deleted", lifecycle: course.CourseDeleted, distribution: course.DistributionBlocked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := partnerCourse("partner-1")
			root.LifecycleStatus = test.lifecycle
			root.DistributionStatus = test.distribution
			if got := CanPreviewPartnerCourse(actor(RoleAdmin), root); got != test.want {
				t.Errorf("CanPreviewPartnerCourse() = %v, want %v", got, test.want)
			}
		})
	}
}
