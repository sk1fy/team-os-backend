package authorization

import (
	"testing"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
)

func TestCreatePermissionMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		check           func(Actor) bool
		ownerAllowed    bool
		adminAllowed    bool
		partnerAllowed  bool
		employeeAllowed bool
	}{
		{
			name:         "create company course",
			check:        CanCreateCompanyCourse,
			ownerAllowed: true,
			adminAllowed: true,
		},
		{
			name:           "create partner course",
			check:          CanCreatePartnerCourse,
			partnerAllowed: true,
		},
		{
			name:         "create company template",
			check:        CanCreateCompanyTemplate,
			ownerAllowed: true,
			adminAllowed: true,
		},
		{
			name:           "instantiate template",
			check:          CanInstantiateTemplate,
			ownerAllowed:   true,
			adminAllowed:   true,
			partnerAllowed: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertRoleMatrix(t, test.check, roleMatrix{
				owner:    test.ownerAllowed,
				admin:    test.adminAllowed,
				partner:  test.partnerAllowed,
				employee: test.employeeAllowed,
			})
		})
	}
}

func TestCompanyCoursePermissionMatrix(t *testing.T) {
	t.Parallel()

	target := companyCourse()
	tests := []struct {
		name  string
		check func(Actor, course.Course) bool
		want  roleMatrix
	}{
		{name: "edit", check: CanEditCompanyCourse, want: managersOnly()},
		{name: "publish", check: CanPublishCompanyCourse, want: managersOnly()},
		{name: "assign", check: CanAssignCompanyCourse, want: managersOnly()},
		{name: "archive", check: CanArchiveCourse, want: managersOnly()},
		{name: "delete", check: CanDeleteCourse, want: managersOnly()},
		{name: "pause partner distribution", check: CanPausePartnerDistribution, want: roleMatrix{}},
		{name: "block partner course", check: CanBlockPartnerCourse, want: roleMatrix{}},
		{name: "copy partner course", check: CanCopyPartnerCourse, want: roleMatrix{}},
		{name: "view partner course", check: CanViewPartnerCourse, want: roleMatrix{}},
		{name: "view external learner", check: CanViewExternalLearner, want: managersOnly()},
		{name: "view enrollment report", check: CanViewEnrollmentReport, want: managersOnly()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertCourseRoleMatrix(t, test.check, target, test.want)
		})
	}
}

func TestOwnPartnerCoursePermissionMatrix(t *testing.T) {
	t.Parallel()

	target := partnerCourse("partner-1")
	tests := []struct {
		name  string
		check func(Actor, course.Course) bool
		want  roleMatrix
	}{
		{name: "edit company course", check: CanEditCompanyCourse, want: roleMatrix{}},
		{name: "publish company course", check: CanPublishCompanyCourse, want: roleMatrix{}},
		{name: "assign company course", check: CanAssignCompanyCourse, want: roleMatrix{}},
		{name: "edit own partner course", check: CanEditPartnerCourse, want: partnerOnly()},
		{name: "publish own partner course", check: CanPublishPartnerCourse, want: partnerOnly()},
		{name: "archive own partner course", check: CanArchiveCourse, want: partnerOnly()},
		{name: "delete own partner course", check: CanDeleteCourse, want: partnerOnly()},
		{name: "pause partner distribution", check: CanPausePartnerDistribution, want: managersOnly()},
		{name: "block partner course", check: CanBlockPartnerCourse, want: managersOnly()},
		{name: "copy partner course", check: CanCopyPartnerCourse, want: managersOnly()},
		{name: "view partner course", check: CanViewPartnerCourse, want: roleMatrix{owner: true, admin: true, partner: true}},
		{name: "view external learner", check: CanViewExternalLearner, want: roleMatrix{owner: true, admin: true, partner: true}},
		{name: "view enrollment report", check: CanViewEnrollmentReport, want: roleMatrix{owner: true, admin: true, partner: true}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertCourseRoleMatrix(t, test.check, target, test.want)
		})
	}
}

func TestOtherPartnerCoursePermissionMatrix(t *testing.T) {
	t.Parallel()

	target := partnerCourse("partner-2")
	tests := []struct {
		name  string
		check func(Actor, course.Course) bool
		want  roleMatrix
	}{
		{name: "edit", check: CanEditPartnerCourse, want: roleMatrix{}},
		{name: "publish", check: CanPublishPartnerCourse, want: roleMatrix{}},
		{name: "archive", check: CanArchiveCourse, want: roleMatrix{}},
		{name: "delete", check: CanDeleteCourse, want: roleMatrix{}},
		{name: "pause", check: CanPausePartnerDistribution, want: managersOnly()},
		{name: "block", check: CanBlockPartnerCourse, want: managersOnly()},
		{name: "copy", check: CanCopyPartnerCourse, want: managersOnly()},
		{name: "view partner course", check: CanViewPartnerCourse, want: managersOnly()},
		{name: "view external learner", check: CanViewExternalLearner, want: managersOnly()},
		{name: "view enrollment report", check: CanViewEnrollmentReport, want: managersOnly()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertCourseRoleMatrix(t, test.check, target, test.want)
		})
	}
}

