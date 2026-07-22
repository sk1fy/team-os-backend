package course

import (
	"errors"
	"reflect"
	"testing"
)

func TestPlanLifecycleEffects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		root       Course
		transition LifecycleTransition
		wantStatus LifecycleStatus
		want       LifecycleEffects
	}{
		{
			name: "archive freezes unfinished external runs and preserves history",
			root: companyCourse(), transition: TransitionArchive, wantStatus: CourseArchived,
			want: LifecycleEffects{
				DisableNewDistribution: true, DisablePendingActivation: true,
				FreezeUnfinishedExternal: true, HideIncompleteContent: true, PreserveHistory: true,
			},
		},
		{
			name: "restore does not revive frozen runs",
			root: func() Course {
				value := companyCourse()
				value.LifecycleStatus = CourseArchived
				return value
			}(),
			transition: TransitionRestore, wantStatus: CourseActive,
			want: LifecycleEffects{PreserveHistory: true, ReactivateFrozenEnrollment: false},
		},
		{
			name: "delete closes access but preserves reporting history",
			root: partnerCourse("partner-1"), transition: TransitionDelete, wantStatus: CourseDeleted,
			want: LifecycleEffects{
				DisableNewDistribution: true, DisablePendingActivation: true,
				CloseLinksAndCampaigns: true, CloseEnrollments: true,
				HideAllLearnerContent: true, PreserveHistory: true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			updated, effects, err := PlanLifecycle(test.root, test.transition)
			if err != nil {
				t.Fatal(err)
			}
			if updated.LifecycleStatus != test.wantStatus {
				t.Errorf("status = %q, want %q", updated.LifecycleStatus, test.wantStatus)
			}
			if !reflect.DeepEqual(effects, test.want) {
				t.Errorf("effects = %#v, want %#v", effects, test.want)
			}
		})
	}
}

func TestPlanLifecycleFailureDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	deleted := companyCourse()
	deleted.LifecycleStatus = CourseDeleted
	tests := []struct {
		name       string
		root       Course
		transition LifecycleTransition
		wantErr    error
	}{
		{name: "cannot restore active", root: companyCourse(), transition: TransitionRestore, wantErr: ErrCourseNotArchived},
		{name: "deleted is terminal", root: deleted, transition: TransitionArchive, wantErr: ErrCourseDeleted},
		{name: "unknown transition", root: companyCourse(), transition: "purge", wantErr: ErrUnknownLifecycleTransition},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			updated, effects, err := PlanLifecycle(test.root, test.transition)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if !reflect.DeepEqual(updated, test.root) || effects != (LifecycleEffects{}) {
				t.Fatalf("failed transition changed state: %#v %#v", updated, effects)
			}
		})
	}
}
