package courseversion

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestNewDraftValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*NewDraftParams)
		wantErr error
	}{
		{name: "valid"},
		{name: "version id", mutate: func(value *NewDraftParams) { value.ID = "" }, wantErr: ErrVersionIDRequired},
		{name: "company", mutate: func(value *NewDraftParams) { value.CompanyID = "" }, wantErr: ErrCompanyRequired},
		{name: "course", mutate: func(value *NewDraftParams) { value.CourseID = "" }, wantErr: ErrCourseRequired},
		{name: "creator", mutate: func(value *NewDraftParams) { value.CreatedByID = "" }, wantErr: ErrCreatorRequired},
		{name: "created at", mutate: func(value *NewDraftParams) { value.CreatedAt = time.Time{} }, wantErr: ErrCreatedAtRequired},
		{name: "zero number", mutate: func(value *NewDraftParams) { value.Number = 0 }, wantErr: ErrVersionNumberInvalid},
		{name: "negative number", mutate: func(value *NewDraftParams) { value.Number = -1 }, wantErr: ErrVersionNumberInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			params := validDraftParams()
			if test.mutate != nil {
				test.mutate(&params)
			}
			version, err := NewDraft(params)
			assertErrorIs(t, err, test.wantErr)
			if test.wantErr == nil && version.Snapshot().Status != StatusDraft {
				t.Fatalf("status = %q, want %q", version.Snapshot().Status, StatusDraft)
			}
		})
	}
}

func TestRehydratePublicationMetadata(t *testing.T) {
	t.Parallel()

	publisher := ID("actor-1")
	publishedAt := testNow().Add(time.Hour)
	tests := []struct {
		name    string
		mutate  func(*Snapshot)
		wantErr error
	}{
		{name: "draft"},
		{
			name: "published",
			mutate: func(value *Snapshot) {
				value.Status = StatusPublished
				value.PublishedByID = &publisher
				value.PublishedAt = &publishedAt
				value.ContentHash = "hash"
			},
		},
		{name: "unknown status", mutate: func(value *Snapshot) { value.Status = "unknown" }, wantErr: ErrUnknownStatus},
		{
			name:    "draft with published actor",
			mutate:  func(value *Snapshot) { value.PublishedByID = &publisher },
			wantErr: ErrDraftPublishedMetadata,
		},
		{
			name:    "draft with publication date",
			mutate:  func(value *Snapshot) { value.PublishedAt = &publishedAt },
			wantErr: ErrDraftPublishedMetadata,
		},
		{
			name:    "draft with content hash",
			mutate:  func(value *Snapshot) { value.ContentHash = "hash" },
			wantErr: ErrDraftPublishedMetadata,
		},
		{
			name: "published without actor",
			mutate: func(value *Snapshot) {
				value.Status = StatusPublished
				value.PublishedAt = &publishedAt
				value.ContentHash = "hash"
			},
			wantErr: ErrPublishedMetadataRequired,
		},
		{
			name: "published without date",
			mutate: func(value *Snapshot) {
				value.Status = StatusPublished
				value.PublishedByID = &publisher
				value.ContentHash = "hash"
			},
			wantErr: ErrPublishedMetadataRequired,
		},
		{
			name: "published without hash",
			mutate: func(value *Snapshot) {
				value.Status = StatusPublished
				value.PublishedByID = &publisher
				value.PublishedAt = &publishedAt
			},
			wantErr: ErrPublishedMetadataRequired,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := draftSnapshot()
			if test.mutate != nil {
				test.mutate(&snapshot)
			}
			_, err := Rehydrate(snapshot)
			assertErrorIs(t, err, test.wantErr)
		})
	}
}

