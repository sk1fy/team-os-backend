package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/pkg/richtext"
	domainversion "github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
	domaintemplate "github.com/sk1fy/team-os-backend/services/academy/internal/domain/template"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func (s *Service) loadCourseTemplateVersionDetails(
	ctx context.Context,
	queries *db.Queries,
	row db.CourseTemplateVersion,
) (CourseTemplateVersionDetails, error) {
	sections, err := queries.ListCourseTemplateVersionSections(ctx, db.ListCourseTemplateVersionSectionsParams{
		CompanyID: row.CompanyID, TemplateVersionID: row.ID,
	})
	if err != nil {
		return CourseTemplateVersionDetails{}, internal("Не удалось получить разделы шаблона", err)
	}
	lessonRows, err := queries.ListCourseTemplateVersionLessons(ctx, db.ListCourseTemplateVersionLessonsParams{
		CompanyID: row.CompanyID, TemplateVersionID: row.ID,
	})
	if err != nil {
		return CourseTemplateVersionDetails{}, internal("Не удалось получить уроки шаблона", err)
	}
	quizRows, err := queries.ListCourseTemplateVersionQuizzes(ctx, db.ListCourseTemplateVersionQuizzesParams{
		CompanyID: row.CompanyID, TemplateVersionID: row.ID,
	})
	if err != nil {
		return CourseTemplateVersionDetails{}, internal("Не удалось получить тесты шаблона", err)
	}
	lessons := make([]CourseTemplateVersionLesson, len(lessonRows))
	for index, lessonRow := range lessonRows {
		lessons[index] = courseTemplateLessonFromRow(lessonRow)
		if !lessonRow.KbSnapshotID.Valid {
			continue
		}
		snapshot, snapshotErr := queries.GetKBArticleSnapshot(ctx, db.GetKBArticleSnapshotParams{
			CompanyID: row.CompanyID, ID: lessonRow.KbSnapshotID.UUID,
		})
		if snapshotErr != nil {
			return CourseTemplateVersionDetails{}, internal("Не удалось получить снимок статьи базы знаний", snapshotErr)
		}
		articleID := snapshot.SourceArticleID
		version := snapshot.SourceArticleVersionNumber.Int32
		lessons[index].SourceArticleID = &articleID
		lessons[index].SourceArticleVersion = &version
	}
	quizzes := make([]CourseTemplateVersionQuiz, len(quizRows))
	for index := range quizRows {
		quizzes[index] = courseTemplateQuizFromRow(quizRows[index])
	}
	convertedSections := make([]CourseTemplateVersionSection, len(sections))
	for index := range sections {
		convertedSections[index] = courseTemplateSectionFromRow(sections[index])
	}
	return CourseTemplateVersionDetails{
		Version: courseTemplateVersionFromRow(row),
		Content: CourseTemplateVersionContent{
			Sections: convertedSections, Lessons: lessons, Quizzes: quizzes,
		},
	}, nil
}

func templateSnapshotFromRow(row db.CourseTemplate) domaintemplate.Snapshot {
	var systemKey *string
	if row.SystemTemplateKey.Valid {
		value := row.SystemTemplateKey.String
		systemKey = &value
	}
	return domaintemplate.Snapshot{
		ID: domaintemplate.ID(row.ID.String()), CompanyID: domaintemplate.ID(row.CompanyID.String()),
		Type: domaintemplate.Type(row.TemplateType), SystemTemplateKey: systemKey,
		LifecycleStatus:          domaintemplate.LifecycleStatus(row.LifecycleStatus),
		CurrentDraftVersionID:    optionalTemplateID(nullUUIDPointer(row.CurrentDraftVersionID)),
		LatestPublishedVersionID: optionalTemplateID(nullUUIDPointer(row.LatestPublishedVersionID)),
		CreatedByID:              domaintemplate.ID(row.CreatedByID.String()), CreatedAt: row.CreatedAt,
	}
}

func optionalTemplateID(value *uuid.UUID) *domaintemplate.ID {
	if value == nil {
		return nil
	}
	converted := domaintemplate.ID(value.String())
	return &converted
}

