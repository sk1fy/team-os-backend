package transport

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func academyEnrollmentFromProto(value *academyv1.CourseEnrollment) (api.AcademyEnrollment, error) {
	if value == nil {
		return api.AcademyEnrollment{}, errors.New("academy returned an empty enrollment")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.AcademyEnrollment{}, err
	}
	companyID, err := uuid.Parse(value.GetCompanyId())
	if err != nil {
		return api.AcademyEnrollment{}, err
	}
	courseID, err := uuid.Parse(value.GetCourseId())
	if err != nil {
		return api.AcademyEnrollment{}, err
	}
	versionID, err := uuid.Parse(value.GetCourseVersionId())
	if err != nil {
		return api.AcademyEnrollment{}, err
	}
	result := api.AcademyEnrollment{
		Id: id, CompanyId: companyID, CourseId: courseID, CourseVersionId: versionID,
		LearnerType: enrollmentLearnerTypeFromProto(value.GetLearnerType()),
		SourceType:  enrollmentSourceTypeFromProto(value.GetSourceType()), AttemptNumber: int(value.GetAttemptNumber()),
		ProgressStatus:  enrollmentProgressStatusFromProto(value.GetProgressStatus()),
		AccessStatus:    enrollmentAccessStatusFromProto(value.GetAccessStatus()),
		ProgressPercent: int(value.GetProgressPercent()), Overdue: value.GetOverdue(),
	}
	if value.GetCreatedAt() != nil {
		result.CreatedAt = value.GetCreatedAt().AsTime()
	}
	if value.GetUpdatedAt() != nil {
		result.UpdatedAt = value.GetUpdatedAt().AsTime()
	}
	if result.UserId, err = parseOptionalUUIDString(value.GetUserId()); err != nil {
		return api.AcademyEnrollment{}, err
	}
	if result.ExternalLearnerId, err = parseOptionalUUIDString(value.GetExternalLearnerId()); err != nil {
		return api.AcademyEnrollment{}, err
	}
	if result.SourceId, err = parseOptionalUUIDString(value.GetSourceId()); err != nil {
		return api.AcademyEnrollment{}, err
	}
	if result.CurrentLessonVersionId, err = parseOptionalUUIDString(value.GetCurrentLessonVersionId()); err != nil {
		return api.AcademyEnrollment{}, err
	}
	result.ActivatedAt = protoTimestampPointer(value.GetActivatedAt())
	result.AccessUntil = protoTimestampPointer(value.GetAccessUntil())
	result.StartedAt = protoTimestampPointer(value.GetStartedAt())
	result.CompletedAt = protoTimestampPointer(value.GetCompletedAt())
	result.LastActivityAt = protoTimestampPointer(value.GetLastActivityAt())
	result.FrozenAt = protoTimestampPointer(value.GetFrozenAt())
	result.SuspendedAt = protoTimestampPointer(value.GetSuspendedAt())
	result.DueDate = protoTimestampPointer(value.GetDueDate())
	return result, nil
}

func academyEnrollmentsFromProto(values []*academyv1.CourseEnrollment) ([]api.AcademyEnrollment, error) {
	result := make([]api.AcademyEnrollment, len(values))
	for index := range values {
		converted, err := academyEnrollmentFromProto(values[index])
		if err != nil {
			return nil, err
		}
		result[index] = converted
	}
	return result, nil
}

func enrollmentOutlineFromProto(value *academyv1.EnrollmentOutline) (api.AcademyEnrollmentOutline, error) {
	if value == nil {
		return api.AcademyEnrollmentOutline{}, errors.New("academy returned an empty enrollment outline")
	}
	enrollment, err := academyEnrollmentFromProto(value.GetEnrollment())
	if err != nil {
		return api.AcademyEnrollmentOutline{}, err
	}
	sections := make([]api.ExternalEnrollmentOutlineSection, len(value.GetSections()))
	for sectionIndex, section := range value.GetSections() {
		sectionID, parseErr := uuid.Parse(section.GetSectionVersionId())
		if parseErr != nil {
			return api.AcademyEnrollmentOutline{}, parseErr
		}
		lessons := make([]api.ExternalEnrollmentOutlineLesson, len(section.GetLessons()))
		for lessonIndex, lesson := range section.GetLessons() {
			lessonID, lessonErr := uuid.Parse(lesson.GetLessonVersionId())
			if lessonErr != nil {
				return api.AcademyEnrollmentOutline{}, lessonErr
			}
			status := api.EnrollmentLessonStatus("locked")
			var lockReason *string
			if lesson.Status != nil {
				status = enrollmentLessonStatusFromProto(lesson.GetStatus())
			} else {
				reason := "Урок ещё не открыт"
				lockReason = &reason
			}
			converted := api.ExternalEnrollmentOutlineLesson{
				Id: lessonID, Title: lesson.GetTitle(), Order: int(lesson.GetOrder()), Status: status, LockReason: lockReason,
			}
			if lesson.EstimatedMinutes != nil {
				minutes := int(lesson.GetEstimatedMinutes())
				converted.EstimatedMinutes = &minutes
			}
			lessons[lessonIndex] = converted
		}
		sections[sectionIndex] = api.ExternalEnrollmentOutlineSection{
			Id: sectionID, Title: section.GetTitle(), Order: int(section.GetOrder()), Lessons: lessons,
		}
	}
	return api.AcademyEnrollmentOutline{Enrollment: enrollment, Sections: sections}, nil
}

