// Package authorization contains object-level Academy permission rules.
package authorization

import (
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/externalcampaign"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/personalaccess"
	coursetemplate "github.com/sk1fy/team-os-backend/services/academy/internal/domain/template"
)

// Role is a user's role within a company.
type Role string

const (
	RoleOwner    Role = "owner"
	RoleAdmin    Role = "admin"
	RolePartner  Role = "partner"
	RoleEmployee Role = "employee"
)

// Actor contains the claims required by pure authorization rules.
type Actor struct {
	UserID    course.ID
	CompanyID course.ID
	Role      Role
}

// CanCreateCompanyCourse reports whether actor can create a company-owned
// course in their company.
func CanCreateCompanyCourse(actor Actor) bool {
	return validActor(actor) && isManager(actor)
}

// CanCreatePartnerCourse reports whether actor can create a course owned by
// themselves as a partner.
func CanCreatePartnerCourse(actor Actor) bool {
	return validActor(actor) && actor.Role == RolePartner
}

// CanEditCompanyCourse protects company content from partner and employee
// mutations. Archived courses remain editable through a draft; deleted ones do
// not.
func CanEditCompanyCourse(actor Actor, target course.Course) bool {
	return validTarget(actor, target) &&
		isManager(actor) &&
		target.OwnerType == course.CourseOwnerCompany &&
		target.LifecycleStatus != course.CourseDeleted
}

// CanPublishCompanyCourse applies company ownership and prevents publishing
// while the course is deleted or blocked.
func CanPublishCompanyCourse(actor Actor, target course.Course) bool {
	return CanEditCompanyCourse(actor, target) &&
		target.DistributionStatus != course.DistributionBlocked
}

// CanAssignCompanyCourse allows new assignments only for an active,
// distributable company course.
func CanAssignCompanyCourse(actor Actor, target course.Course) bool {
	return CanEditCompanyCourse(actor, target) &&
		target.LifecycleStatus == course.CourseActive &&
		target.DistributionStatus == course.DistributionActive
}

// CanEditPartnerCourse allows a partner to edit only their own course.
// Administrative visibility never implies edit permission.
func CanEditPartnerCourse(actor Actor, target course.Course) bool {
	return validTarget(actor, target) &&
		actor.Role == RolePartner &&
		isOwnedPartnerCourse(actor, target) &&
		target.LifecycleStatus != course.CourseDeleted
}

// CanPublishPartnerCourse prevents a partner from bypassing an administrative
// block with a new published version.
func CanPublishPartnerCourse(actor Actor, target course.Course) bool {
	return CanEditPartnerCourse(actor, target) &&
		target.DistributionStatus != course.DistributionBlocked
}

// CanArchiveCourse allows company managers to archive company content and a
// partner to archive their own partner content.
func CanArchiveCourse(actor Actor, target course.Course) bool {
	return canMutateOwnedCourse(actor, target) &&
		target.LifecycleStatus == course.CourseActive
}

// CanDeleteCourse follows the same ownership boundary as archive. Deletion is
// available from active or archived and denied once already deleted.
func CanDeleteCourse(actor Actor, target course.Course) bool {
	return canMutateOwnedCourse(actor, target) &&
		target.LifecycleStatus != course.CourseDeleted
}

// CanPausePartnerDistribution is an administrative action, not a partner
// content mutation.
func CanPausePartnerDistribution(actor Actor, target course.Course) bool {
	return validTarget(actor, target) &&
		isManager(actor) &&
		target.OwnerType == course.CourseOwnerPartner &&
		target.LifecycleStatus == course.CourseActive &&
		target.DistributionStatus == course.DistributionActive
}

// CanBlockPartnerCourse allows emergency blocking of an active or archived
// partner course, including one whose distribution is already paused.
func CanBlockPartnerCourse(actor Actor, target course.Course) bool {
	return validTarget(actor, target) &&
		isManager(actor) &&
		target.OwnerType == course.CourseOwnerPartner &&
		target.LifecycleStatus != course.CourseDeleted &&
		target.DistributionStatus != course.DistributionBlocked
}

