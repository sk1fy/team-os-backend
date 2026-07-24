package application

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func courseFromRow(row db.Course) Course {
	result := Course{
		ID: row.ID, CompanyID: row.CompanyID, Title: row.Title,
		Description:              textPointer(row.Description),
		CoverURL:                 textPointer(row.CoverUrl),
		Status:                   row.Status,
		Visibility:               row.Visibility,
		AuthorID:                 row.AuthorID,
		OwnerType:                row.OwnerType,
		OwnerUserID:              nullUUIDPointer(row.OwnerUserID),
		CreatedByID:              row.AuthorID,
		LifecycleStatus:          row.LifecycleStatus,
		DistributionStatus:       row.DistributionStatus,
		Sequential:               row.Sequential,
		DeadlineDays:             int4Pointer(row.DeadlineDays),
		ArchivedAt:               timestamptzPointer(row.ArchivedAt),
		ArchivedByID:             nullUUIDPointer(row.ArchivedByID),
		DeletedAt:                timestamptzPointer(row.DeletedAt),
		DeletedByID:              nullUUIDPointer(row.DeletedByID),
		CurrentDraftVersionID:    nullUUIDPointer(row.CurrentDraftVersionID),
		LatestPublishedVersionID: nullUUIDPointer(row.LatestPublishedVersionID),
		CreatedAt:                row.CreatedAt,
		UpdatedAt:                row.UpdatedAt,
	}
	if row.CreatedByID.Valid {
		result.CreatedByID = row.CreatedByID.UUID
	}
	return result
}

func courseVersionFromRow(row db.CourseVersion) CourseVersion {
	return CourseVersion{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID,
		Number: row.Number, Status: row.Status, Title: row.Title,
		Description: textPointer(row.Description), CoverFileID: nullUUIDPointer(row.CoverFileID),
		CoverURL: textPointer(row.CoverUrl), Sequential: row.Sequential,
		DefaultInternalDeadlineDays: int4Pointer(row.DefaultInternalDeadlineDays),
		CreatedByID:                 row.CreatedByID, CreatedAt: row.CreatedAt,
		PublishedByID: nullUUIDPointer(row.PublishedByID), PublishedAt: timestamptzPointer(row.PublishedAt),
		ContentHash: textPointer(row.ContentHash),
	}
}

func courseVersionsFromRows(rows []db.CourseVersion) []CourseVersion {
	result := make([]CourseVersion, len(rows))
	for index := range rows {
		result[index] = courseVersionFromRow(rows[index])
	}
	return result
}

func courseVersionSectionFromRow(row db.CourseVersionSection) CourseVersionSection {
	return CourseVersionSection{
		ID: row.ID, CompanyID: row.CompanyID, CourseVersionID: row.CourseVersionID,
		StableKey: row.StableKey.String(), Title: row.Title, Order: row.Order,
	}
}

func courseVersionSectionsFromRows(rows []db.CourseVersionSection) []CourseVersionSection {
	result := make([]CourseVersionSection, len(rows))
	for index := range rows {
		result[index] = courseVersionSectionFromRow(rows[index])
	}
	return result
}

func courseVersionLessonFromRow(row db.CourseVersionLesson) CourseVersionLesson {
	return CourseVersionLesson{
		ID: row.ID, CompanyID: row.CompanyID, CourseVersionID: row.CourseVersionID,
		SectionVersionID: row.SectionVersionID, StableKey: row.StableKey.String(),
		Title: row.Title, Order: row.Order, Content: append(json.RawMessage(nil), row.Content...),
		SourceType: row.SourceType, SourceArticleID: nullUUIDPointer(row.SourceArticleID),
		SourceArticleVersion:    int4Pointer(row.SourceArticleVersion),
		SourceTemplateID:        nullUUIDPointer(row.SourceTemplateID),
		SourceTemplateVersionID: nullUUIDPointer(row.SourceTemplateVersionID),
		EstimatedMinutes:        int4Pointer(row.EstimatedMinutes), QuizVersionID: nullUUIDPointer(row.QuizVersionID),
		FileIDs: append([]uuid.UUID(nil), row.FileIds...),
	}
}

func courseVersionLessonsFromRows(rows []db.CourseVersionLesson) []CourseVersionLesson {
	result := make([]CourseVersionLesson, len(rows))
	for index := range rows {
		result[index] = courseVersionLessonFromRow(rows[index])
	}
	return result
}

func courseVersionQuizFromRow(row db.CourseVersionQuiz) CourseVersionQuiz {
	return CourseVersionQuiz{
		ID: row.ID, CompanyID: row.CompanyID, CourseVersionID: row.CourseVersionID,
		LessonVersionID: row.LessonVersionID, Questions: append(json.RawMessage(nil), row.Questions...),
		PassingScore: row.PassingScore, MaxAttempts: int4Pointer(row.MaxAttempts),
	}
}

