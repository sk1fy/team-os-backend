// Package seed loads exported frontend fixtures into the academy database so
// the development environment mirrors the mock API (§13).
package seed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var fixtureNamespace = uuid.MustParse("7c29d63e-2954-5e23-9cba-7b950e8cc1a8")

type Summary struct {
	CompanyID   string
	Courses     int
	Sections    int
	Lessons     int
	Quizzes     int
	Assignments int
	Progress    int
}

type Fixtures struct {
	CompanyID      string
	Courses        []CourseFixture
	CourseSections []CourseSectionFixture
	Lessons        []LessonFixture
	Quizzes        []QuizFixture
	Assignments    []AssignmentFixture
	Progress       []ProgressFixture
}

type CourseFixture struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Description  *string `json:"description"`
	CoverURL     *string `json:"coverUrl"`
	Status       string  `json:"status"`
	AuthorID     string  `json:"authorId"`
	Sequential   bool    `json:"sequential"`
	DeadlineDays *int32  `json:"deadlineDays"`
	CreatedAt    string  `json:"createdAt"`
	UpdatedAt    string  `json:"updatedAt"`
}

type CourseSectionFixture struct {
	ID       string `json:"id"`
	CourseID string `json:"courseId"`
	Title    string `json:"title"`
	Order    int32  `json:"order"`
}

type LessonFixture struct {
	ID              string          `json:"id"`
	CourseID        string          `json:"courseId"`
	SectionID       string          `json:"sectionId"`
	Title           string          `json:"title"`
	Order           int32           `json:"order"`
	Content         json.RawMessage `json:"content"`
	SourceArticleID *string         `json:"sourceArticleId"`
	SourceMode      *string         `json:"sourceMode"`
	QuizID          *string         `json:"quizId"`
}

type QuizFixture struct {
	ID           string          `json:"id"`
	LessonID     string          `json:"lessonId"`
	Questions    json.RawMessage `json:"questions"`
	PassingScore int32           `json:"passingScore"`
	MaxAttempts  *int32          `json:"maxAttempts"`
}

type AssignmentFixture struct {
	ID           string  `json:"id"`
	CourseID     string  `json:"courseId"`
	AssigneeType string  `json:"assigneeType"`
	AssigneeID   *string `json:"assigneeId"`
	InviteToken  *string `json:"inviteToken"`
	DueDate      *string `json:"dueDate"`
	AssignedByID string  `json:"assignedById"`
	CreatedAt    string  `json:"createdAt"`
}

type ProgressFixture struct {
	UserID             string   `json:"userId"`
	CourseID           string   `json:"courseId"`
	Status             string   `json:"status"`
	CompletedLessonIDs []string `json:"completedLessonIds"`
	StartedAt          *string  `json:"startedAt"`
	CompletedAt        *string  `json:"completedAt"`
}

