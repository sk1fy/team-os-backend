// Package coursecopy plans an independent company-owned draft from one
// immutable partner-course version. Enrollments, assignments, links,
// campaigns, analytics and restrictions are absent from the plan by design.
package coursecopy

import (
	"errors"
	"time"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
)

// EntityKind tells the application ID allocator what nested object is cloned.
type EntityKind string

const (
	EntitySection  EntityKind = "section"
	EntityLesson   EntityKind = "lesson"
	EntityQuiz     EntityKind = "quiz"
	EntityQuestion EntityKind = "question"
	EntityOption   EntityKind = "option"
	EntityFile     EntityKind = "file"
)

// IDMapper allocates an independent destination identifier. Calls are cached
// by kind and source ID, so repeated file references stay consistent.
type IDMapper func(kind EntityKind, sourceID courseversion.ID) courseversion.ID

var (
	ErrPartnerSourceRequired     = errors.New("Копировать в компанию можно только партнёрский курс")
	ErrPublishedSourceRequired   = errors.New("Копировать можно только опубликованную версию курса")
	ErrSourceVersionMismatch     = errors.New("Версия копирования принадлежит другому курсу или компании")
	ErrSourceUnavailable         = errors.New("Заблокированный или удалённый курс нельзя копировать")
	ErrDestinationCourseRequired = errors.New("Для копии требуется новый идентификатор курса")
	ErrDestinationVersionNeeded  = errors.New("Для копии требуется новый идентификатор версии")
	ErrCreatorRequired           = errors.New("Для копии требуется автор")
	ErrCreatedAtRequired         = errors.New("Для копии требуется дата создания")
	ErrIDMapperRequired          = errors.New("Для копии требуется генератор идентификаторов")
	ErrIndependentIDRequired     = errors.New("Для независимой копии требуется новый идентификатор")
	ErrDuplicateDestinationID    = errors.New("Идентификатор объекта копии повторяется")
)

// Origin is immutable provenance stored on the destination course.
type Origin struct {
	SourceCourseID  course.ID
	SourceVersionID courseversion.ID
	SourcePartnerID course.ID
}

// Params contains all externally allocated root IDs and the immutable source.
type Params struct {
	SourceCourse         course.Course
	SourceVersion        courseversion.Snapshot
	DestinationCourseID  course.ID
	DestinationVersionID courseversion.ID
	CreatedByID          course.ID
	CreatedAt            time.Time
	MapID                IDMapper
}

// Plan contains only a new company root, draft version 1 and provenance.
type Plan struct {
	Course course.Course
	Draft  *courseversion.Version
	Origin Origin
}

// CopyPartnerVersion builds a fully detached in-memory plan. The application
// persists it transactionally and uses the mapped file IDs with an idempotent
// Files clone saga.
func CopyPartnerVersion(params Params) (Plan, error) {
	if err := params.SourceCourse.Validate(); err != nil {
		return Plan{}, err
	}
	if params.SourceCourse.OwnerType != course.CourseOwnerPartner || params.SourceCourse.OwnerUserID == nil {
		return Plan{}, ErrPartnerSourceRequired
	}
	if params.SourceCourse.LifecycleStatus == course.CourseDeleted ||
		params.SourceCourse.DistributionStatus == course.DistributionBlocked {
		return Plan{}, ErrSourceUnavailable
	}
	if params.SourceVersion.Status != courseversion.StatusPublished {
		return Plan{}, ErrPublishedSourceRequired
	}
	if course.ID(params.SourceVersion.CourseID) != params.SourceCourse.ID ||
		course.ID(params.SourceVersion.CompanyID) != params.SourceCourse.CompanyID {
		return Plan{}, ErrSourceVersionMismatch
	}
	if _, err := courseversion.Rehydrate(params.SourceVersion); err != nil {
		return Plan{}, err
	}
	switch {
	case params.DestinationCourseID == "" || params.DestinationCourseID == params.SourceCourse.ID:
		return Plan{}, ErrDestinationCourseRequired
	case params.DestinationVersionID == "" || params.DestinationVersionID == params.SourceVersion.ID:
		return Plan{}, ErrDestinationVersionNeeded
	case params.CreatedByID == "":
		return Plan{}, ErrCreatorRequired
	case params.CreatedAt.IsZero():
		return Plan{}, ErrCreatedAtRequired
	case params.MapID == nil:
		return Plan{}, ErrIDMapperRequired
	}

	definition, err := remapDefinition(params.SourceVersion.Definition, params.MapID)
	if err != nil {
		return Plan{}, err
	}
	draft, err := courseversion.NewDraft(courseversion.NewDraftParams{
		ID: params.DestinationVersionID, CompanyID: courseversion.ID(params.SourceCourse.CompanyID),
		CourseID: courseversion.ID(params.DestinationCourseID), Number: 1,
		Definition: definition, CreatedByID: courseversion.ID(params.CreatedByID),
		CreatedAt: params.CreatedAt.UTC(),
	})
	if err != nil {
		return Plan{}, err
	}

	return Plan{
		Course: course.Course{
			ID: params.DestinationCourseID, CompanyID: params.SourceCourse.CompanyID,
			OwnerType: course.CourseOwnerCompany, LifecycleStatus: course.CourseActive,
			DistributionStatus: course.DistributionActive,
		},
		Draft: draft,
		Origin: Origin{
			SourceCourseID: params.SourceCourse.ID, SourceVersionID: params.SourceVersion.ID,
			SourcePartnerID: *params.SourceCourse.OwnerUserID,
		},
	}, nil
}

