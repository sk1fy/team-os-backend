package authorization

import (
	"testing"
	"time"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/externalcampaign"
)

func TestCreateExternalCampaignPolicy(t *testing.T) {
	t.Parallel()

	partnerID := course.ID("partner-1")
	partnerTarget := course.Course{
		ID: "course-1", CompanyID: "company-1", OwnerType: course.CourseOwnerPartner,
		OwnerUserID: &partnerID, LifecycleStatus: course.CourseActive,
		DistributionStatus: course.DistributionActive,
	}
	companyTarget := partnerTarget
	companyTarget.OwnerType = course.CourseOwnerCompany
	companyTarget.OwnerUserID = nil
	version := publishedCourseVersion()
	tests := []struct {
		name    string
		actor   Actor
		target  course.Course
		purpose externalcampaign.Purpose
		mutate  func(*course.Course, *courseversion.Snapshot)
		allowed bool
	}{
		{name: "owner company candidate", actor: actor(RoleOwner), target: companyTarget, purpose: externalcampaign.PurposeCompanyCandidate, allowed: true},
		{name: "admin company candidate", actor: actor(RoleAdmin), target: companyTarget, purpose: externalcampaign.PurposeCompanyCandidate, allowed: true},
		{name: "partner own promo", actor: actor(RolePartner), target: partnerTarget, purpose: externalcampaign.PurposePartnerPromo, allowed: true},
		{name: "owner cannot create partner promo", actor: actor(RoleOwner), target: partnerTarget, purpose: externalcampaign.PurposePartnerPromo},
		{name: "partner cannot create company candidate", actor: actor(RolePartner), target: companyTarget, purpose: externalcampaign.PurposeCompanyCandidate},
		{name: "partner cannot use company course for promo", actor: actor(RolePartner), target: companyTarget, purpose: externalcampaign.PurposePartnerPromo},
		{name: "manager cannot use partner course for candidate", actor: actor(RoleAdmin), target: partnerTarget, purpose: externalcampaign.PurposeCompanyCandidate},
		{name: "employee denied", actor: actor(RoleEmployee), target: companyTarget, purpose: externalcampaign.PurposeCompanyCandidate},
		{name: "other partner denied", actor: func() Actor {
			value := actor(RolePartner)
			value.UserID = "partner-2"
			return value
		}(), target: partnerTarget, purpose: externalcampaign.PurposePartnerPromo},
		{name: "cross tenant denied", actor: func() Actor {
			value := actor(RoleOwner)
			value.CompanyID = "company-2"
			return value
		}(), target: companyTarget, purpose: externalcampaign.PurposeCompanyCandidate},
		{name: "unknown purpose", actor: actor(RoleOwner), target: companyTarget, purpose: "other"},
		{name: "archived course", actor: actor(RoleOwner), target: companyTarget, purpose: externalcampaign.PurposeCompanyCandidate, mutate: func(c *course.Course, _ *courseversion.Snapshot) {
			c.LifecycleStatus = course.CourseArchived
		}},
		{name: "paused course", actor: actor(RolePartner), target: partnerTarget, purpose: externalcampaign.PurposePartnerPromo, mutate: func(c *course.Course, _ *courseversion.Snapshot) {
			c.DistributionStatus = course.DistributionPaused
		}},
		{name: "blocked course", actor: actor(RolePartner), target: partnerTarget, purpose: externalcampaign.PurposePartnerPromo, mutate: func(c *course.Course, _ *courseversion.Snapshot) {
			c.DistributionStatus = course.DistributionBlocked
		}},
		{name: "draft version", actor: actor(RoleOwner), target: companyTarget, purpose: externalcampaign.PurposeCompanyCandidate, mutate: func(_ *course.Course, v *courseversion.Snapshot) {
			v.Status = courseversion.StatusDraft
			v.PublishedAt = nil
			v.PublishedByID = nil
			v.ContentHash = ""
		}},
		{name: "retired version", actor: actor(RoleOwner), target: companyTarget, purpose: externalcampaign.PurposeCompanyCandidate, mutate: func(_ *course.Course, v *courseversion.Snapshot) {
			v.Status = courseversion.StatusRetired
		}},
		{name: "other version course", actor: actor(RoleOwner), target: companyTarget, purpose: externalcampaign.PurposeCompanyCandidate, mutate: func(_ *course.Course, v *courseversion.Snapshot) {
			v.CourseID = "course-2"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			target := test.target
			selected := version
			if test.mutate != nil {
				test.mutate(&target, &selected)
			}
			if got := CanCreateExternalCampaign(test.actor, target, selected, test.purpose); got != test.allowed {
				t.Fatalf("CanCreateExternalCampaign() = %v, want %v", got, test.allowed)
			}
		})
	}
}

func TestExternalCampaignManageAndReportScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		actor  Actor
		target externalcampaign.Snapshot
		manage bool
		view   bool
	}{
		{name: "owner manages company candidate", actor: actor(RoleOwner), target: validCompanyCampaign(), manage: true, view: true},
		{name: "admin manages company candidate", actor: actor(RoleAdmin), target: validCompanyCampaign(), manage: true, view: true},
		{name: "partner cannot see company candidate", actor: actor(RolePartner), target: validCompanyCampaign()},
		{name: "employee cannot see company candidate", actor: actor(RoleEmployee), target: validCompanyCampaign()},
		{name: "partner manages own promo", actor: actor(RolePartner), target: validPartnerCampaign(), manage: true, view: true},
		{name: "owner sees partner promo read only", actor: actor(RoleOwner), target: validPartnerCampaign(), view: true},
		{name: "admin sees partner promo read only", actor: actor(RoleAdmin), target: validPartnerCampaign(), view: true},
		{name: "other partner denied", actor: func() Actor {
			value := actor(RolePartner)
			value.UserID = "partner-2"
			return value
		}(), target: validPartnerCampaign()},
		{name: "cross tenant manager denied", actor: func() Actor {
			value := actor(RoleOwner)
			value.CompanyID = "company-2"
			return value
		}(), target: validCompanyCampaign()},
		{name: "closed company campaign remains reportable", actor: actor(RoleOwner), target: closedCompanyCampaign(), manage: true, view: true},
		{name: "closed partner campaign remains scoped", actor: actor(RolePartner), target: closedPartnerCampaign(), manage: true, view: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := CanManageExternalCampaign(test.actor, test.target); got != test.manage {
				t.Errorf("CanManageExternalCampaign() = %v, want %v", got, test.manage)
			}
			if got := CanViewExternalCampaign(test.actor, test.target); got != test.view {
				t.Errorf("CanViewExternalCampaign() = %v, want %v", got, test.view)
			}
			if got := CanViewExternalCampaignReport(test.actor, test.target); got != test.view {
				t.Errorf("CanViewExternalCampaignReport() = %v, want %v", got, test.view)
			}
		})
	}
}

func TestExternalCampaignPolicyRejectsMalformedTarget(t *testing.T) {
	t.Parallel()

	targets := []externalcampaign.Snapshot{
		{},
		func() externalcampaign.Snapshot {
			value := validCompanyCampaign()
			value.OwnerUserID = externalCampaignIDPointer("owner-1")
			return value
		}(),
		func() externalcampaign.Snapshot {
			value := validPartnerCampaign()
			value.CreatedByID = "partner-2"
			return value
		}(),
	}
	for _, target := range targets {
		for _, value := range knownActors() {
			if CanManageExternalCampaign(value, target) || CanViewExternalCampaign(value, target) ||
				CanViewExternalCampaignReport(value, target) {
				t.Fatalf("malformed campaign allowed for role %q: %#v", value.Role, target)
			}
		}
	}
}

func validCompanyCampaign() externalcampaign.Snapshot {
	return externalcampaign.Snapshot{
		ID: "campaign-1", CompanyID: "company-1", CourseID: "course-1", CourseVersionID: "version-1",
		OwnerType: externalcampaign.OwnerCompany, Purpose: externalcampaign.PurposeCompanyCandidate,
		Name: "Кандидаты", DeadlineDays: 3, Status: externalcampaign.StatusActive,
		TokenHash: make([]byte, 32), TokenPrefix: "prefix1", CreatedByID: "owner-1",
		CreatedAt: phase7Now(), UpdatedAt: phase7Now(),
	}
}

func validPartnerCampaign() externalcampaign.Snapshot {
	value := validCompanyCampaign()
	value.OwnerType = externalcampaign.OwnerPartner
	value.OwnerUserID = externalCampaignIDPointer("partner-1")
	value.Purpose = externalcampaign.PurposePartnerPromo
	value.CreatedByID = "partner-1"
	return value
}

func closedCompanyCampaign() externalcampaign.Snapshot {
	value := validCompanyCampaign()
	value.Status = externalcampaign.StatusClosed
	closedAt := phase7Now().Add(time.Hour)
	value.ClosedAt = &closedAt
	value.UpdatedAt = closedAt
	return value
}

func closedPartnerCampaign() externalcampaign.Snapshot {
	value := validPartnerCampaign()
	value.Status = externalcampaign.StatusClosed
	closedAt := phase7Now().Add(time.Hour)
	value.ClosedAt = &closedAt
	value.UpdatedAt = closedAt
	return value
}

func externalCampaignIDPointer(value externalcampaign.ID) *externalcampaign.ID { return &value }

func phase7Now() time.Time {
	return time.Date(2026, time.July, 22, 14, 0, 0, 0, time.UTC)
}
