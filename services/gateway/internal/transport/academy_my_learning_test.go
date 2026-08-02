package transport

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func TestBuildMyLearningSummarySelectsOnlyAvailableInProgressEnrollment(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	values := []api.EnrollmentSummary{
		enrollmentSummaryForTest(api.EnrollmentProgressStatusCompleted, api.EnrollmentAccessStatusActive),
		enrollmentSummaryForTest(api.EnrollmentProgressStatusNotStarted, api.EnrollmentAccessStatusReady),
		enrollmentSummaryForTest(api.EnrollmentProgressStatusInProgress, api.EnrollmentAccessStatusClosed),
		enrollmentSummaryForTest(api.EnrollmentProgressStatusInProgress, api.EnrollmentAccessStatusActive),
	}

	result := buildMyLearningSummary(values, now)
	if result.ContinueEnrollment == nil || result.ContinueEnrollment.Id != values[3].Id {
		t.Fatalf("continue enrollment = %+v, want %s", result.ContinueEnrollment, values[3].Id)
	}
	if result.Stats.Completed != 1 || result.Stats.InProgress != 2 || result.Stats.TotalAssigned != 4 {
		t.Fatalf("stats = %+v", result.Stats)
	}
}

func TestBuildMyLearningSummaryDoesNotFallbackToUnavailableEnrollment(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		values []api.EnrollmentSummary
	}{
		{name: "completed", values: []api.EnrollmentSummary{
			enrollmentSummaryForTest(api.EnrollmentProgressStatusCompleted, api.EnrollmentAccessStatusActive),
		}},
		{name: "not started", values: []api.EnrollmentSummary{
			enrollmentSummaryForTest(api.EnrollmentProgressStatusNotStarted, api.EnrollmentAccessStatusReady),
		}},
		{name: "closed in progress", values: []api.EnrollmentSummary{
			enrollmentSummaryForTest(api.EnrollmentProgressStatusInProgress, api.EnrollmentAccessStatusClosed),
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			result := buildMyLearningSummary(testCase.values, time.Now())
			if result.ContinueEnrollment != nil {
				t.Fatalf("continue enrollment = %+v, want nil", result.ContinueEnrollment)
			}
		})
	}
}

func enrollmentSummaryForTest(
	progressStatus api.EnrollmentProgressStatus,
	accessStatus api.EnrollmentAccessStatus,
) api.EnrollmentSummary {
	return api.EnrollmentSummary{
		Id: uuid.New(), CourseId: uuid.New(), CourseVersionId: uuid.New(), CourseTitle: "Курс",
		LearnerType:    api.EnrollmentSummaryLearnerTypeUser,
		ProgressStatus: progressStatus, AccessStatus: accessStatus,
	}
}