func domainTemplateVersion(details CourseTemplateVersionDetails) (*domaintemplate.Version, error) {
	quizzes := make(map[uuid.UUID]CourseTemplateVersionQuiz, len(details.Content.Quizzes))
	for _, quiz := range details.Content.Quizzes {
		quizzes[quiz.ID] = quiz
	}
	sections := make([]domainversion.Section, len(details.Content.Sections))
	for index, section := range details.Content.Sections {
		sections[index] = domainversion.Section{
			ID: domainversion.ID(section.ID.String()), StableKey: section.StableKey,
			Title: section.Title, Order: int(section.Order),
		}
	}
	lessons := make([]domainversion.Lesson, len(details.Content.Lessons))
	for index, lesson := range details.Content.Lessons {
		converted := domainversion.Lesson{
			ID: domainversion.ID(lesson.ID.String()), SectionID: domainversion.ID(lesson.SectionVersionID.String()),
			StableKey: lesson.StableKey, Title: lesson.Title, Order: int(lesson.Order),
			Content: append(json.RawMessage(nil), lesson.Content...), SourceType: lesson.SourceType,
			SourceArticleID:      optionalDomainID(lesson.SourceArticleID),
			SourceArticleVersion: optionalInt(lesson.SourceArticleVersion),
			EstimatedMinutes:     optionalInt(lesson.EstimatedMinutes),
		}
		for _, fileID := range lesson.FileIDs {
			converted.FileIDs = append(converted.FileIDs, domainversion.ID(fileID.String()))
		}
		if lesson.QuizVersionID != nil {
			quiz, exists := quizzes[*lesson.QuizVersionID]
			if exists {
				convertedQuiz, err := domainTemplateQuiz(quiz)
				if err != nil {
					return nil, err
				}
				converted.Quiz = &convertedQuiz
			}
		}
		lessons[index] = converted
	}
	definition := domainversion.Definition{
		Title: details.Version.Title, Description: details.Version.Description,
		CoverFileID: optionalDomainID(details.Version.CoverFileID), Sequential: details.Version.Sequential,
		Sections: sections, Lessons: lessons,
	}
	return domaintemplate.RehydrateVersion(domaintemplate.VersionSnapshot{
		ID:         domaintemplate.ID(details.Version.ID.String()),
		CompanyID:  domaintemplate.ID(details.Version.CompanyID.String()),
		TemplateID: domaintemplate.ID(details.Version.TemplateID.String()),
		Number:     int(details.Version.Number), Status: domaintemplate.VersionStatus(details.Version.Status),
		Definition: definition, CreatedByID: domaintemplate.ID(details.Version.CreatedByID.String()),
		CreatedAt:     details.Version.CreatedAt,
		PublishedByID: optionalTemplateID(details.Version.PublishedByID),
		PublishedAt:   details.Version.PublishedAt, ContentHash: optionalString(details.Version.ContentHash),
	})
}

func domainTemplateQuiz(value CourseTemplateVersionQuiz) (domainversion.Quiz, error) {
	return domainQuiz(CourseVersionQuiz{
		ID: value.ID, CompanyID: value.CompanyID, CourseVersionID: value.TemplateVersionID,
		LessonVersionID: value.LessonVersionID, Questions: value.Questions,
		PassingScore: value.PassingScore, MaxAttempts: value.MaxAttempts,
	})
}

