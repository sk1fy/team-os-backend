package restriction

import (
	"errors"
	"testing"
	"time"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
)

var restrictionNow = time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)

func TestPauseBlockPrecedenceAndResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		operations  []operation
		want        course.DistributionStatus
		wantHistory int
	}{
		{name: "empty is active", want: course.DistributionActive},
		{
			name: "pause",
			operations: []operation{{apply: &ApplyParams{
				ID: "pause-1", Kind: KindPause, Reason: "Нужно исправить урок", AppliedByID: "admin-1", AppliedAt: restrictionNow,
			}}},
			want: course.DistributionPaused, wantHistory: 1,
		},
		{
			name: "block takes precedence over pause",
			operations: []operation{
				{apply: &ApplyParams{ID: "pause-1", Kind: KindPause, Reason: "Проверка", AppliedByID: "admin-1", AppliedAt: restrictionNow}},
				{apply: &ApplyParams{ID: "block-1", Kind: KindBlock, Reason: "Экстренная блокировка", AppliedByID: "owner-1", AppliedAt: restrictionNow.Add(time.Minute)}},
			},
			want: course.DistributionBlocked, wantHistory: 2,
		},
		{
			name: "resolving block reveals pause",
			operations: []operation{
				{apply: &ApplyParams{ID: "pause-1", Kind: KindPause, Reason: "Проверка", AppliedByID: "admin-1", AppliedAt: restrictionNow}},
				{apply: &ApplyParams{ID: "block-1", Kind: KindBlock, Reason: "Экстренная блокировка", AppliedByID: "owner-1", AppliedAt: restrictionNow.Add(time.Minute)}},
				{resolveID: "block-1", at: restrictionNow.Add(2 * time.Minute)},
			},
			want: course.DistributionPaused, wantHistory: 2,
		},
		{
			name: "resolving all restrictions restores active",
			operations: []operation{
				{apply: &ApplyParams{ID: "pause-1", Kind: KindPause, Reason: "Проверка", AppliedByID: "admin-1", AppliedAt: restrictionNow}},
				{apply: &ApplyParams{ID: "block-1", Kind: KindBlock, Reason: "Экстренная блокировка", AppliedByID: "owner-1", AppliedAt: restrictionNow.Add(time.Minute)}},
				{resolveID: "block-1", at: restrictionNow.Add(2 * time.Minute)},
				{resolveID: "pause-1", at: restrictionNow.Add(3 * time.Minute)},
			},
			want: course.DistributionActive, wantHistory: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			set, err := Rehydrate("company-1", "course-1", nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, operation := range test.operations {
				if operation.apply != nil {
					if _, err := set.Apply(*operation.apply); err != nil {
						t.Fatal(err)
					}
					continue
				}
				if _, err := set.Resolve(operation.resolveID, "admin-2", operation.at); err != nil {
					t.Fatal(err)
				}
			}
			if got := set.EffectiveStatus(); got != test.want {
				t.Errorf("EffectiveStatus() = %q, want %q", got, test.want)
			}
			if got := len(set.Snapshot()); got != test.wantHistory {
				t.Errorf("history length = %d, want %d", got, test.wantHistory)
			}
		})
	}
}

type operation struct {
	apply     *ApplyParams
	resolveID ID
	at        time.Time
}

func TestRestrictionRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(*Set)
		action  func(*Set) error
		wantErr error
	}{
		{
			name: "reason is required",
			action: func(set *Set) error {
				_, err := set.Apply(ApplyParams{ID: "pause-1", Kind: KindPause, AppliedByID: "admin-1", AppliedAt: restrictionNow})
				return err
			},
			wantErr: ErrReasonRequired,
		},
		{
			name: "unknown kind",
			action: func(set *Set) error {
				_, err := set.Apply(ApplyParams{ID: "r-1", Kind: "slow", Reason: "Причина", AppliedByID: "admin-1", AppliedAt: restrictionNow})
				return err
			},
			wantErr: ErrUnknownKind,
		},
		{
			name: "duplicate active pause",
			prepare: func(set *Set) {
				_, _ = set.Apply(ApplyParams{ID: "pause-1", Kind: KindPause, Reason: "Причина", AppliedByID: "admin-1", AppliedAt: restrictionNow})
			},
			action: func(set *Set) error {
				_, err := set.Apply(ApplyParams{ID: "pause-2", Kind: KindPause, Reason: "Другая", AppliedByID: "admin-1", AppliedAt: restrictionNow.Add(time.Minute)})
				return err
			},
			wantErr: ErrDuplicateActiveKind,
		},
		{
			name: "unknown restriction",
			action: func(set *Set) error {
				_, err := set.Resolve("missing", "admin-1", restrictionNow)
				return err
			},
			wantErr: ErrRestrictionNotFound,
		},
		{
			name: "resolve before apply",
			prepare: func(set *Set) {
				_, _ = set.Apply(ApplyParams{ID: "pause-1", Kind: KindPause, Reason: "Причина", AppliedByID: "admin-1", AppliedAt: restrictionNow})
			},
			action: func(set *Set) error {
				_, err := set.Resolve("pause-1", "admin-1", restrictionNow.Add(-time.Second))
				return err
			},
			wantErr: ErrResolvedBeforeApplied,
		},
		{
			name: "cannot resolve twice",
			prepare: func(set *Set) {
				_, _ = set.Apply(ApplyParams{ID: "pause-1", Kind: KindPause, Reason: "Причина", AppliedByID: "admin-1", AppliedAt: restrictionNow})
				_, _ = set.Resolve("pause-1", "admin-1", restrictionNow.Add(time.Minute))
			},
			action: func(set *Set) error {
				_, err := set.Resolve("pause-1", "admin-1", restrictionNow.Add(2*time.Minute))
				return err
			},
			wantErr: ErrRestrictionResolved,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			set, err := Rehydrate("company-1", "course-1", nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.prepare != nil {
				test.prepare(set)
			}
			if err := test.action(set); !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestSnapshotIsDefensive(t *testing.T) {
	t.Parallel()

	set, err := Rehydrate("company-1", "course-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = set.Apply(ApplyParams{ID: "pause-1", Kind: KindPause, Reason: "Причина", AppliedByID: "admin-1", AppliedAt: restrictionNow})
	_, _ = set.Resolve("pause-1", "admin-2", restrictionNow.Add(time.Minute))

	snapshot := set.Snapshot()
	*snapshot[0].ResolvedByID = "attacker"
	*snapshot[0].ResolvedAt = restrictionNow.Add(-time.Hour)
	if got := set.Snapshot()[0]; *got.ResolvedByID != "admin-2" || !got.ResolvedAt.Equal(restrictionNow.Add(time.Minute)) {
		t.Fatalf("snapshot mutated aggregate: %#v", got)
	}
}
