package application

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func courseTemplateFromRow(row db.CourseTemplate) CourseTemplate {
	updatedAt := row.CreatedAt
	if row.ArchivedAt.Valid {
		updatedAt = row.ArchivedAt.Time
	}
	return CourseTemplate{
		ID: row.ID, CompanyID: row.CompanyID, Type: row.TemplateType,
		SystemTemplateKey: textPointer(row.SystemTemplateKey), LifecycleStatus: row.LifecycleStatus,
		CurrentDraftVersionID:    nullUUIDPointer(row.CurrentDraftVersionID),
		LatestPublishedVersionID: nullUUIDPointer(row.LatestPublishedVersionID),
		CreatedByID:              row.CreatedByID, CreatedAt: row.CreatedAt, UpdatedAt: updatedAt,
	}
}

func courseTemplateVersionFromRow(row db.CourseTemplateVersion) CourseTemplateVersion {
	return CourseTemplateVersion{
		ID: row.ID, CompanyID: row.CompanyID, TemplateID: row.TemplateID,
		Number: row.Number, Status: row.Status, Title: row.Title,
		Description: textPointer(row.Description), CoverFileID: nullUUIDPointer(row.CoverFileID),
		Sequential: row.Sequential, CreatedByID: row.CreatedByID, CreatedAt: row.CreatedAt,
		PublishedByID: nullUUIDPointer(row.PublishedByID), PublishedAt: timestamptzPointer(row.PublishedAt),
		ContentHash: textPointer(row.ContentHash),
	}
}

func courseTemplateVersionsFromRows(rows []db.CourseTemplateVersion) []CourseTemplateVersion {
	result := make([]CourseTemplateVersion, len(rows))
	for index := range rows {
		result[index] = courseTemplateVersionFromRow(rows[index])
	}
	return result
}

func courseTemplateSectionFromRow(row db.CourseTemplateVersionSection) CourseTemplateVersionSection {
	return CourseTemplateVersionSection{
		ID: row.ID, CompanyID: row.CompanyID, TemplateVersionID: row.TemplateVersionID,
		StableKey: row.StableKey.String(), Title: row.Title, Order: row.Order,
	}
}

func courseTemplateLessonFromRow(row db.CourseTemplateVersionLesson) CourseTemplateVersionLesson {
	return CourseTemplateVersionLesson{
		ID: row.ID, CompanyID: row.CompanyID, TemplateVersionID: row.TemplateVersionID,
		SectionVersionID: row.SectionVersionID, StableKey: row.StableKey.String(),
		Title: row.Title, Order: row.Order, Content: append(json.RawMessage(nil), row.Content...),
		SourceType: row.SourceType, EstimatedMinutes: int4Pointer(row.EstimatedMinutes),
		QuizVersionID: nullUUIDPointer(row.QuizVersionID), FileIDs: append([]uuid.UUID(nil), row.FileIds...),
	}
}

func courseTemplateQuizFromRow(row db.CourseTemplateVersionQuiz) CourseTemplateVersionQuiz {
	return CourseTemplateVersionQuiz{
		ID: row.ID, CompanyID: row.CompanyID, TemplateVersionID: row.TemplateVersionID,
		LessonVersionID: row.LessonVersionID, Questions: append(json.RawMessage(nil), row.Questions...),
		PassingScore: row.PassingScore, MaxAttempts: int4Pointer(row.MaxAttempts),
	}
}

func courseTemplateOriginFromRow(row db.CourseOrigin) CourseOrigin {
	return CourseOrigin{
		Type: row.OriginType, SourceCourseID: nullUUIDPointer(row.SourceCourseID),
		SourceCourseVersionID:   nullUUIDPointer(row.SourceCourseVersionID),
		SourcePartnerID:         nullUUIDPointer(row.SourcePartnerID),
		SourceTemplateID:        nullUUIDPointer(row.SourceTemplateID),
		SourceTemplateVersionID: nullUUIDPointer(row.SourceTemplateVersionID),
		InstantiatedByID:        row.InstantiatedByID, InstantiatedAt: row.InstantiatedAt,
		AcquisitionType: row.AcquisitionType, EntitlementID: nullUUIDPointer(row.EntitlementID),
	}
}