// CanResolvePartnerRestriction lets only owner/admin remove an active pause
// or block. Partners cannot lift an administrative restriction on their own
// content.
func CanResolvePartnerRestriction(actor Actor, target course.Course) bool {
	return validTarget(actor, target) &&
		isManager(actor) &&
		target.OwnerType == course.CourseOwnerPartner &&
		target.LifecycleStatus != course.CourseDeleted &&
		(target.DistributionStatus == course.DistributionPaused ||
			target.DistributionStatus == course.DistributionBlocked)
}

// CanCopyPartnerCourse allows a manager to copy partner content without
// granting edit access to the original. Blocked and deleted sources cannot be
// copied.
func CanCopyPartnerCourse(actor Actor, target course.Course) bool {
	return validTarget(actor, target) &&
		isManager(actor) &&
		target.OwnerType == course.CourseOwnerPartner &&
		target.LifecycleStatus != course.CourseDeleted &&
		target.DistributionStatus != course.DistributionBlocked
}

// CanViewPartnerCourse allows owner/admin to inspect any partner course in the
// company and a partner to inspect their own. Deleted content is not exposed.
func CanViewPartnerCourse(actor Actor, target course.Course) bool {
	if !validTarget(actor, target) || target.OwnerType != course.CourseOwnerPartner ||
		target.LifecycleStatus == course.CourseDeleted {
		return false
	}

	return isManager(actor) || isOwnedPartnerCourse(actor, target)
}

// CanPreviewPartnerCourse is the privileged service-preview/test mode. It is
// intentionally available through pause, block and archive, but never for a
// deleted course. Preview does not grant edit rights on the original.
func CanPreviewPartnerCourse(actor Actor, target course.Course) bool {
	return validTarget(actor, target) &&
		isManager(actor) &&
		target.OwnerType == course.CourseOwnerPartner &&
		target.LifecycleStatus != course.CourseDeleted
}

// CanViewExternalLearner is evaluated against a course through which the
// learner is visible. Managers see company-wide history; partners need an
// enrollment on their own course. Historical visibility survives course
// deletion, while the course content itself remains hidden.
func CanViewExternalLearner(actor Actor, sourceCourse course.Course) bool {
	return canViewExternalCourseData(actor, sourceCourse)
}

// CanViewEnrollmentReport applies the same tenant and owner scope to reports.
func CanViewEnrollmentReport(actor Actor, sourceCourse course.Course) bool {
	return canViewExternalCourseData(actor, sourceCourse)
}

// CanCreatePersonalAccess reserves email-bound links for the partner who owns
// an active, unrestricted course and fixes the exact published version.
func CanCreatePersonalAccess(actor Actor, target course.Course, version courseversion.Snapshot) bool {
	_, versionErr := courseversion.Rehydrate(version)
	return validTarget(actor, target) && actor.Role == RolePartner &&
		isOwnedPartnerCourse(actor, target) &&
		target.LifecycleStatus == course.CourseActive &&
		target.DistributionStatus == course.DistributionActive &&
		courseversion.ID(target.CompanyID) == version.CompanyID &&
		courseversion.ID(target.ID) == version.CourseID &&
		version.Status == courseversion.StatusPublished && versionErr == nil
}

// CanManagePersonalAccess allows only its partner owner to extend, rotate,
// revoke, or explicitly start a repeat. Owner/admin reporting is read-only.
func CanManagePersonalAccess(actor Actor, target personalaccess.Snapshot) bool {
	return validActor(actor) && actor.Role == RolePartner &&
		personalaccess.ID(actor.CompanyID) == target.CompanyID &&
		personalaccess.ID(actor.UserID) == target.PartnerOwnerID && target.Validate() == nil
}

// CanViewPersonalAccess allows tenant managers a read-only report and the
// partner owner their own link. Employees and cross-tenant actors are denied.
func CanViewPersonalAccess(actor Actor, target personalaccess.Snapshot) bool {
	if !validActor(actor) || personalaccess.ID(actor.CompanyID) != target.CompanyID || target.Validate() != nil {
		return false
	}
	return isManager(actor) || (actor.Role == RolePartner && personalaccess.ID(actor.UserID) == target.PartnerOwnerID)
}