func TestVersionDefinitionIsDefensivelyCopied(t *testing.T) {
	t.Parallel()

	params := validDraftParams()
	version, err := NewDraft(params)
	if err != nil {
		t.Fatalf("NewDraft() error = %v", err)
	}

	params.Definition.Title = "Изменено снаружи"
	params.Definition.Lessons[0].Content[0] = 'x'
	params.Definition.Lessons[0].Quiz.Questions[0].Options[0].Correct = false
	if got := version.Snapshot(); got.Definition.Title != "Курс" || got.Definition.Lessons[0].Content[0] == 'x' ||
		!got.Definition.Lessons[0].Quiz.Questions[0].Options[0].Correct {
		t.Fatalf("constructor did not defensively copy definition: %#v", got.Definition)
	}

	first := version.Snapshot()
	first.Definition.Title = "Изменено через снимок"
	first.Definition.Lessons[0].Content[0] = 'y'
	first.Definition.Lessons[0].Quiz.Questions[0].Options[0].Correct = false
	second := version.Snapshot()
	if second.Definition.Title != "Курс" || second.Definition.Lessons[0].Content[0] == 'y' ||
		!second.Definition.Lessons[0].Quiz.Questions[0].Options[0].Correct {
		t.Fatalf("Snapshot() exposed internal state: %#v", second.Definition)
	}
}

func TestReplaceDraftAndImmutablePublishedContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		prepare    func(*testing.T, *Version)
		wantErr    error
		wantStatus Status
	}{
		{name: "draft can be replaced", wantStatus: StatusDraft},
		{
			name: "published is immutable",
			prepare: func(t *testing.T, version *Version) {
				publishValid(t, version)
			},
			wantErr: ErrPublishedVersionImmutable, wantStatus: StatusPublished,
		},
		{
			name: "retired is immutable",
			prepare: func(t *testing.T, version *Version) {
				publishValid(t, version)
				if err := version.Retire(); err != nil {
					t.Fatalf("Retire() error = %v", err)
				}
			},
			wantErr: ErrRetiredVersionImmutable, wantStatus: StatusRetired,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			version := mustDraft(t)
			if test.prepare != nil {
				test.prepare(t, version)
			}
			before := version.Snapshot()
			definition := validDefinition()
			definition.Title = "Новое название"
			err := version.ReplaceDraft(definition)
			assertErrorIs(t, err, test.wantErr)
			after := version.Snapshot()
			if after.Status != test.wantStatus {
				t.Fatalf("status = %q, want %q", after.Status, test.wantStatus)
			}
			if test.wantErr == nil {
				if after.Definition.Title != "Новое название" {
					t.Fatalf("title = %q", after.Definition.Title)
				}
			} else if !reflect.DeepEqual(after, before) {
				t.Fatalf("failed edit changed immutable version\nafter: %#v\nbefore: %#v", after, before)
			}
		})
	}
}

func TestRetireTransitions(t *testing.T) {
	t.Parallel()

	t.Run("draft cannot retire", func(t *testing.T) {
		t.Parallel()
		version := mustDraft(t)
		assertErrorIs(t, version.Retire(), ErrOnlyPublishedCanBeRetired)
		if version.Snapshot().Status != StatusDraft {
			t.Fatal("failed transition changed draft")
		}
	})

	t.Run("published retires and stays immutable", func(t *testing.T) {
		t.Parallel()
		version := mustDraft(t)
		publishValid(t, version)
		if err := version.Retire(); err != nil {
			t.Fatalf("Retire() error = %v", err)
		}
		if version.Snapshot().Status != StatusRetired {
			t.Fatalf("status = %q", version.Snapshot().Status)
		}
		assertErrorIs(t, version.Retire(), ErrRetiredVersionImmutable)
	})
}

func validDraftParams() NewDraftParams {
	return NewDraftParams{
		ID:          "version-1",
		CompanyID:   "company-1",
		CourseID:    "course-1",
		Number:      1,
		Definition:  validDefinition(),
		CreatedByID: "actor-1",
		CreatedAt:   testNow(),
	}
}

func draftSnapshot() Snapshot {
	params := validDraftParams()
	return Snapshot{
		ID: params.ID, CompanyID: params.CompanyID, CourseID: params.CourseID,
		Number: params.Number, Status: StatusDraft, Definition: params.Definition,
		CreatedByID: params.CreatedByID, CreatedAt: params.CreatedAt,
	}
}

func mustDraft(t *testing.T) *Version {
	t.Helper()
	version, err := NewDraft(validDraftParams())
	if err != nil {
		t.Fatalf("NewDraft() error = %v", err)
	}
	return version
}

func testNow() time.Time {
	return time.Date(2026, time.July, 22, 14, 0, 0, 0, time.UTC)
}

func assertErrorIs(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
}