func (s *Service) replaceCourseTemplateDraftContent(
	ctx context.Context,
	queries *db.Queries,
	actor Actor,
	version db.CourseTemplateVersion,
	input CourseTemplateDraftContentInput,
) error {
	current, err := s.loadCourseTemplateVersionDetails(ctx, queries, version)
	if err != nil {
		return err
	}
	for _, quiz := range current.Content.Quizzes {
		if _, err = queries.DeleteCourseTemplateVersionQuiz(ctx, db.DeleteCourseTemplateVersionQuizParams{
			CompanyID: actor.CompanyID, ID: quiz.ID,
		}); err != nil {
			return internal("Не удалось заменить тесты шаблона", err)
		}
	}
	for _, lesson := range current.Content.Lessons {
		if _, err = queries.DeleteCourseTemplateVersionLesson(ctx, db.DeleteCourseTemplateVersionLessonParams{
			CompanyID: actor.CompanyID, ID: lesson.ID,
		}); err != nil {
			return internal("Не удалось заменить уроки шаблона", err)
		}
	}
	for _, section := range current.Content.Sections {
		if _, err = queries.DeleteCourseTemplateVersionSection(ctx, db.DeleteCourseTemplateVersionSectionParams{
			CompanyID: actor.CompanyID, ID: section.ID,
		}); err != nil {
			return internal("Не удалось заменить разделы шаблона", err)
		}
	}

	sectionIDs := make(map[string]uuid.UUID, len(input.Sections))
	for _, section := range input.Sections {
		stableKey, parseErr := uuid.Parse(strings.TrimSpace(section.StableKey))
		if parseErr != nil {
			return validation("Некорректный стабильный идентификатор раздела шаблона")
		}
		title, titleErr := requiredText(section.Title, "Укажите название раздела шаблона")
		if titleErr != nil {
			return titleErr
		}
		if _, duplicate := sectionIDs[stableKey.String()]; duplicate {
			return validation("Стабильный идентификатор раздела шаблона указан несколько раз")
		}
		sectionID := uuid.New()
		if _, createErr := queries.CreateCourseTemplateVersionSection(ctx, db.CreateCourseTemplateVersionSectionParams{
			ID: sectionID, StableKey: stableKey, Title: title, OrderValue: section.Order,
			CompanyID: actor.CompanyID, TemplateVersionID: version.ID,
		}); createErr != nil {
			return internal("Не удалось создать раздел шаблона", createErr)
		}
		sectionIDs[stableKey.String()] = sectionID
	}
	lessonKeys := make(map[string]struct{})
	for _, section := range input.Sections {
		sectionID := sectionIDs[strings.TrimSpace(section.StableKey)]
		for _, lesson := range section.Lessons {
			stableKey, parseErr := uuid.Parse(strings.TrimSpace(lesson.StableKey))
			if parseErr != nil {
				return validation("Некорректный стабильный идентификатор урока шаблона")
			}
			if _, duplicate := lessonKeys[stableKey.String()]; duplicate {
				return validation("Стабильный идентификатор урока шаблона указан несколько раз")
			}
			lessonKeys[stableKey.String()] = struct{}{}
			title, titleErr := requiredText(lesson.Title, "Укажите название урока шаблона")
			if titleErr != nil {
				return titleErr
			}
			content := lesson.Content
			if len(content) == 0 {
				content = json.RawMessage(`{"type":"doc"}`)
			}
			if err = richtext.Validate(content); err != nil {
				return validation("Содержимое урока должно быть TipTap JSON")
			}
			sourceType := strings.TrimSpace(lesson.SourceType)
			if sourceType == "" {
				sourceType = "manual"
			}
			if sourceType != "manual" {
				return validation("В шаблоне компании поддерживаются только самостоятельные снимки уроков")
			}
			fileIDs, fileErr := fileUUIDsFromRichText(content)
			if fileErr != nil {
				return fileErr
			}
			lessonID := uuid.New()
			created, createErr := queries.CreateCourseTemplateVersionLesson(ctx, db.CreateCourseTemplateVersionLessonParams{
				ID: lessonID, StableKey: stableKey, Title: title, OrderValue: lesson.Order,
				Content: content, SourceType: sourceType, EstimatedMinutes: nullInt4(lesson.EstimatedMinutes),
				FileIds: fileIDs, SectionVersionID: sectionID,
				CompanyID: actor.CompanyID, TemplateVersionID: version.ID,
			})
			if createErr != nil {
				return internal("Не удалось создать урок шаблона", createErr)
			}
			if lesson.Quiz != nil {
				if !json.Valid(lesson.Quiz.Questions) {
					return validation("Некорректные вопросы теста шаблона")
				}
				if _, quizErr := queries.UpsertCourseTemplateVersionQuiz(ctx, db.UpsertCourseTemplateVersionQuizParams{
					CompanyID: actor.CompanyID, LessonVersionID: created.ID, ID: uuid.New(),
					Questions: lesson.Quiz.Questions, PassingScore: lesson.Quiz.PassingScore,
					MaxAttempts: nullInt4(lesson.Quiz.MaxAttempts),
				}); quizErr != nil {
					return internal("Не удалось создать тест шаблона", quizErr)
				}
			}
		}
	}
	return nil
}

func fileUUIDsFromRichText(content json.RawMessage) ([]uuid.UUID, error) {
	ids := richtext.FileIDs(content)
	result := make([]uuid.UUID, 0, len(ids))
	for _, rawID := range ids {
		id, err := uuid.Parse(rawID)
		if err != nil {
			return nil, validation(fmt.Sprintf("Некорректный идентификатор файла %q", rawID))
		}
		result = append(result, id)
	}
	return result, nil
}
