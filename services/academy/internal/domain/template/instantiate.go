package template

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
)

// EntityKind tells the application which independent nested ID to allocate.
type EntityKind string

const (
	EntitySection  EntityKind = "section"
	EntityLesson   EntityKind = "lesson"
	EntityQuiz     EntityKind = "quiz"
	EntityQuestion EntityKind = "question"
	EntityOption   EntityKind = "option"
	EntityFile     EntityKind = "file"
)

// IDMapper allocates destination IDs. Results are cached by source entity so
// repeated references to one file keep one cloned destination ID.
type IDMapper func(kind EntityKind, sourceID courseversion.ID) courseversion.ID

// OriginType records immutable provenance without coupling the copy to its
// source aggregate.
type OriginType string

const (
	OriginSystemTemplate  OriginType = "system_template"
	OriginCompanyTemplate OriginType = "company_template"
)

var (
	ErrPublishedVersionRequired = errors.New("Применить можно только опубликованную версию шаблона")
	ErrDestinationCourseNeeded  = errors.New("Для курса из шаблона требуется новый идентификатор")
	ErrDestinationVersionNeeded = errors.New("Для курса из шаблона требуется новый идентификатор версии")
	ErrTargetOwnerInvalid       = errors.New("Некорректный владелец курса из шаблона")
	ErrIDMapperRequired         = errors.New("Для применения шаблона требуется генератор идентификаторов")
	ErrIndependentIDRequired    = errors.New("Для независимой копии шаблона требуется новый идентификатор")
	ErrDuplicateDestinationID   = errors.New("Идентификатор объекта из шаблона повторяется")
)

// InstantiationParams contains pre-authorized target ownership and externally
// allocated root IDs. Authorization forces owner/admin to company and partner
// to partner/self before entering this pure operation.
type InstantiationParams struct {
	SourceTemplate      Snapshot
	SourceVersion       VersionSnapshot
	DestinationCourseID course.ID
	DestinationDraftID  courseversion.ID
	TargetOwnerType     course.OwnerType
	TargetOwnerUserID   *course.ID
	CreatedByID         course.ID
	CreatedAt           time.Time
	MapID               IDMapper
}

// InstantiationOrigin is stored on the new course for traceability only.
type InstantiationOrigin struct {
	Type                    OriginType
	SourceTemplateID        ID
	SourceTemplateVersionID ID
}

// InstantiationPlan contains no assignments, learners, progress, campaigns,
// links, reports, or restrictions by construction.
type InstantiationPlan struct {
	Course course.Course
	Draft  *courseversion.Version
	Origin InstantiationOrigin
}

// Instantiate creates a fully detached draft course from one immutable
// published template version.
func Instantiate(params InstantiationParams) (InstantiationPlan, error) {
	if err := params.SourceTemplate.Validate(); err != nil {
		return InstantiationPlan{}, err
	}
	if params.SourceTemplate.LifecycleStatus != LifecycleActive {
		return InstantiationPlan{}, ErrTemplateArchived
	}
	if params.SourceVersion.Status != VersionPublished {
		return InstantiationPlan{}, ErrPublishedVersionRequired
	}
	if err := validateVersionScope(params.SourceTemplate, params.SourceVersion); err != nil {
		return InstantiationPlan{}, err
	}
	if _, err := RehydrateVersion(params.SourceVersion); err != nil {
		return InstantiationPlan{}, err
	}
	switch {
	case params.DestinationCourseID == "" || params.DestinationCourseID == course.ID(params.SourceTemplate.ID):
		return InstantiationPlan{}, ErrDestinationCourseNeeded
	case params.DestinationDraftID == "" || ID(params.DestinationDraftID) == params.SourceVersion.ID:
		return InstantiationPlan{}, ErrDestinationVersionNeeded
	case params.CreatedByID == "":
		return InstantiationPlan{}, ErrCreatorRequired
	case params.CreatedAt.IsZero():
		return InstantiationPlan{}, ErrCreatedAtRequired
	case params.MapID == nil:
		return InstantiationPlan{}, ErrIDMapperRequired
	}
	if !validTargetOwner(params.TargetOwnerType, params.TargetOwnerUserID, params.CreatedByID) {
		return InstantiationPlan{}, ErrTargetOwnerInvalid
	}

	definition, err := instantiateDefinition(params.SourceTemplate, params.SourceVersion, params.MapID)
	if err != nil {
		return InstantiationPlan{}, err
	}
	draft, err := courseversion.NewDraft(courseversion.NewDraftParams{
		ID:        params.DestinationDraftID,
		CompanyID: courseversion.ID(params.SourceTemplate.CompanyID),
		CourseID:  courseversion.ID(params.DestinationCourseID),
		Number:    1, Definition: definition,
		CreatedByID: courseversion.ID(params.CreatedByID), CreatedAt: params.CreatedAt.UTC(),
	})
	if err != nil {
		return InstantiationPlan{}, err
	}

	ownerUserID := cloneCourseID(params.TargetOwnerUserID)
	return InstantiationPlan{
		Course: course.Course{
			ID: params.DestinationCourseID, CompanyID: course.ID(params.SourceTemplate.CompanyID),
			OwnerType: params.TargetOwnerType, OwnerUserID: ownerUserID,
			LifecycleStatus: course.CourseActive, DistributionStatus: course.DistributionActive,
		},
		Draft: draft,
		Origin: InstantiationOrigin{
			Type:                    originType(params.SourceTemplate.Type),
			SourceTemplateID:        params.SourceTemplate.ID,
			SourceTemplateVersionID: params.SourceVersion.ID,
		},
	}, nil
}

