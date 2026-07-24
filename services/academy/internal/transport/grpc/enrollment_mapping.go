package grpc

import (
	"encoding/json"
	"fmt"

	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func catalogCardToProto(card application.CatalogCard) *academyv1.CatalogCourseCard {
	return &academyv1.CatalogCourseCard{
		Id: card.ID.String(), Title: card.Title, Description: card.Description, CoverUrl: card.CoverURL,
		LessonCount: card.LessonCount, EstimatedMinutes: card.EstimatedMinutes,
		LatestVersionNumber: card.LatestVersionNumber, Enrolled: card.Enrolled,
		EnrollmentId: optionalUUIDString(card.EnrollmentID), ProgressPercent: card.ProgressPercent,
	}
}

func catalogPageToProto(page application.CatalogPage) *academyv1.GetAcademyCatalogResponse {
	items := make([]*academyv1.CatalogCourseCard, len(page.Items))
	for index := range page.Items {
		items[index] = catalogCardToProto(page.Items[index])
	}
	total := page.Total
	if total < 0 {
		total = 0
	}
	return &academyv1.GetAcademyCatalogResponse{
		Items: items, Page: uint32(max(0, page.Page)), PageSize: uint32(max(0, page.PageSize)), Total: uint32(total),
	}
}

func enrollmentToProto(value application.Enrollment) *academyv1.CourseEnrollment {
	result := &academyv1.CourseEnrollment{
		Id: value.ID.String(), CompanyId: value.CompanyID.String(), CourseId: value.CourseID.String(),
		CourseVersionId: value.CourseVersionID.String(), LearnerType: enrollmentLearnerTypeToProto(value.LearnerType),
		UserId: optionalUUIDString(value.UserID), ExternalLearnerId: optionalUUIDString(value.ExternalLearnerID),
		SourceType: enrollmentSourceTypeToProto(value.SourceType), SourceId: optionalUUIDString(value.SourceID),
		AttemptNumber: uint32(max(0, value.AttemptNumber)), ProgressStatus: enrollmentProgressStatusToProto(value.ProgressStatus),
		AccessStatus: enrollmentAccessStatusToProto(value.AccessStatus), CurrentLessonVersionId: optionalUUIDString(value.CurrentLessonVersionID),
		ProgressPercent: uint32(max(0, value.ProgressPercent)), CreatedAt: timestamppb.New(value.CreatedAt.UTC()),
		UpdatedAt: timestamppb.New(value.UpdatedAt.UTC()), Overdue: value.Overdue,
		CourseTitle: value.CourseTitle, CourseCoverUrl: value.CourseCoverURL,
	}
	if value.CompletedLessonCount != nil {
		completed := uint32(max(0, *value.CompletedLessonCount))
		result.CompletedLessonCount = &completed
	}
	if value.TotalLessonCount != nil {
		total := uint32(max(0, *value.TotalLessonCount))
		result.TotalLessonCount = &total
	}
	if value.DueDate != nil {
		result.DueDate = timestamppb.New(value.DueDate.UTC())
	}
	if value.ActivatedAt != nil {
		result.ActivatedAt = timestamppb.New(value.ActivatedAt.UTC())
	}
	if value.AccessUntil != nil {
		result.AccessUntil = timestamppb.New(value.AccessUntil.UTC())
	}
	if value.StartedAt != nil {
		result.StartedAt = timestamppb.New(value.StartedAt.UTC())
	}
	if value.CompletedAt != nil {
		result.CompletedAt = timestamppb.New(value.CompletedAt.UTC())
	}
	if value.LastActivityAt != nil {
		result.LastActivityAt = timestamppb.New(value.LastActivityAt.UTC())
	}
	if value.FrozenAt != nil {
		result.FrozenAt = timestamppb.New(value.FrozenAt.UTC())
	}
	if value.SuspendedAt != nil {
		result.SuspendedAt = timestamppb.New(value.SuspendedAt.UTC())
	}
	return result
}

func enrollmentsToProto(values []application.Enrollment) []*academyv1.CourseEnrollment {
	result := make([]*academyv1.CourseEnrollment, len(values))
	for index := range values {
		result[index] = enrollmentToProto(values[index])
	}
	return result
}

func internalEnrollmentReportPageToProto(
	value application.InternalEnrollmentReportPage,
) *academyv1.GetInternalEnrollmentReportPageResponse {
	total := value.Total
	if total < 0 {
		total = 0
	}
	return &academyv1.GetInternalEnrollmentReportPageResponse{
		Items: enrollmentsToProto(value.Items), Page: uint32(max(0, value.Page)),
		PageSize: uint32(max(0, value.PageSize)), Total: uint32(total),
	}
}

func enrollmentLessonProgressToProto(value application.EnrollmentLessonProgress) (*academyv1.EnrollmentLessonProgress, error) {
	result := &academyv1.EnrollmentLessonProgress{
		EnrollmentId: value.EnrollmentID.String(), LessonVersionId: value.LessonVersionID.String(),
		Status: enrollmentLessonStatusToProto(value.Status), ActiveSeconds: uint64(max(0, value.ActiveSeconds)),
	}
	if value.FirstOpenedAt != nil {
		result.FirstOpenedAt = timestamppb.New(value.FirstOpenedAt.UTC())
	}
	if value.CompletedAt != nil {
		result.CompletedAt = timestamppb.New(value.CompletedAt.UTC())
	}
	if value.LastPosition != nil {
		position, err := contentToStruct(json.RawMessage(*value.LastPosition))
		if err != nil {
			return nil, fmt.Errorf("decode enrollment position: %w", err)
		}
		result.LastPosition = position
	}
	return result, nil
}

func enrollmentLessonProgressListToProto(values []application.EnrollmentLessonProgress) ([]*academyv1.EnrollmentLessonProgress, error) {
	result := make([]*academyv1.EnrollmentLessonProgress, len(values))
	for index := range values {
		converted, err := enrollmentLessonProgressToProto(values[index])
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func enrollmentQuizAttemptToProto(value application.EnrollmentQuizAttempt) (*academyv1.EnrollmentQuizAttempt, error) {
	var answers []application.EnrollmentQuizAnswer
	if len(value.Answers) > 0 {
		if err := json.Unmarshal(value.Answers, &answers); err != nil {
			return nil, fmt.Errorf("decode enrollment answers: %w", err)
		}
	}
	convertedAnswers := make([]*academyv1.EnrollmentQuizAnswer, len(answers))
	for index, answer := range answers {
		convertedAnswers[index] = &academyv1.EnrollmentQuizAnswer{
			QuestionId: answer.QuestionID, SelectedOptionIds: append([]string(nil), answer.SelectedOptionIDs...), Text: answer.Text,
		}
	}
	result := &academyv1.EnrollmentQuizAttempt{
		Id: value.ID.String(), EnrollmentId: value.EnrollmentID.String(), QuizVersionId: value.QuizVersionID.String(),
		AttemptNumber: uint32(max(0, value.AttemptNumber)), Answers: convertedAnswers,
		Score: uint32(max(0, value.Score)), Passed: value.Passed, PendingReview: value.PendingReview,
		ReviewedById: optionalUUIDString(value.ReviewedByID), ReviewComment: value.ReviewComment,
		CreatedAt: timestamppb.New(value.CreatedAt.UTC()),
	}
	if value.ReviewedAt != nil {
		result.ReviewedAt = timestamppb.New(value.ReviewedAt.UTC())
	}
	return result, nil
}

func enrollmentQuizAttemptsToProto(values []application.EnrollmentQuizAttempt) ([]*academyv1.EnrollmentQuizAttempt, error) {
	result := make([]*academyv1.EnrollmentQuizAttempt, len(values))
	for index := range values {
		converted, err := enrollmentQuizAttemptToProto(values[index])
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func enrollmentOutlineToProto(value application.EnrollmentOutline) *academyv1.EnrollmentOutline {
	sections := make([]*academyv1.EnrollmentOutlineSection, len(value.Sections))
	for sectionIndex, section := range value.Sections {
		lessons := make([]*academyv1.EnrollmentOutlineLesson, len(section.Lessons))
		for lessonIndex, lesson := range section.Lessons {
			converted := &academyv1.EnrollmentOutlineLesson{
				LessonVersionId: lesson.ID.String(), SectionVersionId: lesson.SectionVersionID.String(),
				Title: lesson.Title, Order: uint32(max(0, lesson.Order)), HasQuiz: lesson.QuizVersionID != nil,
			}
			if lesson.Status != "locked" && lesson.Status != "" {
				status := enrollmentLessonStatusToProto(lesson.Status)
				converted.Status = &status
			}
			if lesson.EstimatedMinutes != nil && *lesson.EstimatedMinutes > 0 {
				minutes := uint32(*lesson.EstimatedMinutes)
				converted.EstimatedMinutes = &minutes
			}
			lessons[lessonIndex] = converted
		}
		sections[sectionIndex] = &academyv1.EnrollmentOutlineSection{
			SectionVersionId: section.ID.String(), Title: section.Title,
			Order: uint32(max(0, section.Order)), Lessons: lessons,
		}
	}
	return &academyv1.EnrollmentOutline{Enrollment: enrollmentToProto(value.Enrollment), Sections: sections}
}

func enrollmentLessonToProto(value application.EnrollmentLesson) (*academyv1.EnrollmentLesson, error) {
	content, err := contentToStruct(value.Lesson.Content)
	if err != nil {
		return nil, err
	}
	lesson := &academyv1.LearnerCourseVersionLesson{
		Id: value.Lesson.ID.String(), CourseVersionId: value.Lesson.CourseVersionID.String(),
		SectionVersionId: value.Lesson.SectionVersionID.String(), StableKey: value.Lesson.StableKey,
		Title: value.Lesson.Title, Order: uint32(max(0, value.Lesson.Order)), Content: content,
	}
	if value.Lesson.EstimatedMinutes != nil && *value.Lesson.EstimatedMinutes > 0 {
		minutes := uint32(*value.Lesson.EstimatedMinutes)
		lesson.EstimatedMinutes = &minutes
	}
	if value.Quiz != nil {
		quiz, quizErr := learnerCourseVersionQuizToProto(*value.Quiz)
		if quizErr != nil {
			return nil, quizErr
		}
		lesson.Quiz = quiz
	}
	progress, err := enrollmentLessonProgressToProto(value.Progress)
	if err != nil {
		return nil, err
	}
	return &academyv1.EnrollmentLesson{Enrollment: enrollmentToProto(value.Enrollment), Lesson: lesson, Progress: progress}, nil
}

func enrollmentProgressSnapshotToProto(value application.EnrollmentProgressSnapshot) (*academyv1.EnrollmentProgressSnapshot, error) {
	lessons, err := enrollmentLessonProgressListToProto(value.Lessons)
	if err != nil {
		return nil, err
	}
	attempts, err := enrollmentQuizAttemptsToProto(value.QuizAttempts)
	if err != nil {
		return nil, err
	}
	return &academyv1.EnrollmentProgressSnapshot{
		Enrollment: enrollmentToProto(value.Enrollment), Lessons: lessons, QuizAttempts: attempts,
	}, nil
}

func enrollmentReportToProto(value application.EnrollmentReport) (*academyv1.EnrollmentReport, error) {
	lessons, err := enrollmentLessonProgressListToProto(value.Lessons)
	if err != nil {
		return nil, err
	}
	attempts, err := enrollmentQuizAttemptsToProto(value.QuizAttempts)
	if err != nil {
		return nil, err
	}
	return &academyv1.EnrollmentReport{
		Enrollment: enrollmentToProto(value.Enrollment), Version: courseVersionToProto(value.Version),
		Lessons: lessons, QuizAttempts: attempts, ActiveSeconds: uint64(max(0, value.ActiveSeconds)),
	}, nil
}

func enrollmentLearnerTypeToProto(value string) academyv1.EnrollmentLearnerType {
	switch value {
	case "user":
		return academyv1.EnrollmentLearnerType_ENROLLMENT_LEARNER_TYPE_USER
	case "external":
		return academyv1.EnrollmentLearnerType_ENROLLMENT_LEARNER_TYPE_EXTERNAL
	default:
		return academyv1.EnrollmentLearnerType_ENROLLMENT_LEARNER_TYPE_UNSPECIFIED
	}
}

func enrollmentSourceTypeToProto(value string) academyv1.EnrollmentSourceType {
	switch value {
	case "assignment":
		return academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_ASSIGNMENT
	case "personal_access":
		return academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_PERSONAL_ACCESS
	case "partner_promo_campaign":
		return academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_PARTNER_PROMO_CAMPAIGN
	case "company_candidate_campaign":
		return academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_COMPANY_CANDIDATE_CAMPAIGN
	case "repeat_training":
		return academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_REPEAT_TRAINING
	case "self_enrollment":
		return academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_SELF_ENROLLMENT
	case "legacy":
		return academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_LEGACY
	default:
		return academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_UNSPECIFIED
	}
}

func enrollmentProgressStatusToProto(value string) academyv1.EnrollmentProgressStatus {
	switch value {
	case "not_started":
		return academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_NOT_STARTED
	case "in_progress":
		return academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_IN_PROGRESS
	case "completed":
		return academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_COMPLETED
	default:
		return academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_UNSPECIFIED
	}
}

func enrollmentProgressStatusFromProto(value academyv1.EnrollmentProgressStatus) (string, error) {
	switch value {
	case academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_NOT_STARTED:
		return "not_started", nil
	case academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_IN_PROGRESS:
		return "in_progress", nil
	case academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_COMPLETED:
		return "completed", nil
	default:
		return "", invalidArgument("Некорректное состояние прогресса")
	}
}

func internalEnrollmentReportStatusFromProto(
	value academyv1.InternalEnrollmentReportStatus,
) (*string, error) {
	var result string
	switch value {
	case academyv1.InternalEnrollmentReportStatus_INTERNAL_ENROLLMENT_REPORT_STATUS_UNSPECIFIED:
		return nil, nil
	case academyv1.InternalEnrollmentReportStatus_INTERNAL_ENROLLMENT_REPORT_STATUS_NOT_STARTED:
		result = "not_started"
	case academyv1.InternalEnrollmentReportStatus_INTERNAL_ENROLLMENT_REPORT_STATUS_IN_PROGRESS:
		result = "in_progress"
	case academyv1.InternalEnrollmentReportStatus_INTERNAL_ENROLLMENT_REPORT_STATUS_COMPLETED:
		result = "completed"
	case academyv1.InternalEnrollmentReportStatus_INTERNAL_ENROLLMENT_REPORT_STATUS_OVERDUE:
		result = "overdue"
	case academyv1.InternalEnrollmentReportStatus_INTERNAL_ENROLLMENT_REPORT_STATUS_FROZEN:
		result = "frozen"
	default:
		return nil, invalidArgument("Некорректный статус внутреннего отчёта")
	}
	return &result, nil
}

func internalEnrollmentReportSortFromProto(
	value academyv1.InternalEnrollmentReportSort,
) (string, error) {
	switch value {
	case academyv1.InternalEnrollmentReportSort_INTERNAL_ENROLLMENT_REPORT_SORT_UNSPECIFIED,
		academyv1.InternalEnrollmentReportSort_INTERNAL_ENROLLMENT_REPORT_SORT_UPDATED_DESC:
		return "updated_desc", nil
	case academyv1.InternalEnrollmentReportSort_INTERNAL_ENROLLMENT_REPORT_SORT_UPDATED_ASC:
		return "updated_asc", nil
	case academyv1.InternalEnrollmentReportSort_INTERNAL_ENROLLMENT_REPORT_SORT_TITLE_ASC:
		return "title_asc", nil
	case academyv1.InternalEnrollmentReportSort_INTERNAL_ENROLLMENT_REPORT_SORT_TITLE_DESC:
		return "title_desc", nil
	case academyv1.InternalEnrollmentReportSort_INTERNAL_ENROLLMENT_REPORT_SORT_DEADLINE_ASC:
		return "deadline_asc", nil
	case academyv1.InternalEnrollmentReportSort_INTERNAL_ENROLLMENT_REPORT_SORT_STATUS:
		return "status", nil
	default:
		return "", invalidArgument("Некорректная сортировка внутреннего отчёта")
	}
}

func enrollmentAccessStatusToProto(value string) academyv1.EnrollmentAccessStatus {
	switch value {
	case "invited":
		return academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_INVITED
	case "ready":
		return academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_READY
	case "active":
		return academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_ACTIVE
	case "expired":
		return academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_EXPIRED
	case "frozen":
		return academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_FROZEN
	case "suspended":
		return academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_SUSPENDED
	case "revoked":
		return academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_REVOKED
	case "closed":
		return academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_CLOSED
	default:
		return academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_UNSPECIFIED
	}
}

func enrollmentAccessStatusFromProto(value academyv1.EnrollmentAccessStatus) (string, error) {
	switch value {
	case academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_INVITED:
		return "invited", nil
	case academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_READY:
		return "ready", nil
	case academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_ACTIVE:
		return "active", nil
	case academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_EXPIRED:
		return "expired", nil
	case academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_FROZEN:
		return "frozen", nil
	case academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_SUSPENDED:
		return "suspended", nil
	case academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_REVOKED:
		return "revoked", nil
	case academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_CLOSED:
		return "closed", nil
	default:
		return "", invalidArgument("Некорректное состояние доступа")
	}
}

func enrollmentLessonStatusToProto(value string) academyv1.EnrollmentLessonStatus {
	switch value {
	case "available":
		return academyv1.EnrollmentLessonStatus_ENROLLMENT_LESSON_STATUS_AVAILABLE
	case "current":
		return academyv1.EnrollmentLessonStatus_ENROLLMENT_LESSON_STATUS_CURRENT
	case "completed":
		return academyv1.EnrollmentLessonStatus_ENROLLMENT_LESSON_STATUS_COMPLETED
	default:
		return academyv1.EnrollmentLessonStatus_ENROLLMENT_LESSON_STATUS_UNSPECIFIED
	}
}
