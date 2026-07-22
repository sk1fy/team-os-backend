package template

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
)

var templateNow = time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC)

func TestSystemTemplateCatalogueAndImmutability(t *testing.T) {
	t.Parallel()

	keys := SystemTemplateKeys()
	if len(keys) != 10 {
		t.Fatalf("system template count = %d, want 10", len(keys))
	}
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if !IsSystemTemplateKey(key) {
			t.Fatalf("catalogue key %q not recognized", key)
		}
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate system key %q", key)
		}
		seen[key] = struct{}{}
	}

	system, err := NewSystem("template-1", "company-1", "system", keys[0], "version-1", templateNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := system.Archive(); !errors.Is(err, ErrSystemTemplateImmutable) {
		t.Fatalf("Archive() error = %v", err)
	}
	draft, err := NewDraft(system.Snapshot(), NewDraftParams{
		ID: "version-2", Number: 2, Definition: validDefinition(), CreatedByID: "owner-1", CreatedAt: templateNow,
	})
	if draft != nil || !errors.Is(err, ErrCompanyDraftRequiresRoot) {
		t.Fatalf("NewDraft(system) = %#v, %v", draft, err)
	}
}

func TestSystemSeedAddsPublishedVersionsWithoutDrafts(t *testing.T) {
	t.Parallel()

	root, err := NewSystem("template-1", "company-1", "system", "employee-onboarding", "version-1", templateNow)
	if err != nil {
		t.Fatal(err)
	}
	version, err := NewSystemPublished(root.Snapshot(), NewDraftParams{
		ID: "version-2", Number: 2, Definition: validDefinition(),
		CreatedByID: "system", CreatedAt: templateNow.Add(time.Hour),
	}, PublishParams{ActorID: "system", At: templateNow.Add(time.Hour)}, validValidators())
	if err != nil {
		t.Fatal(err)
	}
	if err := root.RecordSystemPublication(version.Snapshot()); err != nil {
		t.Fatal(err)
	}
	got := root.Snapshot()
	if got.CurrentDraftVersionID != nil || got.LatestPublishedVersionID == nil || *got.LatestPublishedVersionID != "version-2" {
		t.Fatalf("system root = %#v", got)
	}
	if err := version.ReplaceDraft(root.Snapshot(), validDefinition()); !errors.Is(err, ErrSystemTemplateImmutable) {
		t.Fatalf("ReplaceDraft(system) error = %v", err)
	}
}

func TestCompanyTemplateLifecycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		action  func(*Template) error
		wantErr error
		want    LifecycleStatus
	}{
		{name: "archive active", action: func(root *Template) error { return root.Archive() }, want: LifecycleArchived},
		{name: "archive twice", action: func(root *Template) error {
			_ = root.Archive()
			return root.Archive()
		}, wantErr: ErrTemplateAlreadyArchived, want: LifecycleArchived},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := newCompanyTemplate(t)
			err := test.action(root)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if got := root.Snapshot().LifecycleStatus; got != test.want {
				t.Fatalf("lifecycle = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCompanyTemplateDraftPublishAndImmutability(t *testing.T) {
	t.Parallel()

	root := newCompanyTemplate(t)
	definition := validDefinition()
	draft, err := NewDraft(root.Snapshot(), NewDraftParams{
		ID: "version-1", Number: 1, Definition: definition,
		CreatedByID: "admin-1", CreatedAt: templateNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.AttachDraft(draft.Snapshot()); err != nil {
		t.Fatal(err)
	}
	if err := draft.Publish(root.Snapshot(), PublishParams{ActorID: "owner-1", At: templateNow.Add(time.Minute)}, validValidators()); err != nil {
		t.Fatal(err)
	}
	if err := root.RecordPublication(draft.Snapshot()); err != nil {
		t.Fatal(err)
	}

	published := draft.Snapshot()
	if published.Status != VersionPublished || published.ContentHash == "" ||
		root.Snapshot().CurrentDraftVersionID != nil ||
		root.Snapshot().LatestPublishedVersionID == nil || *root.Snapshot().LatestPublishedVersionID != published.ID {
		t.Fatalf("published template = %#v root=%#v", published, root.Snapshot())
	}
	if err := draft.ReplaceDraft(root.Snapshot(), validDefinition()); !errors.Is(err, ErrPublishedVersionImmutable) {
		t.Fatalf("ReplaceDraft() error = %v", err)
	}

	// Both input and output snapshots are detached from the frozen version.
	definition.Title = "Атака через вход"
	published.Definition.Title = "Атака через выход"
	published.Definition.Lessons[0].Content[0] = 'X'
	got := draft.Snapshot()
	if got.Definition.Title != "Шаблон адаптации" || got.Definition.Lessons[0].Content[0] == 'X' {
		t.Fatalf("published content mutated: %#v", got.Definition)
	}
}

func TestTemplateVersionNumbering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		versions  []VersionRef
		expected  int
		want      int
		wantError error
	}{
		{name: "first", expected: 0, want: 1},
		{name: "next", versions: []VersionRef{{Number: 1, Status: VersionPublished}}, expected: 1, want: 2},
		{name: "draft exists", versions: []VersionRef{{Number: 1, Status: VersionPublished}, {Number: 2, Status: VersionDraft}}, expected: 2, wantError: ErrDraftAlreadyExists},
		{name: "stale", versions: []VersionRef{{Number: 1, Status: VersionPublished}}, expected: 0, wantError: ErrVersionNumberConflict},
		{name: "gap", versions: []VersionRef{{Number: 2, Status: VersionPublished}}, expected: 1, wantError: ErrVersionNumberGap},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := PlanNextDraft(test.versions, test.expected)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("error = %v, want %v", err, test.wantError)
			}
			if got != test.want {
				t.Fatalf("number = %d, want %d", got, test.want)
			}
		})
	}
}