func TestCoursePolicyRejectsAnotherCompany(t *testing.T) {
	t.Parallel()

	targets := []course.Course{companyCourse(), partnerCourse("partner-1")}
	checks := map[string]func(Actor, course.Course) bool{
		"edit company":          CanEditCompanyCourse,
		"publish company":       CanPublishCompanyCourse,
		"assign company":        CanAssignCompanyCourse,
		"edit partner":          CanEditPartnerCourse,
		"publish partner":       CanPublishPartnerCourse,
		"archive":               CanArchiveCourse,
		"delete":                CanDeleteCourse,
		"pause":                 CanPausePartnerDistribution,
		"block":                 CanBlockPartnerCourse,
		"copy":                  CanCopyPartnerCourse,
		"view partner":          CanViewPartnerCourse,
		"view external learner": CanViewExternalLearner,
		"view report":           CanViewEnrollmentReport,
	}

	for _, target := range targets {
		for name, check := range checks {
			t.Run(name+"/"+string(target.OwnerType), func(t *testing.T) {
				for _, actor := range knownActors() {
					actor.CompanyID = "company-2"
					if check(actor, target) {
						t.Errorf("%s allowed actor from another company with role %q", name, actor.Role)
					}
				}
			})
		}
	}
}

func TestCoursePolicyStateGuards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		actor  Actor
		target course.Course
		check  func(Actor, course.Course) bool
		want   bool
	}{
		{name: "archived company course remains editable", actor: actor(RoleAdmin), target: withLifecycle(companyCourse(), course.CourseArchived), check: CanEditCompanyCourse, want: true},
		{name: "archived company course remains publishable", actor: actor(RoleAdmin), target: withLifecycle(companyCourse(), course.CourseArchived), check: CanPublishCompanyCourse, want: true},
		{name: "archived company course cannot be assigned", actor: actor(RoleAdmin), target: withLifecycle(companyCourse(), course.CourseArchived), check: CanAssignCompanyCourse},
		{name: "archived course cannot be archived again", actor: actor(RoleAdmin), target: withLifecycle(companyCourse(), course.CourseArchived), check: CanArchiveCourse},
		{name: "archived company course can be deleted", actor: actor(RoleAdmin), target: withLifecycle(companyCourse(), course.CourseArchived), check: CanDeleteCourse, want: true},
		{name: "deleted company course cannot be edited", actor: actor(RoleOwner), target: withLifecycle(companyCourse(), course.CourseDeleted), check: CanEditCompanyCourse},
		{name: "deleted company course cannot be deleted again", actor: actor(RoleOwner), target: withLifecycle(companyCourse(), course.CourseDeleted), check: CanDeleteCourse},
		{name: "deleted report remains visible to manager", actor: actor(RoleOwner), target: withLifecycle(companyCourse(), course.CourseDeleted), check: CanViewEnrollmentReport, want: true},
		{name: "paused own partner course remains editable", actor: actor(RolePartner), target: withDistribution(partnerCourse("partner-1"), course.DistributionPaused), check: CanEditPartnerCourse, want: true},
		{name: "paused own partner course remains publishable", actor: actor(RolePartner), target: withDistribution(partnerCourse("partner-1"), course.DistributionPaused), check: CanPublishPartnerCourse, want: true},
		{name: "blocked own partner course remains editable", actor: actor(RolePartner), target: withDistribution(partnerCourse("partner-1"), course.DistributionBlocked), check: CanEditPartnerCourse, want: true},
		{name: "blocked own partner course cannot be published", actor: actor(RolePartner), target: withDistribution(partnerCourse("partner-1"), course.DistributionBlocked), check: CanPublishPartnerCourse},
		{name: "paused partner course cannot be paused twice", actor: actor(RoleAdmin), target: withDistribution(partnerCourse("partner-2"), course.DistributionPaused), check: CanPausePartnerDistribution},
		{name: "paused partner course can be blocked", actor: actor(RoleAdmin), target: withDistribution(partnerCourse("partner-2"), course.DistributionPaused), check: CanBlockPartnerCourse, want: true},
		{name: "blocked partner course cannot be copied", actor: actor(RoleAdmin), target: withDistribution(partnerCourse("partner-2"), course.DistributionBlocked), check: CanCopyPartnerCourse},
		{name: "deleted partner report remains visible to owner", actor: actor(RoleOwner), target: withLifecycle(partnerCourse("partner-2"), course.CourseDeleted), check: CanViewEnrollmentReport, want: true},
		{name: "deleted own partner report remains visible", actor: actor(RolePartner), target: withLifecycle(partnerCourse("partner-1"), course.CourseDeleted), check: CanViewEnrollmentReport, want: true},
		{name: "deleted partner content is not visible", actor: actor(RoleOwner), target: withLifecycle(partnerCourse("partner-2"), course.CourseDeleted), check: CanViewPartnerCourse},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.check(test.actor, test.target); got != test.want {
				t.Errorf("permission = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCoursePolicyRejectsInvalidActorAndTarget(t *testing.T) {
	t.Parallel()

	invalidActors := []Actor{
		{},
		{UserID: "owner-1", CompanyID: "company-1", Role: Role("unknown")},
		{UserID: "", CompanyID: "company-1", Role: RoleOwner},
		{UserID: "owner-1", CompanyID: "", Role: RoleOwner},
	}
	for _, invalid := range invalidActors {
		if CanCreateCompanyCourse(invalid) || CanCreatePartnerCourse(invalid) ||
			CanCreateCompanyTemplate(invalid) || CanInstantiateTemplate(invalid) {
			t.Errorf("create permission allowed invalid actor: %+v", invalid)
		}
	}

	invalidTarget := companyCourse()
	invalidTarget.OwnerType = course.OwnerType("unknown")
	checks := []func(Actor, course.Course) bool{
		CanEditCompanyCourse,
		CanPublishCompanyCourse,
		CanAssignCompanyCourse,
		CanArchiveCourse,
		CanDeleteCourse,
		CanViewExternalLearner,
		CanViewEnrollmentReport,
	}
	for _, check := range checks {
		if check(actor(RoleOwner), invalidTarget) {
			t.Fatal("permission allowed invalid course target")
		}
	}
}

type roleMatrix struct {
	owner    bool
	admin    bool
	partner  bool
	employee bool
}

func managersOnly() roleMatrix {
	return roleMatrix{owner: true, admin: true}
}

func partnerOnly() roleMatrix {
	return roleMatrix{partner: true}
}

func assertCourseRoleMatrix(
	t *testing.T,
	check func(Actor, course.Course) bool,
	target course.Course,
	want roleMatrix,
) {
	t.Helper()
	assertRoleMatrix(t, func(value Actor) bool { return check(value, target) }, want)
}

func assertRoleMatrix(t *testing.T, check func(Actor) bool, want roleMatrix) {
	t.Helper()
	tests := []struct {
		name  string
		actor Actor
		want  bool
	}{
		{name: "owner", actor: actor(RoleOwner), want: want.owner},
		{name: "admin", actor: actor(RoleAdmin), want: want.admin},
		{name: "partner", actor: actor(RolePartner), want: want.partner},
		{name: "employee", actor: actor(RoleEmployee), want: want.employee},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Helper()
			if got := check(test.actor); got != test.want {
				t.Errorf("permission for role %q = %v, want %v", test.actor.Role, got, test.want)
			}
		})
	}
}

func knownActors() []Actor {
	return []Actor{actor(RoleOwner), actor(RoleAdmin), actor(RolePartner), actor(RoleEmployee)}
}

func actor(role Role) Actor {
	userID := course.ID(string(role) + "-1")
	return Actor{UserID: userID, CompanyID: "company-1", Role: role}
}

func companyCourse() course.Course {
	return course.Course{
		ID:                 "company-course-1",
		CompanyID:          "company-1",
		OwnerType:          course.CourseOwnerCompany,
		LifecycleStatus:    course.CourseActive,
		DistributionStatus: course.DistributionActive,
	}
}

func partnerCourse(ownerID course.ID) course.Course {
	return course.Course{
		ID:                 "partner-course-1",
		CompanyID:          "company-1",
		OwnerType:          course.CourseOwnerPartner,
		OwnerUserID:        &ownerID,
		LifecycleStatus:    course.CourseActive,
		DistributionStatus: course.DistributionActive,
	}
}

func withLifecycle(value course.Course, status course.LifecycleStatus) course.Course {
	value.LifecycleStatus = status
	return value
}

func withDistribution(value course.Course, status course.DistributionStatus) course.Course {
	value.DistributionStatus = status
	return value
}