func enrollmentLessonProgressFromProto(value *academyv1.EnrollmentLessonProgress) (api.EnrollmentLessonProgress, error) {
	if value == nil {
		return api.EnrollmentLessonProgress{}, errors.New("academy returned empty lesson progress")
	}
	enrollmentID, err := uuid.Parse(value.GetEnrollmentId())
	if err != nil {
		return api.EnrollmentLessonProgress{}, err
	}
	lessonID, err := uuid.Parse(value.GetLessonVersionId())
	if err != nil {
		return api.EnrollmentLessonProgress{}, err
	}
	result := api.EnrollmentLessonProgress{
		EnrollmentId: enrollmentID, LessonVersionId: lessonID,
		Status: enrollmentLessonStatusFromProto(value.GetStatus()), ActiveSeconds: int64(value.GetActiveSeconds()),
		FirstOpenedAt: protoTimestampPointer(value.GetFirstOpenedAt()), CompletedAt: protoTimestampPointer(value.GetCompletedAt()),
	}
	if value.GetLastPosition() != nil {
		position := value.GetLastPosition().AsMap()
		result.LastPosition = &position
	}
	return result, nil
}

func enrollmentQuizAttemptFromProto(value *academyv1.EnrollmentQuizAttempt) (api.EnrollmentQuizAttempt, error) {
	if value == nil {
		return api.EnrollmentQuizAttempt{}, errors.New("academy returned empty quiz attempt")
	}
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.EnrollmentQuizAttempt{}, err
	}
	enrollmentID, err := uuid.Parse(value.GetEnrollmentId())
	if err != nil {
		return api.EnrollmentQuizAttempt{}, err
	}
	quizID, err := uuid.Parse(value.GetQuizVersionId())
	if err != nil {
		return api.EnrollmentQuizAttempt{}, err
	}
	answers := make([]api.ExternalQuizAnswer, len(value.GetAnswers()))
	for index, answer := range value.GetAnswers() {
		questionID, parseErr := uuid.Parse(answer.GetQuestionId())
		if parseErr != nil {
			return api.EnrollmentQuizAttempt{}, parseErr
		}
		optionIDs := make([]uuid.UUID, len(answer.GetSelectedOptionIds()))
		for optionIndex, option := range answer.GetSelectedOptionIds() {
			optionID, optionErr := uuid.Parse(option)
			if optionErr != nil {
				return api.EnrollmentQuizAttempt{}, optionErr
			}
			optionIDs[optionIndex] = optionID
		}
		answers[index] = api.ExternalQuizAnswer{QuestionId: questionID, OptionIds: &optionIDs, Text: answer.Text}
	}
	result := api.EnrollmentQuizAttempt{
		Id: id, EnrollmentId: enrollmentID, QuizVersionId: quizID, AttemptNumber: int(value.GetAttemptNumber()),
		Answers: &answers, Score: int(value.GetScore()), Passed: value.GetPassed(), PendingReview: value.GetPendingReview(),
		ReviewComment: value.ReviewComment, CreatedAt: value.GetCreatedAt().AsTime(),
	}
	if result.ReviewedById, err = parseOptionalUUIDString(value.GetReviewedById()); err != nil {
		return api.EnrollmentQuizAttempt{}, err
	}
	result.ReviewedAt = protoTimestampPointer(value.GetReviewedAt())
	return result, nil
}