func Run(ctx context.Context, pool *pgxpool.Pool, directory string) (Summary, error) {
	if pool == nil {
		return Summary{}, errors.New("соединение с PostgreSQL не задано")
	}
	fixtures, err := Load(directory)
	if err != nil {
		return Summary{}, err
	}
	dataset, err := Normalize(fixtures)
	if err != nil {
		return Summary{}, fmt.Errorf("проверить фикстуры: %w", err)
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Summary{}, fmt.Errorf("начать seed-транзакцию: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := Apply(ctx, tx, dataset); err != nil {
		return Summary{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Summary{}, fmt.Errorf("зафиксировать seed-транзакцию: %w", err)
	}
	return Summary{
		CompanyID: dataset.CompanyID.String(),
		Courses:   len(dataset.Courses), Sections: len(dataset.Sections),
		Lessons: len(dataset.Lessons), Quizzes: len(dataset.Quizzes),
		Assignments: len(dataset.Assignments), Progress: len(dataset.Progress),
	}, nil
}

func Load(directory string) (Fixtures, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return Fixtures{}, errors.New("директория фикстур не задана")
	}
	var fixtures Fixtures
	if raw, err := os.ReadFile(filepath.Join(directory, "company.json")); err == nil {
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Fixtures{}, fmt.Errorf("company.json: %w", err)
		}
		fixtures.CompanyID = payload.ID
	}
	if fixtures.CompanyID == "" {
		if err := readWrapped(directory, []string{"fixtures.json", "seed.json", "manifest.json"}, func(key string, raw json.RawMessage) error {
			switch key {
			case "company":
				var payload struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(raw, &payload); err != nil {
					return err
				}
				fixtures.CompanyID = payload.ID
			case "courses":
				return json.Unmarshal(raw, &fixtures.Courses)
			case "courseSections":
				return json.Unmarshal(raw, &fixtures.CourseSections)
			case "lessons":
				return json.Unmarshal(raw, &fixtures.Lessons)
			case "quizzes":
				return json.Unmarshal(raw, &fixtures.Quizzes)
			case "courseAssignments", "assignments":
				return json.Unmarshal(raw, &fixtures.Assignments)
			case "courseProgress", "progress":
				return json.Unmarshal(raw, &fixtures.Progress)
			}
			return nil
		}); err != nil {
			return Fixtures{}, err
		}
	}
	for _, name := range []struct {
		file string
		keys []string
		load func([]byte) error
	}{
		{"courses.json", []string{"courses"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.Courses) }},
		{"course-sections.json", []string{"courseSections"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.CourseSections) }},
		{"course_sections.json", []string{"courseSections"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.CourseSections) }},
		{"lessons.json", []string{"lessons"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.Lessons) }},
		{"quizzes.json", []string{"quizzes"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.Quizzes) }},
		{"course-assignments.json", []string{"courseAssignments"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.Assignments) }},
		{"course_assignments.json", []string{"courseAssignments"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.Assignments) }},
		{"course-progress.json", []string{"courseProgress"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.Progress) }},
		{"course_progress.json", []string{"courseProgress"}, func(raw []byte) error { return json.Unmarshal(raw, &fixtures.Progress) }},
	} {
		_ = readEntityFile(filepath.Join(directory, name.file), name.keys, name.load)
	}
	if fixtures.CompanyID == "" {
		return Fixtures{}, errors.New("company.id не найден в фикстурах")
	}
	if len(fixtures.Courses) == 0 {
		return Fixtures{}, errors.New("courses не найдены в фикстурах")
	}
	return fixtures, nil
}

func readEntityFile(path string, keys []string, load func([]byte) error) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var direct any
	if err := json.Unmarshal(raw, &direct); err != nil {
		return err
	}
	switch value := direct.(type) {
	case []any:
		return load(raw)
	case map[string]any:
		for _, key := range keys {
			if nested, ok := value[key]; ok {
				encoded, encodeErr := json.Marshal(nested)
				if encodeErr != nil {
					return encodeErr
				}
				return load(encoded)
			}
		}
	}
	return load(raw)
}

func readWrapped(directory string, names []string, handle func(string, json.RawMessage) error) error {
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			continue
		}
		var manifest map[string]json.RawMessage
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return fmt.Errorf("разобрать %s: %w", name, err)
		}
		for key, value := range manifest {
			if err := handle(key, value); err != nil {
				return fmt.Errorf("%s.%s: %w", name, key, err)
			}
		}
		return nil
	}
	return errors.New("manifest fixtures not found")
}

type Dataset struct {
	CompanyID   uuid.UUID
	Courses     []courseRow
	Sections    []sectionRow
	Lessons     []lessonRow
	Quizzes     []quizRow
	Assignments []assignmentRow
	Progress    []progressRow
}

type courseRow struct {
	ID, CompanyID, AuthorID uuid.UUID
	VersionID               uuid.UUID
	Title, Status           string
	Description, CoverURL   *string
	Sequential              bool
	DeadlineDays            *int32
	CreatedAt, UpdatedAt    time.Time
}

type sectionRow struct {
	ID, CompanyID, CourseID uuid.UUID
	Title                   string
	Order                   int32
}

type lessonRow struct {
	ID, CompanyID, CourseID, SectionID uuid.UUID
	Title                              string
	Order                              int32
	Content                            []byte
	SourceArticleID, QuizID            *uuid.UUID
	SourceArticleTitle, SourceMode     *string
}

type quizRow struct {
	ID, CompanyID, LessonID uuid.UUID
	Questions               []byte
	PassingScore            int32
	MaxAttempts             *int32
}

type assignmentRow struct {
	ID, CompanyID, CourseID, AssignedByID uuid.UUID
	AssigneeType                          string
	AssigneeID                            *uuid.UUID
	InviteToken                           *string
	DueDate                               *time.Time
	ResolvedUserIDs                       []uuid.UUID
	CreatedAt                             time.Time
}

type progressRow struct {
	CompanyID, UserID, CourseID uuid.UUID
	Status                      string
	CompletedLessonIDs          []uuid.UUID
	StartedAt, CompletedAt      *time.Time
}