func TestInstantiateCreatesIndependentCourseDraft(t *testing.T) {
	t.Parallel()

	root, version := publishedCompanyTemplate(t)
	params := InstantiationParams{
		SourceTemplate: root.Snapshot(), SourceVersion: version.Snapshot(),
		DestinationCourseID: "course-1", DestinationDraftID: "course-version-1",
		TargetOwnerType:   course.CourseOwnerPartner,
		TargetOwnerUserID: courseIDPointer("partner-1"),
		CreatedByID:       "partner-1", CreatedAt: templateNow.Add(time.Hour),
		MapID: func(kind EntityKind, sourceID courseversion.ID) courseversion.ID {
			return courseversion.ID(fmt.Sprintf("copy-%s-%s", kind, sourceID))
		},
	}
	plan, err := Instantiate(params)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Course.OwnerType != course.CourseOwnerPartner || plan.Course.OwnerUserID == nil ||
		*plan.Course.OwnerUserID != "partner-1" || plan.Origin.Type != OriginCompanyTemplate {
		t.Fatalf("plan roots = %#v %#v", plan.Course, plan.Origin)
	}
	destination := plan.Draft.Snapshot()
	source := params.SourceVersion.Definition
	lesson := destination.Definition.Lessons[0]
	if destination.Status != courseversion.StatusDraft || destination.Number != 1 ||
		destination.Definition.Sections[0].ID == source.Sections[0].ID || lesson.ID == source.Lessons[0].ID ||
		lesson.SourceType != "template_snapshot" || lesson.SourceTemplateID == nil ||
		*lesson.SourceTemplateID != courseversion.ID(params.SourceTemplate.ID) ||
		lesson.SourceTemplateVersionID == nil || *lesson.SourceTemplateVersionID != courseversion.ID(params.SourceVersion.ID) {
		t.Fatalf("destination draft = %#v", destination)
	}
	if destination.Definition.CoverFileID == nil || lesson.FileIDs[0] != *destination.Definition.CoverFileID {
		t.Fatal("repeated file reference was not cloned consistently")
	}

	params.SourceVersion.Definition.Title = "Изменённый шаблон"
	params.SourceVersion.Definition.Lessons[0].Content[0] = 'X'
	if got := plan.Draft.Snapshot(); got.Definition.Title == "Изменённый шаблон" || got.Definition.Lessons[0].Content[0] == 'X' {
		t.Fatalf("template mutation leaked into course: %#v", got.Definition)
	}
}

