package course

import (
	"errors"
	"testing"
)

func TestValidateOwnership(t *testing.T) {
	t.Parallel()

	partnerID := ID("partner-1")
	emptyID := ID("")
	tests := []struct {
		name        string
		ownerType   OwnerType
		ownerUserID *ID
		wantErr     error
		wantMessage string
	}{
		{name: "company owner without user", ownerType: CourseOwnerCompany},
		{
			name:        "company owner with user",
			ownerType:   CourseOwnerCompany,
			ownerUserID: &partnerID,
			wantErr:     ErrCompanyOwnerUserForbidden,
			wantMessage: "У курса компании не может быть владельца-пользователя",
		},
		{name: "partner owner with user", ownerType: CourseOwnerPartner, ownerUserID: &partnerID},
		{
			name:        "partner owner without user",
			ownerType:   CourseOwnerPartner,
			wantErr:     ErrPartnerOwnerUserRequired,
			wantMessage: "Для партнёрского курса требуется владелец-партнёр",
		},
		{
			name:        "partner owner with empty user",
			ownerType:   CourseOwnerPartner,
			ownerUserID: &emptyID,
			wantErr:     ErrPartnerOwnerUserRequired,
			wantMessage: "Для партнёрского курса требуется владелец-партнёр",
		},
		{
			name:        "unknown owner type",
			ownerType:   OwnerType("unknown"),
			wantErr:     ErrUnknownOwnerType,
			wantMessage: "Неизвестный тип владельца курса",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := ValidateOwnership(test.ownerType, test.ownerUserID)
			assertError(t, got, test.wantErr, test.wantMessage)
		})
	}
}

func TestCourseValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		course      Course
		wantErr     error
		wantMessage string
	}{
		{name: "valid company course", course: companyCourse()},
		{name: "valid partner course", course: partnerCourse("partner-1")},
		{
			name:        "company is required",
			course:      Course{OwnerType: CourseOwnerCompany, LifecycleStatus: CourseActive, DistributionStatus: DistributionActive},
			wantErr:     ErrCompanyRequired,
			wantMessage: "Для курса требуется компания",
		},
		{
			name: "unknown lifecycle",
			course: func() Course {
				value := companyCourse()
				value.LifecycleStatus = LifecycleStatus("unknown")
				return value
			}(),
			wantErr:     ErrUnknownLifecycleStatus,
			wantMessage: "Неизвестное состояние жизненного цикла курса",
		},
		{
			name: "unknown distribution",
			course: func() Course {
				value := companyCourse()
				value.DistributionStatus = DistributionStatus("unknown")
				return value
			}(),
			wantErr:     ErrUnknownDistributionStatus,
			wantMessage: "Неизвестное состояние распространения курса",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertError(t, test.course.Validate(), test.wantErr, test.wantMessage)
		})
	}
}

func TestCourseEffectiveStatePriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		lifecycle    LifecycleStatus
		distribution DistributionStatus
		want         EffectiveState
	}{
		{name: "active and distributable", lifecycle: CourseActive, distribution: DistributionActive, want: EffectiveActive},
		{name: "paused active course", lifecycle: CourseActive, distribution: DistributionPaused, want: EffectivePaused},
		{name: "blocked active course", lifecycle: CourseActive, distribution: DistributionBlocked, want: EffectiveBlocked},
		{name: "archived active distribution", lifecycle: CourseArchived, distribution: DistributionActive, want: EffectiveArchived},
		{name: "archive overrides pause", lifecycle: CourseArchived, distribution: DistributionPaused, want: EffectiveArchived},
		{name: "block overrides archive", lifecycle: CourseArchived, distribution: DistributionBlocked, want: EffectiveBlocked},
		{name: "delete overrides active distribution", lifecycle: CourseDeleted, distribution: DistributionActive, want: EffectiveDeleted},
		{name: "delete overrides pause", lifecycle: CourseDeleted, distribution: DistributionPaused, want: EffectiveDeleted},
		{name: "delete overrides block", lifecycle: CourseDeleted, distribution: DistributionBlocked, want: EffectiveDeleted},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := companyCourse()
			value.LifecycleStatus = test.lifecycle
			value.DistributionStatus = test.distribution

			got, err := value.EffectiveState()
			if err != nil {
				t.Fatalf("EffectiveState() error = %v", err)
			}
			if got != test.want {
				t.Errorf("EffectiveState() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCourseEffectiveStateRejectsInvalidCourse(t *testing.T) {
	t.Parallel()

	value := companyCourse()
	value.DistributionStatus = DistributionStatus("unknown")
	got, err := value.EffectiveState()
	if got != "" {
		t.Errorf("EffectiveState() = %q, want empty state", got)
	}
	assertError(t, err, ErrUnknownDistributionStatus, "Неизвестное состояние распространения курса")
}

func TestCourseArchiveTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		from        LifecycleStatus
		want        LifecycleStatus
		wantErr     error
		wantMessage string
	}{
		{name: "active to archived", from: CourseActive, want: CourseArchived},
		{
			name:        "already archived",
			from:        CourseArchived,
			want:        CourseArchived,
			wantErr:     ErrCourseAlreadyArchived,
			wantMessage: "Курс уже находится в архиве",
		},
		{
			name:        "deleted is irreversible",
			from:        CourseDeleted,
			want:        CourseDeleted,
			wantErr:     ErrCourseDeleted,
			wantMessage: "Курс удалён",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := companyCourse()
			value.LifecycleStatus = test.from
			err := value.Archive()
			assertError(t, err, test.wantErr, test.wantMessage)
			if value.LifecycleStatus != test.want {
				t.Errorf("Archive() status = %q, want %q", value.LifecycleStatus, test.want)
			}
		})
	}
}

func TestCourseRestoreTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		from        LifecycleStatus
		want        LifecycleStatus
		wantErr     error
		wantMessage string
	}{
		{name: "archived to active", from: CourseArchived, want: CourseActive},
		{
			name:        "active is not archived",
			from:        CourseActive,
			want:        CourseActive,
			wantErr:     ErrCourseNotArchived,
			wantMessage: "Курс не находится в архиве",
		},
		{
			name:        "deleted is irreversible",
			from:        CourseDeleted,
			want:        CourseDeleted,
			wantErr:     ErrCourseDeleted,
			wantMessage: "Курс удалён",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := companyCourse()
			value.LifecycleStatus = test.from
			err := value.Restore()
			assertError(t, err, test.wantErr, test.wantMessage)
			if value.LifecycleStatus != test.want {
				t.Errorf("Restore() status = %q, want %q", value.LifecycleStatus, test.want)
			}
		})
	}
}

func TestCourseDeleteTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		from        LifecycleStatus
		wantErr     error
		wantMessage string
	}{
		{name: "active to deleted", from: CourseActive},
		{name: "archived to deleted", from: CourseArchived},
		{
			name:        "deleted is terminal",
			from:        CourseDeleted,
			wantErr:     ErrCourseDeleted,
			wantMessage: "Курс удалён",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := companyCourse()
			value.LifecycleStatus = test.from
			err := value.Delete()
			assertError(t, err, test.wantErr, test.wantMessage)
			if value.LifecycleStatus != CourseDeleted {
				t.Errorf("Delete() status = %q, want %q", value.LifecycleStatus, CourseDeleted)
			}
		})
	}
}

func TestDeletedCourseCannotTransition(t *testing.T) {
	t.Parallel()

	value := companyCourse()
	if err := value.Delete(); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	for name, transition := range map[string]func() error{
		"archive": value.Archive,
		"restore": value.Restore,
		"delete":  value.Delete,
	} {
		t.Run(name, func(t *testing.T) {
			err := transition()
			assertError(t, err, ErrCourseDeleted, "Курс удалён")
			if value.LifecycleStatus != CourseDeleted {
				t.Errorf("status = %q, want %q", value.LifecycleStatus, CourseDeleted)
			}
		})
	}
}

func companyCourse() Course {
	return Course{
		ID:                 "course-1",
		CompanyID:          "company-1",
		OwnerType:          CourseOwnerCompany,
		LifecycleStatus:    CourseActive,
		DistributionStatus: DistributionActive,
	}
}

func partnerCourse(ownerID ID) Course {
	return Course{
		ID:                 "course-1",
		CompanyID:          "company-1",
		OwnerType:          CourseOwnerPartner,
		OwnerUserID:        &ownerID,
		LifecycleStatus:    CourseActive,
		DistributionStatus: DistributionActive,
	}
}

func assertError(t *testing.T, got, want error, wantMessage string) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
	if got != nil && got.Error() != wantMessage {
		t.Errorf("error message = %q, want %q", got.Error(), wantMessage)
	}
}
