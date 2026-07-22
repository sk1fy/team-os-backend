package coursecopy

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
)

var copyNow = time.Date(2026, 7, 22, 11, 0, 0, 0, time.UTC)

func TestCopyPartnerVersionCreatesIndependentCompanyDraft(t *testing.T) {
	t.Parallel()

	params := validCopyParams()
	plan, err := CopyPartnerVersion(params)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Course.OwnerType != course.CourseOwnerCompany || plan.Course.OwnerUserID != nil ||
		plan.Course.LifecycleStatus != course.CourseActive || plan.Course.DistributionStatus != course.DistributionActive {
		t.Fatalf("destination course = %#v", plan.Course)
	}
	if plan.Origin.SourceCourseID != params.SourceCourse.ID ||
		plan.Origin.SourceVersionID != params.SourceVersion.ID ||
		plan.Origin.SourcePartnerID != *params.SourceCourse.OwnerUserID {
		t.Fatalf("origin = %#v", plan.Origin)
	}

	draft := plan.Draft.Snapshot()
	if draft.Status != courseversion.StatusDraft || draft.Number != 1 ||
		draft.CourseID != courseversion.ID(params.DestinationCourseID) || draft.CompanyID != params.SourceVersion.CompanyID {
		t.Fatalf("draft root = %#v", draft)
	}
	source := params.SourceVersion.Definition
	if draft.Definition.Sections[0].ID == source.Sections[0].ID ||
		draft.Definition.Lessons[0].ID == source.Lessons[0].ID ||
		draft.Definition.Lessons[0].Quiz.ID == source.Lessons[0].Quiz.ID ||
		draft.Definition.Lessons[0].FileIDs[0] == source.Lessons[0].FileIDs[0] ||
		*draft.Definition.CoverFileID == *source.CoverFileID {
		t.Fatalf("nested identifiers were reused: %#v", draft.Definition)
	}
	if draft.Definition.Lessons[0].SourceType != "kb_snapshot" {
		t.Fatalf("KB live source was not snapped: %q", draft.Definition.Lessons[0].SourceType)
	}
	if draft.Definition.Lessons[0].FileIDs[0] != *draft.Definition.CoverFileID {
		t.Fatal("repeated source file did not retain one cloned destination ID")
	}

	// Changing or deleting the original after planning cannot mutate the copy.
	params.SourceVersion.Definition.Title = "Изменённый оригинал"
	params.SourceVersion.Definition.Lessons[0].Content[0] = 'X'
	params.SourceVersion.Definition.Lessons[0].Quiz.Questions[0].Options[0].Text = "Изменено"
	params.SourceCourse.LifecycleStatus = course.CourseDeleted
	got := plan.Draft.Snapshot()
	if got.Definition.Title == params.SourceVersion.Definition.Title || got.Definition.Lessons[0].Content[0] == 'X' ||
		got.Definition.Lessons[0].Quiz.Questions[0].Options[0].Text == "Изменено" ||
		plan.Course.LifecycleStatus != course.CourseActive {
		t.Fatalf("source mutation leaked into destination: %#v", got.Definition)
	}

	// Mutating an observed draft snapshot cannot reach either aggregate.
	got.Definition.Lessons[0].Content[0] = 'Y'
	if plan.Draft.Snapshot().Definition.Lessons[0].Content[0] == 'Y' || params.SourceVersion.Definition.Lessons[0].Content[0] == 'Y' {
		t.Fatal("draft snapshot is not independent")
	}
}