func TestInstantiatePolicyBoundaries(t *testing.T) {
	t.Parallel()

	root, version := publishedCompanyTemplate(t)
	base := InstantiationParams{
		SourceTemplate: root.Snapshot(), SourceVersion: version.Snapshot(),
		DestinationCourseID: "course-1", DestinationDraftID: "draft-1",
		TargetOwnerType: course.CourseOwnerCompany, CreatedByID: "owner-1", CreatedAt: templateNow,
		MapID: func(kind EntityKind, sourceID courseversion.ID) courseversion.ID {
			return "new-" + courseversion.ID(kind) + "-" + sourceID
		},
	}
	tests := []struct {
		name    string
		mutate  func(*InstantiationParams)
		wantErr error
	}{
		{name: "active published"},
		{name: "archived", mutate: func(params *InstantiationParams) { params.SourceTemplate.LifecycleStatus = LifecycleArchived }, wantErr: ErrTemplateArchived},
		{name: "draft source", mutate: func(params *InstantiationParams) {
			params.SourceVersion.Status = VersionDraft
			params.SourceVersion.PublishedByID = nil
			params.SourceVersion.PublishedAt = nil
			params.SourceVersion.ContentHash = ""
		}, wantErr: ErrPublishedVersionRequired},
		{name: "partner owner differs from actor", mutate: func(params *InstantiationParams) {
			params.TargetOwnerType = course.CourseOwnerPartner
			params.TargetOwnerUserID = courseIDPointer("partner-2")
			params.CreatedByID = "partner-1"
		}, wantErr: ErrTargetOwnerInvalid},
		{name: "company course has user owner", mutate: func(params *InstantiationParams) {
			params.TargetOwnerUserID = courseIDPointer("owner-1")
		}, wantErr: ErrTargetOwnerInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			params := base
			if test.mutate != nil {
				test.mutate(&params)
			}
			_, err := Instantiate(params)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func newCompanyTemplate(t *testing.T) *Template {
	t.Helper()
	root, err := NewCompany("template-1", "company-1", "owner-1", templateNow)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func publishedCompanyTemplate(t *testing.T) (*Template, *Version) {
	t.Helper()
	root := newCompanyTemplate(t)
	version, err := NewDraft(root.Snapshot(), NewDraftParams{
		ID: "template-version-1", Number: 1, Definition: validDefinition(),
		CreatedByID: "owner-1", CreatedAt: templateNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.AttachDraft(version.Snapshot()); err != nil {
		t.Fatal(err)
	}
	if err := version.Publish(root.Snapshot(), PublishParams{ActorID: "admin-1", At: templateNow.Add(time.Minute)}, validValidators()); err != nil {
		t.Fatal(err)
	}
	if err := root.RecordPublication(version.Snapshot()); err != nil {
		t.Fatal(err)
	}
	return root, version
}

func validDefinition() Definition {
	description := "Описание"
	coverFileID := courseversion.ID("file-1")
	maxAttempts := 2
	return Definition{
		Title: "Шаблон адаптации", Description: &description, CoverFileID: &coverFileID, Sequential: true,
		Sections: []courseversion.Section{{ID: "section-1", StableKey: "section", Title: "Раздел", Order: 0}},
		Lessons: []courseversion.Lesson{{
			ID: "lesson-1", SectionID: "section-1", StableKey: "lesson", Title: "Урок", Order: 0,
			Content: json.RawMessage(`{"type":"doc"}`), SourceType: "manual", FileIDs: []courseversion.ID{"file-1"},
			Quiz: &courseversion.Quiz{
				ID: "quiz-1", PassingScore: 80, MaxAttempts: &maxAttempts,
				Questions: []courseversion.Question{{
					ID: "question-1", Type: courseversion.QuestionSingle, Text: "Вопрос?",
					Options: []courseversion.Option{{ID: "option-1", Text: "Да", Correct: true}, {ID: "option-2", Text: "Нет"}},
				}},
			},
		}},
	}
}

func validValidators() PublicationValidators {
	return PublicationValidators{
		RichText:      func(json.RawMessage) error { return nil },
		FileAvailable: func(courseversion.ID) bool { return true },
	}
}

func courseIDPointer(value course.ID) *course.ID { return &value }