type remapper struct {
	mapper IDMapper
	cache  map[idKey]courseversion.ID
	used   map[courseversion.ID]idKey
}

type idKey struct {
	kind EntityKind
	id   courseversion.ID
}

func remapDefinition(source courseversion.Definition, mapper IDMapper) (courseversion.Definition, error) {
	ids := &remapper{mapper: mapper, cache: make(map[idKey]courseversion.ID), used: make(map[courseversion.ID]idKey)}
	result := source
	result.Description = cloneString(source.Description)
	result.CoverURL = cloneString(source.CoverURL)
	result.DefaultInternalDeadlineDays = cloneInt(source.DefaultInternalDeadlineDays)

	if source.CoverFileID != nil {
		mapped, err := ids.mapID(EntityFile, *source.CoverFileID)
		if err != nil {
			return courseversion.Definition{}, err
		}
		result.CoverFileID = &mapped
	}

	sectionIDs := make(map[courseversion.ID]courseversion.ID, len(source.Sections))
	result.Sections = make([]courseversion.Section, len(source.Sections))
	for index, value := range source.Sections {
		mapped, err := ids.mapID(EntitySection, value.ID)
		if err != nil {
			return courseversion.Definition{}, err
		}
		sectionIDs[value.ID] = mapped
		result.Sections[index] = value
		result.Sections[index].ID = mapped
	}

	result.Lessons = make([]courseversion.Lesson, len(source.Lessons))
	for lessonIndex, value := range source.Lessons {
		lessonID, err := ids.mapID(EntityLesson, value.ID)
		if err != nil {
			return courseversion.Definition{}, err
		}
		sectionID, exists := sectionIDs[value.SectionID]
		if !exists {
			return courseversion.Definition{}, courseversion.ErrLessonSectionMissing
		}
		lesson := value
		lesson.ID = lessonID
		lesson.SectionID = sectionID
		lesson.Content = append([]byte(nil), value.Content...)
		lesson.SourceArticleID = cloneID(value.SourceArticleID)
		lesson.SourceArticleVersion = cloneInt(value.SourceArticleVersion)
		lesson.SourceTemplateID = cloneID(value.SourceTemplateID)
		lesson.SourceTemplateVersionID = cloneID(value.SourceTemplateVersionID)
		lesson.EstimatedMinutes = cloneInt(value.EstimatedMinutes)
		if lesson.SourceType == "kb_link" {
			lesson.SourceType = "kb_snapshot"
		}
		lesson.FileIDs = make([]courseversion.ID, len(value.FileIDs))
		for fileIndex, fileID := range value.FileIDs {
			mapped, err := ids.mapID(EntityFile, fileID)
			if err != nil {
				return courseversion.Definition{}, err
			}
			lesson.FileIDs[fileIndex] = mapped
		}
		if value.Quiz != nil {
			quiz, err := remapQuiz(*value.Quiz, ids)
			if err != nil {
				return courseversion.Definition{}, err
			}
			lesson.Quiz = &quiz
		}
		result.Lessons[lessonIndex] = lesson
	}
	return result, nil
}

func remapQuiz(source courseversion.Quiz, ids *remapper) (courseversion.Quiz, error) {
	result := source
	quizID, err := ids.mapID(EntityQuiz, source.ID)
	if err != nil {
		return courseversion.Quiz{}, err
	}
	result.ID = quizID
	result.MaxAttempts = cloneInt(source.MaxAttempts)
	result.Questions = make([]courseversion.Question, len(source.Questions))
	for questionIndex, value := range source.Questions {
		questionID, err := ids.mapID(EntityQuestion, value.ID)
		if err != nil {
			return courseversion.Quiz{}, err
		}
		question := value
		question.ID = questionID
		question.Options = make([]courseversion.Option, len(value.Options))
		for optionIndex, option := range value.Options {
			optionID, err := ids.mapID(EntityOption, option.ID)
			if err != nil {
				return courseversion.Quiz{}, err
			}
			option.ID = optionID
			question.Options[optionIndex] = option
		}
		result.Questions[questionIndex] = question
	}
	return result, nil
}

func (r *remapper) mapID(kind EntityKind, source courseversion.ID) (courseversion.ID, error) {
	key := idKey{kind: kind, id: source}
	if mapped, exists := r.cache[key]; exists {
		return mapped, nil
	}
	mapped := r.mapper(kind, source)
	if source == "" || mapped == "" || mapped == source {
		return "", ErrIndependentIDRequired
	}
	if previous, exists := r.used[mapped]; exists && previous != key {
		return "", ErrDuplicateDestinationID
	}
	r.cache[key] = mapped
	r.used[mapped] = key
	return mapped, nil
}

func cloneID(value *courseversion.ID) *courseversion.ID {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
