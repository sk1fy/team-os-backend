package preview

import (
	"errors"
	"testing"
	"time"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
)

var previewNow = time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)

func TestPreviewAndTestModeNeverPersistRealProgress(t *testing.T) {
	t.Parallel()

	for _, mode := range []Mode{ModePreview, ModeTest} {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			session, err := New(validParams(mode))
			if err != nil {
				t.Fatal(err)
			}
			if err := session.RecordLesson("lesson-1", true, previewNow.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}

			realProgressWrites := 0
			writeRealProgress := func() {
				if session.GuardRealProgressWrite() == nil {
					realProgressWrites++
				}
			}
			writeRealProgress()
			if realProgressWrites != 0 {
				t.Fatalf("real progress writes = %d, want 0", realProgressWrites)
			}
			if !errors.Is(session.GuardRealProgressWrite(), ErrRealProgressWriteForbidden) {
				t.Fatalf("GuardRealProgressWrite() = %v", session.GuardRealProgressWrite())
			}
			if got := session.Snapshot(); len(got.Activity) != 1 || !got.Activity[0].Completed {
				t.Fatalf("ephemeral activity = %#v", got.Activity)
			}
		})
	}
}

func TestPreviewAcceptsAdministrativeStatesButNotDeleted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		lifecycle    course.LifecycleStatus
		distribution course.DistributionStatus
		wantErr      error
	}{
		{name: "active", lifecycle: course.CourseActive, distribution: course.DistributionActive},
		{name: "paused", lifecycle: course.CourseActive, distribution: course.DistributionPaused},
		{name: "blocked service preview", lifecycle: course.CourseActive, distribution: course.DistributionBlocked},
		{name: "archived service preview", lifecycle: course.CourseArchived, distribution: course.DistributionActive},
		{name: "deleted remains hidden", lifecycle: course.CourseDeleted, distribution: course.DistributionBlocked, wantErr: ErrDeletedCourse},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			params := validParams(ModePreview)
			params.Course.LifecycleStatus = test.lifecycle
			params.Course.DistributionStatus = test.distribution
			_, err := New(params)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestPreviewTargetValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Params)
		wantErr error
	}{
		{name: "company original", mutate: func(params *Params) {
			params.Course.OwnerType = course.CourseOwnerCompany
			params.Course.OwnerUserID = nil
		}, wantErr: ErrPartnerCourseRequired},
		{name: "draft", mutate: func(params *Params) {
			params.Version.Status = courseversion.StatusDraft
			params.Version.PublishedByID = nil
			params.Version.PublishedAt = nil
			params.Version.ContentHash = ""
		}, wantErr: ErrPublishedVersionRequired},
		{name: "other course", mutate: func(params *Params) { params.Version.CourseID = "course-2" }, wantErr: ErrVersionScopeMismatch},
		{name: "unknown mode", mutate: func(params *Params) { params.Mode = "learn" }, wantErr: ErrUnknownMode},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			params := validParams(ModePreview)
			test.mutate(&params)
			_, err := New(params)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestPreviewSnapshotIsDefensive(t *testing.T) {
	t.Parallel()

	session, err := New(validParams(ModeTest))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.RecordLesson("lesson-1", true, previewNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	snapshot := session.Snapshot()
	snapshot.Activity[0].Completed = false
	if !session.Snapshot().Activity[0].Completed {
		t.Fatal("caller mutated session through Snapshot")
	}
}

func validParams(mode Mode) Params {
	partnerID := course.ID("partner-1")
	publisherID := courseversion.ID("partner-1")
	publishedAt := previewNow.Add(-time.Hour)
	return Params{
		SessionID: "preview-1", ActorID: "owner-1", Mode: mode, StartedAt: previewNow,
		Course: course.Course{
			ID: "course-1", CompanyID: "company-1", OwnerType: course.CourseOwnerPartner,
			OwnerUserID: &partnerID, LifecycleStatus: course.CourseActive,
			DistributionStatus: course.DistributionActive,
		},
		Version: courseversion.Snapshot{
			ID: "version-1", CompanyID: "company-1", CourseID: "course-1", Number: 1,
			Status: courseversion.StatusPublished, CreatedByID: publisherID,
			CreatedAt: previewNow.Add(-2 * time.Hour), PublishedByID: &publisherID,
			PublishedAt: &publishedAt, ContentHash: "hash",
			Definition: courseversion.Definition{
				Title:    "Партнёрский курс",
				Sections: []courseversion.Section{{ID: "section-1", StableKey: "section", Title: "Раздел", Order: 0}},
				Lessons:  []courseversion.Lesson{{ID: "lesson-1", SectionID: "section-1", StableKey: "lesson", Title: "Урок", Content: []byte(`{"type":"doc"}`)}},
			},
		},
	}
}