// CanCreateExternalCampaign applies the complete purpose/ownership matrix and
// fixes campaigns to an active, distributable course and published version.
// Company candidates belong to owner/admin; partner promotions belong only to
// the partner who owns the source course.
func CanCreateExternalCampaign(
	actor Actor,
	target course.Course,
	version courseversion.Snapshot,
	purpose externalcampaign.Purpose,
) bool {
	_, versionErr := courseversion.Rehydrate(version)
	if !validTarget(actor, target) || target.LifecycleStatus != course.CourseActive ||
		target.DistributionStatus != course.DistributionActive ||
		courseversion.ID(target.CompanyID) != version.CompanyID ||
		courseversion.ID(target.ID) != version.CourseID ||
		version.Status != courseversion.StatusPublished || versionErr != nil {
		return false
	}

	switch purpose {
	case externalcampaign.PurposeCompanyCandidate:
		return isManager(actor) && target.OwnerType == course.CourseOwnerCompany
	case externalcampaign.PurposePartnerPromo:
		return actor.Role == RolePartner && isOwnedPartnerCourse(actor, target)
	default:
		return false
	}
}

// CanManageExternalCampaign reserves campaign mutation for the corresponding
// owner boundary. Managers do not mutate a partner's campaign, and partners do
// not mutate company candidate campaigns.
func CanManageExternalCampaign(actor Actor, target externalcampaign.Snapshot) bool {
	if !validCampaignTarget(actor, target) {
		return false
	}
	switch target.OwnerType {
	case externalcampaign.OwnerCompany:
		return isManager(actor) && target.Purpose == externalcampaign.PurposeCompanyCandidate
	case externalcampaign.OwnerPartner:
		return actor.Role == RolePartner && target.OwnerUserID != nil &&
			externalcampaign.ID(actor.UserID) == *target.OwnerUserID &&
			target.Purpose == externalcampaign.PurposePartnerPromo
	default:
		return false
	}
}

// CanViewExternalCampaign gives owner/admin company-wide read access and a
// partner access only to campaigns on their own courses.
func CanViewExternalCampaign(actor Actor, target externalcampaign.Snapshot) bool {
	if !validCampaignTarget(actor, target) {
		return false
	}
	return isManager(actor) || (actor.Role == RolePartner &&
		target.OwnerType == externalcampaign.OwnerPartner && target.OwnerUserID != nil &&
		externalcampaign.ID(actor.UserID) == *target.OwnerUserID)
}

// CanViewExternalCampaignReport enforces backend report scoping rather than
// relying on response filtering in a client.
func CanViewExternalCampaignReport(actor Actor, target externalcampaign.Snapshot) bool {
	return CanViewExternalCampaign(actor, target)
}

// CanCreateCompanyTemplate reserves company template management for
// owner/admin.
func CanCreateCompanyTemplate(actor Actor) bool {
	return validActor(actor) && isManager(actor)
}

// CanInstantiateTemplate allows owner/admin to create a company course and a
// partner to create their own course. Template visibility is checked
// separately by the template aggregate.
func CanInstantiateTemplate(actor Actor) bool {
	return validActor(actor) && (isManager(actor) || actor.Role == RolePartner)
}

// CanViewCourseTemplate applies tenant and role visibility. Managers may
// inspect archived company templates; partners see only active templates and
// never see drafts through this root-level policy.
func CanViewCourseTemplate(actor Actor, target coursetemplate.Snapshot) bool {
	if !validTemplateTarget(actor, target) {
		return false
	}
	if isManager(actor) {
		return true
	}
	return actor.Role == RolePartner && target.LifecycleStatus == coursetemplate.LifecycleActive &&
		target.LatestPublishedVersionID != nil
}