func Normalize(fixtures Fixtures) (Dataset, error) {
	companyID, err := MapID(fixtures.CompanyID)
	if err != nil {
		return Dataset{}, fmt.Errorf("company.id: %w", err)
	}
	dataset := Dataset{CompanyID: companyID}

	for _, fixture := range fixtures.Courses {
		id, mapErr := MapID(fixture.ID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("course %s: %w", fixture.ID, mapErr)
		}
		authorID, mapErr := MapID(fixture.AuthorID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("course %s authorId: %w", fixture.ID, mapErr)
		}
		createdAt, mapErr := parseTimestamp(fixture.CreatedAt)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("course %s createdAt: %w", fixture.ID, mapErr)
		}
		updatedAt, mapErr := parseTimestamp(fixture.UpdatedAt)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("course %s updatedAt: %w", fixture.ID, mapErr)
		}
		if fixture.Status != "draft" && fixture.Status != "published" {
			return Dataset{}, fmt.Errorf("course %s: некорректный статус %q", fixture.ID, fixture.Status)
		}
		dataset.Courses = append(dataset.Courses, courseRow{
			ID: id, CompanyID: companyID, AuthorID: authorID,
			VersionID: seedEntityID("course-version-v1", id),
			Title:     fixture.Title, Status: fixture.Status,
			Description: fixture.Description, CoverURL: fixture.CoverURL,
			Sequential: fixture.Sequential, DeadlineDays: fixture.DeadlineDays,
			CreatedAt: createdAt, UpdatedAt: updatedAt,
		})
	}
	for _, fixture := range fixtures.CourseSections {
		id, mapErr := MapID(fixture.ID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("courseSection %s: %w", fixture.ID, mapErr)
		}
		courseID, mapErr := MapID(fixture.CourseID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("courseSection %s courseId: %w", fixture.ID, mapErr)
		}
		dataset.Sections = append(dataset.Sections, sectionRow{
			ID: id, CompanyID: companyID, CourseID: courseID,
			Title: fixture.Title, Order: fixture.Order,
		})
	}
	for _, fixture := range fixtures.Lessons {
		row, mapErr := normalizeLesson(fixture, companyID)
		if mapErr != nil {
			return Dataset{}, mapErr
		}
		dataset.Lessons = append(dataset.Lessons, row)
	}
	for _, fixture := range fixtures.Quizzes {
		id, mapErr := MapID(fixture.ID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("quiz %s: %w", fixture.ID, mapErr)
		}
		lessonID, mapErr := MapID(fixture.LessonID)
		if mapErr != nil {
			return Dataset{}, fmt.Errorf("quiz %s lessonId: %w", fixture.ID, mapErr)
		}
		questions := fixture.Questions
		if len(questions) == 0 {
			questions = json.RawMessage(`[]`)
		}
		dataset.Quizzes = append(dataset.Quizzes, quizRow{
			ID: id, CompanyID: companyID, LessonID: lessonID,
			Questions: questions, PassingScore: fixture.PassingScore,
			MaxAttempts: fixture.MaxAttempts,
		})
	}
	for _, fixture := range fixtures.Assignments {
		row, mapErr := normalizeAssignment(fixture, companyID)
		if mapErr != nil {
			return Dataset{}, mapErr
		}
		dataset.Assignments = append(dataset.Assignments, row)
	}
	for _, fixture := range fixtures.Progress {
		row, mapErr := normalizeProgress(fixture, companyID)
		if mapErr != nil {
			return Dataset{}, mapErr
		}
		dataset.Progress = append(dataset.Progress, row)
	}
	return dataset, nil
}

func normalizeLesson(fixture LessonFixture, companyID uuid.UUID) (lessonRow, error) {
	id, err := MapID(fixture.ID)
	if err != nil {
		return lessonRow{}, fmt.Errorf("lesson %s: %w", fixture.ID, err)
	}
	courseID, err := MapID(fixture.CourseID)
	if err != nil {
		return lessonRow{}, fmt.Errorf("lesson %s courseId: %w", fixture.ID, err)
	}
	sectionID, err := MapID(fixture.SectionID)
	if err != nil {
		return lessonRow{}, fmt.Errorf("lesson %s sectionId: %w", fixture.ID, err)
	}
	row := lessonRow{
		ID: id, CompanyID: companyID, CourseID: courseID, SectionID: sectionID,
		Title: fixture.Title, Order: fixture.Order,
		Content: fixture.Content, SourceMode: fixture.SourceMode,
	}
	if len(row.Content) == 0 {
		row.Content = []byte(`{"type":"doc"}`)
	}
	if fixture.SourceArticleID != nil {
		parsed, parseErr := MapID(*fixture.SourceArticleID)
		if parseErr != nil {
			return lessonRow{}, fmt.Errorf("lesson %s sourceArticleId: %w", fixture.ID, parseErr)
		}
		row.SourceArticleID = &parsed
		// The link title snapshot starts as the current lesson title.
		title := fixture.Title
		row.SourceArticleTitle = &title
	}
	if fixture.QuizID != nil {
		parsed, parseErr := MapID(*fixture.QuizID)
		if parseErr != nil {
			return lessonRow{}, fmt.Errorf("lesson %s quizId: %w", fixture.ID, parseErr)
		}
		row.QuizID = &parsed
	}
	return row, nil
}