func enrollmentProgressSnapshotFromProto(value *academyv1.EnrollmentProgressSnapshot) (api.EnrollmentProgressSnapshot, error) {
	if value == nil {
		return api.EnrollmentProgressSnapshot{}, errors.New("academy returned empty progress snapshot")
	}
	enrollment, err := academyEnrollmentFromProto(value.GetEnrollment())
	if err != nil {
		return api.EnrollmentProgressSnapshot{}, err
	}
	lessons := make([]api.EnrollmentLessonProgress, len(value.GetLessons()))
	for index, lesson := range value.GetLessons() {
		converted, convertErr := enrollmentLessonProgressFromProto(lesson)
		if convertErr != nil {
			return api.EnrollmentProgressSnapshot{}, convertErr
		}
		lessons[index] = converted
	}
	attempts := make([]api.EnrollmentQuizAttempt, len(value.GetQuizAttempts()))
	for index, attempt := range value.GetQuizAttempts() {
		converted, convertErr := enrollmentQuizAttemptFromProto(attempt)
		if convertErr != nil {
			return api.EnrollmentProgressSnapshot{}, convertErr
		}
		attempts[index] = converted
	}
	return api.EnrollmentProgressSnapshot{Enrollment: enrollment, Lessons: lessons, QuizAttempts: attempts}, nil
}

func academyEnrollmentLessonFromProto(value *academyv1.EnrollmentLesson) (api.AcademyEnrollmentLesson, error) {
	if value == nil || value.GetLesson() == nil {
		return api.AcademyEnrollmentLesson{}, errors.New("academy returned empty enrollment lesson")
	}
	enrollment, err := academyEnrollmentFromProto(value.GetEnrollment())
	if err != nil {
		return api.AcademyEnrollmentLesson{}, err
	}
	lesson := value.GetLesson()
	id, err := uuid.Parse(lesson.GetId())
	if err != nil {
		return api.AcademyEnrollmentLesson{}, err
	}
	versionID, err := uuid.Parse(lesson.GetCourseVersionId())
	if err != nil {
		return api.AcademyEnrollmentLesson{}, err
	}
	sectionID, err := uuid.Parse(lesson.GetSectionVersionId())
	if err != nil {
		return api.AcademyEnrollmentLesson{}, err
	}
	content, err := richTextFromStruct(lesson.GetContent())
	if err != nil {
		return api.AcademyEnrollmentLesson{}, err
	}
	progress, err := enrollmentLessonProgressFromProto(value.GetProgress())
	if err != nil {
		return api.AcademyEnrollmentLesson{}, err
	}
	converted := api.ExternalEnrollmentLesson{
		Id: id, CourseVersionId: versionID, SectionVersionId: sectionID, Title: lesson.GetTitle(),
		Order: int(lesson.GetOrder()), Content: content, Status: progress.Status,
	}
	if lesson.EstimatedMinutes != nil {
		minutes := int(lesson.GetEstimatedMinutes())
		converted.EstimatedMinutes = &minutes
	}
	if lesson.GetQuiz() != nil {
		quiz, quizErr := learnerQuizFromProto(lesson.GetQuiz())
		if quizErr != nil {
			return api.AcademyEnrollmentLesson{}, quizErr
		}
		converted.Quiz = &quiz
	}
	return api.AcademyEnrollmentLesson{Enrollment: enrollment, Lesson: converted, Progress: &progress}, nil
}

func learnerQuizFromProto(value *academyv1.LearnerCourseVersionQuiz) (api.LearnerQuiz, error) {
	id, err := uuid.Parse(value.GetId())
	if err != nil {
		return api.LearnerQuiz{}, err
	}
	questions := make([]api.LearnerQuizQuestion, len(value.GetQuestions()))
	for questionIndex, question := range value.GetQuestions() {
		questionID, parseErr := uuid.Parse(question.GetId())
		if parseErr != nil {
			return api.LearnerQuiz{}, parseErr
		}
		options := make([]api.LearnerQuizOption, len(question.GetOptions()))
		for optionIndex, option := range question.GetOptions() {
			optionID, optionErr := uuid.Parse(option.GetId())
			if optionErr != nil {
				return api.LearnerQuiz{}, optionErr
			}
			options[optionIndex] = api.LearnerQuizOption{Id: optionID, Text: option.GetText()}
		}
		questionType, typeErr := quizQuestionTypeFromProto(question.GetType())
		if typeErr != nil {
			return api.LearnerQuiz{}, typeErr
		}
		questions[questionIndex] = api.LearnerQuizQuestion{Id: questionID, Type: questionType, Text: question.GetText(), Options: options}
	}
	result := api.LearnerQuiz{Id: id, Questions: questions, PassingScore: int(value.GetPassingScore())}
	if value.MaxAttempts != nil {
		attempts := int(value.GetMaxAttempts())
		result.MaxAttempts = &attempts
	}
	return result, nil
}

