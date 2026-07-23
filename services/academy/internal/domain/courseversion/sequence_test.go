package courseversion

import (
	"sync"
	"testing"
)

func TestValidateVersionSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		versions []VersionRef
		wantErr  error
	}{
		{name: "empty"},
		{name: "published sequence", versions: []VersionRef{{1, StatusPublished}, {2, StatusPublished}}},
		{name: "retired sequence", versions: []VersionRef{{1, StatusRetired}, {2, StatusPublished}}},
		{name: "one latest draft", versions: []VersionRef{{1, StatusPublished}, {2, StatusDraft}}},
		{name: "unsorted input", versions: []VersionRef{{2, StatusDraft}, {1, StatusPublished}}},
		{name: "zero number", versions: []VersionRef{{0, StatusDraft}}, wantErr: ErrVersionNumberInvalid},
		{name: "negative number", versions: []VersionRef{{-1, StatusDraft}}, wantErr: ErrVersionNumberInvalid},
		{name: "duplicate number", versions: []VersionRef{{1, StatusPublished}, {1, StatusDraft}}, wantErr: ErrDuplicateVersionNumber},
		{name: "starts after one", versions: []VersionRef{{2, StatusPublished}}, wantErr: ErrVersionNumberGap},
		{name: "gap", versions: []VersionRef{{1, StatusPublished}, {3, StatusDraft}}, wantErr: ErrVersionNumberGap},
		{name: "multiple drafts", versions: []VersionRef{{1, StatusDraft}, {2, StatusDraft}}, wantErr: ErrMultipleDrafts},
		{name: "draft is not latest", versions: []VersionRef{{1, StatusDraft}, {2, StatusPublished}}, wantErr: ErrDraftMustBeLatest},
		{name: "unknown status", versions: []VersionRef{{1, "unknown"}}, wantErr: ErrUnknownStatus},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertErrorIs(t, ValidateVersionSet(test.versions), test.wantErr)
		})
	}
}

func TestPlanNextDraft(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		versions       []VersionRef
		expectedLatest int
		want           int
		wantErr        error
	}{
		{name: "first version", expectedLatest: 0, want: 1},
		{name: "after published", versions: []VersionRef{{1, StatusPublished}}, expectedLatest: 1, want: 2},
		{name: "after retired", versions: []VersionRef{{1, StatusRetired}, {2, StatusPublished}}, expectedLatest: 2, want: 3},
		{name: "draft exists", versions: []VersionRef{{1, StatusPublished}, {2, StatusDraft}}, expectedLatest: 2, wantErr: ErrDraftAlreadyExists},
		{name: "stale empty snapshot token", expectedLatest: 1, wantErr: ErrVersionNumberConflict},
		{name: "stale published snapshot token", versions: []VersionRef{{1, StatusPublished}, {2, StatusPublished}}, expectedLatest: 1, wantErr: ErrVersionNumberConflict},
		{name: "invalid set wins", versions: []VersionRef{{1, StatusPublished}, {1, StatusPublished}}, expectedLatest: 1, wantErr: ErrDuplicateVersionNumber},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := PlanNextDraft(test.versions, test.expectedLatest)
			assertErrorIs(t, err, test.wantErr)
			if got != test.want {
				t.Fatalf("PlanNextDraft() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestPlanNextDraftConcurrentReaders(t *testing.T) {
	t.Parallel()

	// The domain planner is side-effect free and safe for concurrent readers.
	// Both proposals deliberately carry the same number; the application must
	// serialize them with the documented course lock and database uniqueness.
	versions := []VersionRef{{1, StatusPublished}}
	const workers = 64
	results := make(chan int, workers)
	errorsChannel := make(chan error, workers)
	var waitGroup sync.WaitGroup
	waitGroup.Add(workers)
	for range workers {
		go func() {
			defer waitGroup.Done()
			number, err := PlanNextDraft(versions, 1)
			results <- number
			errorsChannel <- err
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("PlanNextDraft() error = %v", err)
		}
	}
	for number := range results {
		if number != 2 {
			t.Fatalf("PlanNextDraft() = %d, want 2", number)
		}
	}
}