func normalizeAssignment(fixture AssignmentFixture, companyID uuid.UUID) (assignmentRow, error) {
	id, err := MapID(fixture.ID)
	if err != nil {
		return assignmentRow{}, fmt.Errorf("assignment %s: %w", fixture.ID, err)
	}
	courseID, err := MapID(fixture.CourseID)
	if err != nil {
		return assignmentRow{}, fmt.Errorf("assignment %s courseId: %w", fixture.ID, err)
	}
	assignedByID, err := MapID(fixture.AssignedByID)
	if err != nil {
		return assignmentRow{}, fmt.Errorf("assignment %s assignedById: %w", fixture.ID, err)
	}
	createdAt, err := parseTimestamp(fixture.CreatedAt)
	if err != nil {
		return assignmentRow{}, fmt.Errorf("assignment %s createdAt: %w", fixture.ID, err)
	}
	row := assignmentRow{
		ID: id, CompanyID: companyID, CourseID: courseID, AssignedByID: assignedByID,
		AssigneeType: fixture.AssigneeType, InviteToken: fixture.InviteToken,
		ResolvedUserIDs: []uuid.UUID{}, CreatedAt: createdAt,
	}
	if fixture.AssigneeID != nil {
		parsed, parseErr := MapID(*fixture.AssigneeID)
		if parseErr != nil {
			return assignmentRow{}, fmt.Errorf("assignment %s assigneeId: %w", fixture.ID, parseErr)
		}
		row.AssigneeID = &parsed
		if fixture.AssigneeType == "user" {
			row.ResolvedUserIDs = []uuid.UUID{parsed}
		}
	}
	if fixture.DueDate != nil {
		parsed, parseErr := parseTimestamp(*fixture.DueDate)
		if parseErr != nil {
			return assignmentRow{}, fmt.Errorf("assignment %s dueDate: %w", fixture.ID, parseErr)
		}
		row.DueDate = &parsed
	}
	return row, nil
}

func normalizeProgress(fixture ProgressFixture, companyID uuid.UUID) (progressRow, error) {
	userID, err := MapID(fixture.UserID)
	if err != nil {
		return progressRow{}, fmt.Errorf("progress userId %s: %w", fixture.UserID, err)
	}
	courseID, err := MapID(fixture.CourseID)
	if err != nil {
		return progressRow{}, fmt.Errorf("progress courseId %s: %w", fixture.CourseID, err)
	}
	completed, err := mapIDList(fixture.CompletedLessonIDs)
	if err != nil {
		return progressRow{}, fmt.Errorf("progress %s completedLessonIds: %w", fixture.CourseID, err)
	}
	row := progressRow{
		CompanyID: companyID, UserID: userID, CourseID: courseID,
		Status: fixture.Status, CompletedLessonIDs: completed,
	}
	if fixture.StartedAt != nil {
		parsed, parseErr := parseTimestamp(*fixture.StartedAt)
		if parseErr != nil {
			return progressRow{}, fmt.Errorf("progress %s startedAt: %w", fixture.CourseID, parseErr)
		}
		row.StartedAt = &parsed
	}
	if fixture.CompletedAt != nil {
		parsed, parseErr := parseTimestamp(*fixture.CompletedAt)
		if parseErr != nil {
			return progressRow{}, fmt.Errorf("progress %s completedAt: %w", fixture.CourseID, parseErr)
		}
		row.CompletedAt = &parsed
	}
	return row, nil
}

func mapIDList(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		parsed, err := MapID(value)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

func MapID(value string) (uuid.UUID, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return uuid.Nil, errors.New("пустой ID")
	}
	if parsed, err := uuid.Parse(normalized); err == nil {
		return parsed, nil
	}
	return uuid.NewSHA1(fixtureNamespace, []byte(normalized)), nil
}

func parseTimestamp(value string) (time.Time, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return time.Time{}, errors.New("пустая дата")
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z", "2006-01-02"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, normalized); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("неподдерживаемый формат даты %q", value)
}

func seedEntityID(kind string, source uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(fixtureNamespace, []byte(kind+":"+source.String()))
}
