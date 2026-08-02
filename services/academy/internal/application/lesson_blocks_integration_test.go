//go:build integration

package application

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func TestLessonBlocksQuizAndSectionTitlePersist(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := academyTestPool(t, ctx)
	service, err := NewService(pool, nil, nil, nil)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	actor := Actor{CompanyID: uuid.New(), UserID: uuid.New(), Role: "partner"}
	course, err := service.CreateCourse(ctx, actor, CreateCourseInput{Title: "Блочный курс"})
	if err != nil || course.CurrentDraftVersionID == nil {
		t.Fatalf("create course: course=%+v err=%v", course, err)
	}
	draftID := *course.CurrentDraftVersionID
	queries := db.New(pool)
	sections, err := queries.GetCourseVersionSections(ctx, db.GetCourseVersionSectionsParams{
		CompanyID: actor.CompanyID, CourseVersionID: draftID,
	})
	if err != nil || len(sections) != 1 {
		t.Fatalf("get initial section: sections=%v err=%v", sections, err)
	}

	sectionTitle := "Постоянное название"
	if _, err = service.UpdateCourseVersionSection(ctx, actor, UpdateCourseVersionSectionInput{
		ID: sections[0].ID, Title: &sectionTitle,
	}); err != nil {
		t.Fatalf("rename section: %v", err)
	}

	lesson, err := service.CreateCourseVersionLesson(ctx, actor, CreateCourseVersionLessonInput{
		VersionID: draftID, SectionVersionID: sections[0].ID, Title: "Урок",
		Content: json.RawMessage(`{"type":"doc","content":[]}`),
	})
	if err != nil {
		t.Fatalf("create lesson: %v", err)
	}
	blockContent := json.RawMessage(`{
		"type":"doc",
		"content":[
			{"type":"lessonBlock","attrs":{"id":"block-text","kind":"richText"},"content":[{"type":"paragraph","content":[{"type":"text","text":"Материал"}]}]},
			{"type":"lessonBlock","attrs":{"id":"block-callout","kind":"callout","data":{"style":"card","tone":"warning","title":"Важно","body":"Замечание"}}},
			{"type":"lessonBlock","attrs":{"id":"block-checklist","kind":"checklist","data":{"style":"card","title":"Проверьте себя","items":[{"id":"item-1","text":"Первый пункт"}]}}},
			{"type":"lessonBlock","attrs":{"id":"block-quiz","kind":"quiz"}}
		]
	}`)
	if _, err = service.UpdateCourseVersionLesson(ctx, actor, UpdateCourseVersionLessonInput{
		ID: lesson.ID, Content: blockContent, SetContent: true,
	}); err != nil {
		t.Fatalf("save lesson blocks: %v", err)
	}
	questions := json.RawMessage(`[{"id":"q-1","type":"single","text":"Вопрос?","options":[{"id":"o-1","text":"Да","correct":true},{"id":"o-2","text":"Нет","correct":false}]}]`)
	if _, err = service.UpsertCourseVersionQuiz(ctx, actor, UpsertCourseVersionQuizInput{
		LessonVersionID: lesson.ID, Questions: questions, PassingScore: 100,
	}); err != nil {
		t.Fatalf("save quiz: %v", err)
	}

	draft, err := service.GetCourseVersion(ctx, actor, course.ID, draftID)
	if err != nil {
		t.Fatalf("reload draft: %v", err)
	}
	if len(draft.Sections) != 1 || draft.Sections[0].Title != sectionTitle {
		t.Fatalf("section title was not persisted: %+v", draft.Sections)
	}
	if len(draft.Lessons) != 1 || len(draft.Quizzes) != 1 || draft.Lessons[0].QuizVersionID == nil {
		t.Fatalf("lesson quiz was not persisted: lessons=%+v quizzes=%+v", draft.Lessons, draft.Quizzes)
	}
	var stored map[string]any
	if err = json.Unmarshal(draft.Lessons[0].Content, &stored); err != nil {
		t.Fatalf("decode stored blocks: %v", err)
	}
	content, _ := stored["content"].([]any)
	if len(content) != 4 {
		t.Fatalf("stored lesson blocks=%d, want 4", len(content))
	}

	publicVisibility := "public"
	if updated, updateErr := service.UpdateCourse(ctx, actor, UpdateCourseInput{
		ID: course.ID, Visibility: &publicVisibility,
	}); updateErr != nil || updated.Visibility != publicVisibility {
		t.Fatalf("make partner course public: course=%+v err=%v", updated, updateErr)
	}
	if _, err = service.PublishCourseVersion(ctx, actor, course.ID, "lesson-blocks-v1"); err != nil {
		t.Fatalf("publish first version: %v", err)
	}
	cloned, err := service.CreateCourseDraft(ctx, actor, course.ID)
	if err != nil {
		t.Fatalf("create cloned draft: %v", err)
	}
	clonedContent, err := service.GetCourseVersion(ctx, actor, course.ID, cloned.ID)
	if err != nil {
		t.Fatalf("reload cloned draft: %v", err)
	}
	if len(clonedContent.Sections) != 1 || clonedContent.Sections[0].Title != sectionTitle ||
		len(clonedContent.Lessons) != 1 || len(clonedContent.Quizzes) != 1 {
		t.Fatalf("cloned draft lost content: %+v", clonedContent)
	}
}