func enrollmentReportFromProto(value *academyv1.EnrollmentReport) (api.EnrollmentReport, error) {
	if value == nil {
		return api.EnrollmentReport{}, errors.New("academy returned empty enrollment report")
	}
	snapshot, err := enrollmentProgressSnapshotFromProto(&academyv1.EnrollmentProgressSnapshot{
		Enrollment: value.GetEnrollment(), Lessons: value.GetLessons(), QuizAttempts: value.GetQuizAttempts(),
	})
	if err != nil {
		return api.EnrollmentReport{}, err
	}
	return api.EnrollmentReport{
		Enrollment: snapshot.Enrollment, Lessons: snapshot.Lessons,
		QuizAttempts: snapshot.QuizAttempts, ActiveSeconds: int(value.GetActiveSeconds()),
	}, nil
}

func protoTimestampPointer(value *timestamppb.Timestamp) *time.Time {
	if value == nil || !value.IsValid() {
		return nil
	}
	converted := value.AsTime()
	return &converted
}

func enrollmentLearnerTypeFromProto(value academyv1.EnrollmentLearnerType) api.EnrollmentLearnerType {
	switch value {
	case academyv1.EnrollmentLearnerType_ENROLLMENT_LEARNER_TYPE_USER:
		return api.EnrollmentLearnerType("user")
	case academyv1.EnrollmentLearnerType_ENROLLMENT_LEARNER_TYPE_EXTERNAL:
		return api.EnrollmentLearnerType("external")
	default:
		return api.EnrollmentLearnerType("")
	}
}

func enrollmentSourceTypeFromProto(value academyv1.EnrollmentSourceType) api.EnrollmentSourceType {
	values := map[academyv1.EnrollmentSourceType]api.EnrollmentSourceType{
		academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_ASSIGNMENT:                 "assignment",
		academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_PERSONAL_ACCESS:            "personal_access",
		academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_PARTNER_PROMO_CAMPAIGN:     "partner_promo_campaign",
		academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_COMPANY_CANDIDATE_CAMPAIGN: "company_candidate_campaign",
		academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_REPEAT_TRAINING:            "repeat_training",
		academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_LEGACY:                     "legacy",
		academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_SELF_ENROLLMENT:            "self_enrollment",
	}
	return values[value]
}

func enrollmentProgressStatusFromProto(value academyv1.EnrollmentProgressStatus) api.EnrollmentProgressStatus {
	switch value {
	case academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_NOT_STARTED:
		return "not_started"
	case academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_IN_PROGRESS:
		return "in_progress"
	case academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_COMPLETED:
		return "completed"
	default:
		return ""
	}
}

func enrollmentAccessStatusFromProto(value academyv1.EnrollmentAccessStatus) api.EnrollmentAccessStatus {
	names := map[academyv1.EnrollmentAccessStatus]api.EnrollmentAccessStatus{
		academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_INVITED:   "invited",
		academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_READY:     "ready",
		academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_ACTIVE:    "active",
		academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_EXPIRED:   "expired",
		academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_FROZEN:    "frozen",
		academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_SUSPENDED: "suspended",
		academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_REVOKED:   "revoked",
		academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_CLOSED:    "closed",
	}
	return names[value]
}

func enrollmentLessonStatusFromProto(value academyv1.EnrollmentLessonStatus) api.EnrollmentLessonStatus {
	switch value {
	case academyv1.EnrollmentLessonStatus_ENROLLMENT_LESSON_STATUS_AVAILABLE:
		return "available"
	case academyv1.EnrollmentLessonStatus_ENROLLMENT_LESSON_STATUS_CURRENT:
		return "current"
	case academyv1.EnrollmentLessonStatus_ENROLLMENT_LESSON_STATUS_COMPLETED:
		return "completed"
	default:
		return "locked"
	}
}

func enrollmentFilterProgressToProto(value api.EnrollmentProgressStatus) (academyv1.EnrollmentProgressStatus, error) {
	switch value {
	case "not_started":
		return academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_NOT_STARTED, nil
	case "in_progress":
		return academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_IN_PROGRESS, nil
	case "completed":
		return academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_COMPLETED, nil
	default:
		return 0, fmt.Errorf("unknown enrollment progress status %q", value)
	}
}

func enrollmentFilterAccessToProto(value api.EnrollmentAccessStatus) (academyv1.EnrollmentAccessStatus, error) {
	values := map[api.EnrollmentAccessStatus]academyv1.EnrollmentAccessStatus{
		"invited":   academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_INVITED,
		"ready":     academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_READY,
		"active":    academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_ACTIVE,
		"expired":   academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_EXPIRED,
		"frozen":    academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_FROZEN,
		"suspended": academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_SUSPENDED,
		"revoked":   academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_REVOKED,
		"closed":    academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_CLOSED,
	}
	converted, ok := values[value]
	if !ok {
		return 0, fmt.Errorf("unknown enrollment access status %q", value)
	}
	return converted, nil
}