// CanEditCompanyTemplate reserves company-template authoring for owner/admin.
// System and archived templates are immutable.
func CanEditCompanyTemplate(actor Actor, target coursetemplate.Snapshot) bool {
	return validTemplateTarget(actor, target) && isManager(actor) &&
		target.Type == coursetemplate.TypeCompany &&
		target.LifecycleStatus == coursetemplate.LifecycleActive
}

// CanPublishCompanyTemplate has the same object boundary as editing; version
// status and content validity are enforced by the version aggregate.
func CanPublishCompanyTemplate(actor Actor, target coursetemplate.Snapshot) bool {
	return CanEditCompanyTemplate(actor, target)
}

// CanArchiveCompanyTemplate never grants company users mutation rights over
// tenant-local system seeds.
func CanArchiveCompanyTemplate(actor Actor, target coursetemplate.Snapshot) bool {
	return CanEditCompanyTemplate(actor, target)
}

// CanInstantiateTemplateVersion requires an active template and a concrete
// immutable published version. Both owner/admin and partner may instantiate;
// the destination owner is forced by TemplateInstantiationOwner.
func CanInstantiateTemplateVersion(
	actor Actor,
	target coursetemplate.Snapshot,
	version coursetemplate.VersionSnapshot,
) bool {
	if !CanInstantiateTemplate(actor) || !validTemplateTarget(actor, target) ||
		target.LifecycleStatus != coursetemplate.LifecycleActive ||
		version.CompanyID != target.CompanyID || version.TemplateID != target.ID ||
		version.Status != coursetemplate.VersionPublished {
		return false
	}
	_, err := coursetemplate.RehydrateVersion(version)
	return err == nil
}

// TemplateInstantiationOwner prevents callers from choosing ownership:
// owner/admin always create a company course, partner always creates a course
// owned by themselves.
func TemplateInstantiationOwner(actor Actor) (course.OwnerType, *course.ID, bool) {
	if !validActor(actor) {
		return "", nil, false
	}
	if isManager(actor) {
		return course.CourseOwnerCompany, nil, true
	}
	if actor.Role == RolePartner {
		ownerID := actor.UserID
		return course.CourseOwnerPartner, &ownerID, true
	}
	return "", nil, false
}

func canMutateOwnedCourse(actor Actor, target course.Course) bool {
	if !validTarget(actor, target) {
		return false
	}

	switch target.OwnerType {
	case course.CourseOwnerCompany:
		return isManager(actor)
	case course.CourseOwnerPartner:
		return actor.Role == RolePartner && isOwnedPartnerCourse(actor, target)
	default:
		return false
	}
}

func canViewExternalCourseData(actor Actor, sourceCourse course.Course) bool {
	if !validTarget(actor, sourceCourse) {
		return false
	}
	if isManager(actor) {
		return true
	}

	return actor.Role == RolePartner && isOwnedPartnerCourse(actor, sourceCourse)
}

func validTarget(actor Actor, target course.Course) bool {
	return validActor(actor) && actor.CompanyID == target.CompanyID && target.Validate() == nil
}

func validTemplateTarget(actor Actor, target coursetemplate.Snapshot) bool {
	return validActor(actor) && coursetemplate.ID(actor.CompanyID) == target.CompanyID && target.Validate() == nil
}

func validCampaignTarget(actor Actor, target externalcampaign.Snapshot) bool {
	return validActor(actor) && externalcampaign.ID(actor.CompanyID) == target.CompanyID && target.Validate() == nil
}

func validActor(actor Actor) bool {
	if actor.UserID == "" || actor.CompanyID == "" {
		return false
	}

	switch actor.Role {
	case RoleOwner, RoleAdmin, RolePartner, RoleEmployee:
		return true
	default:
		return false
	}
}

func isManager(actor Actor) bool {
	return actor.Role == RoleOwner || actor.Role == RoleAdmin
}

func isOwnedPartnerCourse(actor Actor, target course.Course) bool {
	return target.OwnerType == course.CourseOwnerPartner &&
		target.OwnerUserID != nil &&
		*target.OwnerUserID == actor.UserID
}