func courseVersionQuizFromCreatedRow(row db.CreateCourseVersionQuizRow) CourseVersionQuiz {
	return CourseVersionQuiz{
		ID: row.ID, CompanyID: row.CompanyID, CourseVersionID: row.CourseVersionID,
		LessonVersionID: row.LessonVersionID, Questions: append(json.RawMessage(nil), row.Questions...),
		PassingScore: row.PassingScore, MaxAttempts: int4Pointer(row.MaxAttempts),
	}
}

func courseVersionQuizzesFromRows(rows []db.CourseVersionQuiz) []CourseVersionQuiz {
	result := make([]CourseVersionQuiz, len(rows))
	for index := range rows {
		result[index] = courseVersionQuizFromRow(rows[index])
	}
	return result
}

func coursesFromRows(rows []db.Course) []Course {
	result := make([]Course, len(rows))
	for index := range rows {
		result[index] = courseFromRow(rows[index])
	}
	return result
}

func sectionFromRow(row db.CourseSection) CourseSection {
	return CourseSection{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID,
		Title: row.Title, Order: row.Order,
	}
}

func lessonFromRow(row db.Lesson) Lesson {
	return Lesson{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID,
		SectionID: row.SectionID, Title: row.Title, Order: row.Order,
		Content:         append(json.RawMessage(nil), row.Content...),
		SourceArticleID: nullUUIDPointer(row.SourceArticleID),
		SourceMode:      textPointer(row.SourceMode),
		QuizID:          nullUUIDPointer(row.QuizID),
	}
}

func lessonsFromRows(rows []db.Lesson) []Lesson {
	result := make([]Lesson, len(rows))
	for index := range rows {
		result[index] = lessonFromRow(rows[index])
	}
	return result
}

func quizFromRow(row db.Quiz) Quiz {
	return Quiz{
		ID: row.ID, CompanyID: row.CompanyID, LessonID: row.LessonID,
		Questions:    append(json.RawMessage(nil), row.Questions...),
		PassingScore: row.PassingScore,
		MaxAttempts:  int4Pointer(row.MaxAttempts),
	}
}

func quizzesFromRows(rows []db.Quiz) []Quiz {
	result := make([]Quiz, len(rows))
	for index := range rows {
		result[index] = quizFromRow(rows[index])
	}
	return result
}

func assignmentFromGetAssignmentsRow(row db.GetAssignmentsRow) Assignment {
	versionID := row.CourseVersionID
	return Assignment{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: &versionID,
		AssigneeType: row.AssigneeType, AssigneeID: nullUUIDPointer(row.AssigneeID),
		InviteToken: textPointer(row.InviteToken), DueDate: timestamptzPointer(row.DueDate),
		AssignedByID: row.AssignedByID, CreatedAt: row.CreatedAt,
	}
}

func assignmentFromGetUserAssignmentsRow(row db.GetUserAssignmentsRow) Assignment {
	versionID := row.CourseVersionID
	return Assignment{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: &versionID,
		AssigneeType: row.AssigneeType, AssigneeID: nullUUIDPointer(row.AssigneeID),
		InviteToken: textPointer(row.InviteToken), DueDate: timestamptzPointer(row.DueDate),
		AssignedByID: row.AssignedByID, CreatedAt: row.CreatedAt,
	}
}

func assignmentFromCreateRow(row db.CreateAssignmentRow) Assignment {
	versionID := row.CourseVersionID
	return Assignment{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: &versionID,
		AssigneeType: row.AssigneeType, AssigneeID: nullUUIDPointer(row.AssigneeID),
		InviteToken: textPointer(row.InviteToken), DueDate: timestamptzPointer(row.DueDate),
		AssignedByID: row.AssignedByID, CreatedAt: row.CreatedAt,
	}
}

func assignmentFromTargetRow(row db.GetAssignmentByTargetRow) Assignment {
	versionID := row.CourseVersionID
	return Assignment{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: &versionID,
		AssigneeType: row.AssigneeType, AssigneeID: nullUUIDPointer(row.AssigneeID),
		InviteToken: textPointer(row.InviteToken), DueDate: timestamptzPointer(row.DueDate),
		AssignedByID: row.AssignedByID, CreatedAt: row.CreatedAt,
	}
}

func progressFromRow(row db.Progress, attempts []QuizAttempt) Progress {
	if attempts == nil {
		attempts = []QuizAttempt{}
	}
	return Progress{
		UserID: row.UserID, CourseID: row.CourseID, Status: row.Status,
		CompletedLessonIDs: append([]uuid.UUID{}, row.CompletedLessonIds...),
		QuizAttempts:       attempts,
		StartedAt:          timestamptzPointer(row.StartedAt),
		CompletedAt:        timestamptzPointer(row.CompletedAt),
	}
}