func validTargetOwner(ownerType course.OwnerType, ownerUserID *course.ID, creatorID course.ID) bool {
	switch ownerType {
	case course.CourseOwnerCompany:
		return ownerUserID == nil
	case course.CourseOwnerPartner:
		return ownerUserID != nil && *ownerUserID != "" && *ownerUserID == creatorID
	default:
		return false
	}
}

func originType(templateType Type) OriginType {
	if templateType == TypeSystem {
		return OriginSystemTemplate
	}
	return OriginCompanyTemplate
}

type mappedIDKey struct {
	kind EntityKind
	id   courseversion.ID
}

type instantiationRemapper struct {
	mapper IDMapper
	cache  map[mappedIDKey]courseversion.ID
	used   map[courseversion.ID]mappedIDKey
}

func instantiateDefinition(root Snapshot, source VersionSnapshot, mapper IDMapper) (courseversion.Definition, error) {
	ids := &instantiationRemapper{
		mapper: mapper,
		cache:  make(map[mappedIDKey]courseversion.ID),
		used:   make(map[courseversion.ID]mappedIDKey),
	}
	definition := cloneDefinition(source.Definition)
	if source.Definition.CoverFileID != nil {
		mapped, err := ids.mapID(EntityFile, *source.Definition.CoverFileID)
		if err != nil {
			return courseversion.Definition{}, err
		}
		definition.CoverFileID = &mapped
	}

	sectionIDs := make(map[courseversion.ID]courseversion.ID, len(source.Definition.Sections))
	for index, section := range source.Definition.Sections {
		mapped, err := ids.mapID(EntitySection, section.ID)
		if err != nil {
			return courseversion.Definition{}, err
		}
		sectionIDs[section.ID] = mapped
		definition.Sections[index].ID = mapped
	}

	templateID := courseversion.ID(root.ID)
	templateVersionID := courseversion.ID(source.ID)
	for lessonIndex, lesson := range source.Definition.Lessons {
		mappedLessonID, err := ids.mapID(EntityLesson, lesson.ID)
		if err != nil {
			return courseversion.Definition{}, err
		}
		mappedSectionID, exists := sectionIDs[lesson.SectionID]
		if !exists {
			return courseversion.Definition{}, courseversion.ErrLessonSectionMissing
		}
		destination := &definition.Lessons[lessonIndex]
		destination.ID = mappedLessonID
		destination.SectionID = mappedSectionID
		destination.SourceType = "template_snapshot"
		destination.SourceArticleID = nil
		destination.SourceArticleVersion = nil
		destination.SourceTemplateID = &templateID
		destination.SourceTemplateVersionID = &templateVersionID
		destination.Content = append(json.RawMessage(nil), lesson.Content...)
		for fileIndex, fileID := range lesson.FileIDs {
			mapped, mapErr := ids.mapID(EntityFile, fileID)
			if mapErr != nil {
				return courseversion.Definition{}, mapErr
			}
			destination.FileIDs[fileIndex] = mapped
		}
		if lesson.Quiz != nil {
			if err := remapTemplateQuiz(destination.Quiz, ids); err != nil {
				return courseversion.Definition{}, err
			}
		}
	}
	return definition, nil
}

func remapTemplateQuiz(quiz *courseversion.Quiz, ids *instantiationRemapper) error {
	quizID, err := ids.mapID(EntityQuiz, quiz.ID)
	if err != nil {
		return err
	}
	quiz.ID = quizID
	for questionIndex := range quiz.Questions {
		question := &quiz.Questions[questionIndex]
		questionID, mapErr := ids.mapID(EntityQuestion, question.ID)
		if mapErr != nil {
			return mapErr
		}
		question.ID = questionID
		for optionIndex := range question.Options {
			option := &question.Options[optionIndex]
			optionID, optionErr := ids.mapID(EntityOption, option.ID)
			if optionErr != nil {
				return optionErr
			}
			option.ID = optionID
		}
	}
	return nil
}

func (r *instantiationRemapper) mapID(kind EntityKind, sourceID courseversion.ID) (courseversion.ID, error) {
	key := mappedIDKey{kind: kind, id: sourceID}
	if mapped, exists := r.cache[key]; exists {
		return mapped, nil
	}
	mapped := r.mapper(kind, sourceID)
	if sourceID == "" || mapped == "" || mapped == sourceID {
		return "", ErrIndependentIDRequired
	}
	if previous, exists := r.used[mapped]; exists && previous != key {
		return "", ErrDuplicateDestinationID
	}
	r.cache[key] = mapped
	r.used[mapped] = key
	return mapped, nil
}

func cloneCourseID(value *course.ID) *course.ID {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