func TestCopyPartnerVersionValidatesSourceAndDestination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Params)
		wantErr error
	}{
		{
			name: "company source",
			mutate: func(params *Params) {
				params.SourceCourse.OwnerType = course.CourseOwnerCompany
				params.SourceCourse.OwnerUserID = nil
			},
			wantErr: ErrPartnerSourceRequired,
		},
		{name: "deleted source", mutate: func(params *Params) { params.SourceCourse.LifecycleStatus = course.CourseDeleted }, wantErr: ErrSourceUnavailable},
		{name: "blocked source", mutate: func(params *Params) { params.SourceCourse.DistributionStatus = course.DistributionBlocked }, wantErr: ErrSourceUnavailable},
		{
			name: "draft source",
			mutate: func(params *Params) {
				params.SourceVersion.Status = courseversion.StatusDraft
				params.SourceVersion.PublishedByID = nil
				params.SourceVersion.PublishedAt = nil
				params.SourceVersion.ContentHash = ""
			},
			wantErr: ErrPublishedSourceRequired,
		},
		{name: "other course version", mutate: func(params *Params) { params.SourceVersion.CourseID = "other" }, wantErr: ErrSourceVersionMismatch},
		{name: "reused course ID", mutate: func(params *Params) { params.DestinationCourseID = params.SourceCourse.ID }, wantErr: ErrDestinationCourseRequired},
		{name: "reused version ID", mutate: func(params *Params) { params.DestinationVersionID = params.SourceVersion.ID }, wantErr: ErrDestinationVersionNeeded},
		{name: "missing mapper", mutate: func(params *Params) { params.MapID = nil }, wantErr: ErrIDMapperRequired},
		{name: "mapper reuses source IDs", mutate: func(params *Params) {
			params.MapID = func(_ EntityKind, id courseversion.ID) courseversion.ID { return id }
		}, wantErr: ErrIndependentIDRequired},
		{name: "mapper collides", mutate: func(params *Params) {
			params.MapID = func(EntityKind, courseversion.ID) courseversion.ID { return "same" }
		}, wantErr: ErrDuplicateDestinationID},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			params := validCopyParams()
			test.mutate(&params)
			_, err := CopyPartnerVersion(params)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestCopyAllowsPausedOrArchivedPublishedSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		lifecycle    course.LifecycleStatus
		distribution course.DistributionStatus
	}{
		{name: "paused", lifecycle: course.CourseActive, distribution: course.DistributionPaused},
		{name: "archived", lifecycle: course.CourseArchived, distribution: course.DistributionActive},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			params := validCopyParams()
			params.SourceCourse.LifecycleStatus = test.lifecycle
			params.SourceCourse.DistributionStatus = test.distribution
			if _, err := CopyPartnerVersion(params); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func validCopyParams() Params {
	partnerID := course.ID("partner-1")
	publisherID := courseversion.ID("partner-1")
	publishedAt := copyNow.Add(-time.Hour)
	description := "Описание"
	coverURL := "https://files.invalid/cover"
	deadline := 3
	articleID := courseversion.ID("article-1")
	articleVersion := 7
	maxAttempts := 2
	coverFileID := courseversion.ID("file-1")
	return Params{
		SourceCourse: course.Course{
			ID: "partner-course-1", CompanyID: "company-1", OwnerType: course.CourseOwnerPartner,
			OwnerUserID: &partnerID, LifecycleStatus: course.CourseActive,
			DistributionStatus: course.DistributionActive,
		},
		SourceVersion: courseversion.Snapshot{
			ID: "source-version-3", CompanyID: "company-1", CourseID: "partner-course-1",
			Number: 3, Status: courseversion.StatusPublished, CreatedByID: publisherID,
			CreatedAt: copyNow.Add(-2 * time.Hour), PublishedByID: &publisherID,
			PublishedAt: &publishedAt, ContentHash: "immutable-hash",
			Definition: courseversion.Definition{
				Title: "Партнёрский курс", Description: &description, CoverFileID: &coverFileID,
				CoverURL: &coverURL, Sequential: true, DefaultInternalDeadlineDays: &deadline,
				Sections: []courseversion.Section{{ID: "section-1", StableKey: "section", Title: "Раздел", Order: 0}},
				Lessons: []courseversion.Lesson{{
					ID: "lesson-1", SectionID: "section-1", StableKey: "lesson", Title: "Урок", Order: 0,
					Content: []byte(`{"type":"doc"}`), SourceType: "kb_link",
					SourceArticleID: &articleID, SourceArticleVersion: &articleVersion,
					FileIDs: []courseversion.ID{"file-1"},
					Quiz: &courseversion.Quiz{
						ID: "quiz-1", PassingScore: 80, MaxAttempts: &maxAttempts,
						Questions: []courseversion.Question{{
							ID: "question-1", Type: courseversion.QuestionSingle, Text: "Вопрос?",
							Options: []courseversion.Option{{ID: "option-1", Text: "Да", Correct: true}, {ID: "option-2", Text: "Нет"}},
						}},
					},
				}},
			},
		},
		DestinationCourseID: "company-course-1", DestinationVersionID: "company-draft-1",
		CreatedByID: "owner-1", CreatedAt: copyNow,
		MapID: func(kind EntityKind, sourceID courseversion.ID) courseversion.ID {
			return courseversion.ID(fmt.Sprintf("copy-%s-%s", kind, sourceID))
		},
	}
}