func enrollmentFromRow(row db.CourseEnrollment) Enrollment {
	return Enrollment{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: row.CourseVersionID,
		LearnerType: row.LearnerType, UserID: nullUUIDPointer(row.UserID), ExternalLearnerID: nullUUIDPointer(row.ExternalLearnerID),
		SourceType: row.SourceType, SourceID: nullUUIDPointer(row.SourceID), AttemptNumber: row.AttemptNumber,
		ProgressStatus: row.ProgressStatus, AccessStatus: row.AccessStatus,
		CurrentLessonVersionID: nullUUIDPointer(row.CurrentLessonVersionID), ActivatedAt: timestamptzPointer(row.ActivatedAt),
		AccessUntil: timestamptzPointer(row.AccessUntil), StartedAt: timestamptzPointer(row.StartedAt),
		CompletedAt: timestamptzPointer(row.CompletedAt), LastActivityAt: timestamptzPointer(row.LastActivityAt),
		FrozenAt: timestamptzPointer(row.FrozenAt), SuspendedAt: timestamptzPointer(row.SuspendedAt),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func enrollmentFromResumeRow(row db.GetEnrollmentResumeRow) Enrollment {
	result := Enrollment{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: row.CourseVersionID,
		LearnerType: row.LearnerType, UserID: nullUUIDPointer(row.UserID), ExternalLearnerID: nullUUIDPointer(row.ExternalLearnerID),
		SourceType: row.SourceType, SourceID: nullUUIDPointer(row.SourceID), AttemptNumber: row.AttemptNumber,
		ProgressStatus: row.ProgressStatus, AccessStatus: row.AccessStatus,
		CurrentLessonVersionID: nullUUIDPointer(row.CurrentLessonVersionID), ProgressPercent: row.ProgressPercent,
		ActivatedAt: timestamptzPointer(row.ActivatedAt), AccessUntil: timestamptzPointer(row.AccessUntil),
		StartedAt: timestamptzPointer(row.StartedAt), CompletedAt: timestamptzPointer(row.CompletedAt),
		LastActivityAt: timestamptzPointer(row.LastActivityAt), FrozenAt: timestamptzPointer(row.FrozenAt),
		SuspendedAt: timestamptzPointer(row.SuspendedAt), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	result.VersionNumber = row.VersionNumber
	return result
}

func enrollmentFromListRow(row db.ListInternalEnrollmentsRow) Enrollment {
	result := Enrollment{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID, CourseVersionID: row.CourseVersionID,
		VersionNumber: row.VersionNumber, LearnerType: row.LearnerType, UserID: nullUUIDPointer(row.UserID),
		ExternalLearnerID: nullUUIDPointer(row.ExternalLearnerID), SourceType: row.SourceType,
		SourceID: nullUUIDPointer(row.SourceID), AttemptNumber: row.AttemptNumber,
		ProgressStatus: row.ProgressStatus, AccessStatus: row.AccessStatus,
		CurrentLessonVersionID: nullUUIDPointer(row.CurrentLessonVersionID), ProgressPercent: row.ProgressPercent,
		ActivatedAt: timestamptzPointer(row.ActivatedAt), AccessUntil: timestamptzPointer(row.AccessUntil),
		StartedAt: timestamptzPointer(row.StartedAt), CompletedAt: timestamptzPointer(row.CompletedAt),
		LastActivityAt: timestamptzPointer(row.LastActivityAt), FrozenAt: timestamptzPointer(row.FrozenAt),
		SuspendedAt: timestamptzPointer(row.SuspendedAt), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	result.CourseTitle = &row.CourseTitle
	result.CourseCoverURL = textPointer(row.CourseCoverUrl)
	completedLessons, totalLessons := row.CompletedLessonCount, row.TotalLessonCount
	result.CompletedLessonCount = &completedLessons
	result.TotalLessonCount = &totalLessons
	result.DueDate = timestamptzPointer(row.DueDate)
	result.Overdue, _ = row.Overdue.(bool)
	return result
}

func enrollmentFromInternalReportRow(row db.ListInternalEnrollmentReportRowsRow) Enrollment {
	result := Enrollment{
		ID: row.ID, CompanyID: row.CompanyID, CourseID: row.CourseID,
		CourseVersionID: row.CourseVersionID, VersionNumber: row.VersionNumber,
		LearnerType: row.LearnerType, UserID: nullUUIDPointer(row.UserID),
		ExternalLearnerID: nullUUIDPointer(row.ExternalLearnerID),
		SourceType:        row.SourceType, SourceID: nullUUIDPointer(row.SourceID),
		AttemptNumber: row.AttemptNumber, ProgressStatus: row.ProgressStatus,
		AccessStatus:           row.AccessStatus,
		CurrentLessonVersionID: nullUUIDPointer(row.CurrentLessonVersionID),
		ProgressPercent:        row.ProgressPercent,
		ActivatedAt:            timestamptzPointer(row.ActivatedAt),
		AccessUntil:            timestamptzPointer(row.AccessUntil),
		StartedAt:              timestamptzPointer(row.StartedAt),
		CompletedAt:            timestamptzPointer(row.CompletedAt),
		LastActivityAt:         timestamptzPointer(row.LastActivityAt),
		FrozenAt:               timestamptzPointer(row.FrozenAt),
		SuspendedAt:            timestamptzPointer(row.SuspendedAt),
		CreatedAt:              row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	result.CourseTitle = &row.CourseTitle
	result.CourseCoverURL = textPointer(row.CourseCoverUrl)
	completedLessons, totalLessons := row.CompletedLessonCount, row.TotalLessonCount
	result.CompletedLessonCount = &completedLessons
	result.TotalLessonCount = &totalLessons
	result.DueDate = timestamptzPointer(row.DueDate)
	result.Overdue, _ = row.Overdue.(bool)
	return result
}

func enrollmentLessonProgressFromRow(row db.EnrollmentLessonProgress) EnrollmentLessonProgress {
	return EnrollmentLessonProgress{
		CompanyID: row.CompanyID, EnrollmentID: row.EnrollmentID, LessonVersionID: row.LessonVersionID,
		Status: row.Status, FirstOpenedAt: timestamptzPointer(row.FirstOpenedAt), CompletedAt: timestamptzPointer(row.CompletedAt),
		ActiveSeconds: row.ActiveSeconds, LastPosition: textPointer(row.LastPosition),
	}
}

func enrollmentQuizAttemptFromListRow(row db.ListEnrollmentQuizAttemptsRow) EnrollmentQuizAttempt {
	return EnrollmentQuizAttempt{
		ID: row.ID, CompanyID: row.CompanyID, EnrollmentID: row.EnrollmentID, QuizVersionID: row.QuizVersionID,
		Answers: append(json.RawMessage(nil), row.Answers...), Score: row.Score, Passed: row.Passed,
		PendingReview: row.PendingReview, ReviewedByID: nullUUIDPointer(row.ReviewedByID),
		ReviewedAt: timestamptzPointer(row.ReviewedAt), ReviewComment: textPointer(row.ReviewComment), CreatedAt: row.CreatedAt,
	}
}

func enrollmentQuizAttemptFromCreateRow(row db.CreateEnrollmentQuizAttemptRow) EnrollmentQuizAttempt {
	return EnrollmentQuizAttempt{
		ID: row.ID, CompanyID: row.CompanyID, EnrollmentID: row.EnrollmentID, QuizVersionID: row.QuizVersionID,
		Answers: append(json.RawMessage(nil), row.Answers...), Score: row.Score, Passed: row.Passed,
		PendingReview: row.PendingReview, ReviewedByID: nullUUIDPointer(row.ReviewedByID),
		ReviewedAt: timestamptzPointer(row.ReviewedAt), ReviewComment: textPointer(row.ReviewComment), CreatedAt: row.CreatedAt,
	}
}

func enrollmentQuizAttemptFromReviewRow(row db.ReviewEnrollmentQuizAttemptRow) EnrollmentQuizAttempt {
	return EnrollmentQuizAttempt{
		ID: row.ID, CompanyID: row.CompanyID, EnrollmentID: row.EnrollmentID, QuizVersionID: row.QuizVersionID,
		Answers: append(json.RawMessage(nil), row.Answers...), Score: row.Score, Passed: row.Passed,
		PendingReview: row.PendingReview, ReviewedByID: nullUUIDPointer(row.ReviewedByID),
		ReviewedAt: timestamptzPointer(row.ReviewedAt), ReviewComment: textPointer(row.ReviewComment), CreatedAt: row.CreatedAt,
	}
}

func textPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func nullText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func int4Pointer(value pgtype.Int4) *int32 {
	if !value.Valid {
		return nil
	}
	result := value.Int32
	return &result
}

func boolNull(value *bool) pgtype.Bool {
	if value == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *value, Valid: true}
}

func nullInt4(value *int32) pgtype.Int4 {
	if value == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *value, Valid: true}
}

func nullUUIDPointer(value uuid.NullUUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	result := value.UUID
	return &result
}

func nullUUID(value *uuid.UUID) uuid.NullUUID {
	if value == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *value, Valid: true}
}

func timestamptzPointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func nullTimestamptz(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
