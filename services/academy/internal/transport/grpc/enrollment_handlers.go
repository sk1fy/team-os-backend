package grpc

import (
	"context"
	"encoding/json"

	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func (s *Server) GetEnrollments(ctx context.Context, request *academyv1.GetEnrollmentsRequest) (*academyv1.GetEnrollmentsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	filters := application.EnrollmentFilters{}
	if filters.CourseID, err = parseOptionalUUID(request.CourseId); err != nil {
		return nil, err
	}
	if filters.CourseVersionID, err = parseOptionalUUID(request.CourseVersionId); err != nil {
		return nil, err
	}
	if filters.UserID, err = parseOptionalUUID(request.UserId); err != nil {
		return nil, err
	}
	if request.ProgressStatus != nil {
		value, convertErr := enrollmentProgressStatusFromProto(request.GetProgressStatus())
		if convertErr != nil {
			return nil, convertErr
		}
		filters.ProgressStatus = &value
	}
	if request.AccessStatus != nil {
		value, convertErr := enrollmentAccessStatusFromProto(request.GetAccessStatus())
		if convertErr != nil {
			return nil, convertErr
		}
		filters.AccessStatus = &value
	}
	values, err := s.application.GetEnrollments(ctx, actor, filters)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetEnrollmentsResponse{Enrollments: enrollmentsToProto(values)}, nil
}

func (s *Server) GetInternalEnrollmentReportPage(
	ctx context.Context,
	request *academyv1.GetInternalEnrollmentReportPageRequest,
) (*academyv1.GetInternalEnrollmentReportPageResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	userIDs, err := parseUUIDList(request.GetUserIds())
	if err != nil {
		return nil, err
	}
	searchUserIDs, err := parseUUIDList(request.GetSearchUserIds())
	if err != nil {
		return nil, err
	}
	courseID, err := parseOptionalUUID(request.CourseId)
	if err != nil {
		return nil, err
	}
	reportStatus, err := internalEnrollmentReportStatusFromProto(request.GetStatus())
	if err != nil {
		return nil, err
	}
	reportSort, err := internalEnrollmentReportSortFromProto(request.GetSort())
	if err != nil {
		return nil, err
	}
	page, err := s.application.GetInternalEnrollmentReportPage(ctx, actor, application.InternalEnrollmentReportQuery{
		UserIDs: userIDs, SearchUserIDs: searchUserIDs, Search: request.Search,
		CourseID: courseID, Status: reportStatus, Sort: reportSort,
		Page: int32(request.GetPage()), PageSize: int32(request.GetPageSize()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return internalEnrollmentReportPageToProto(page), nil
}

func (s *Server) SelfEnrollCourse(ctx context.Context, request *academyv1.SelfEnrollCourseRequest) (*academyv1.SelfEnrollCourseResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.SelfEnrollCourse(ctx, actor, courseID)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.SelfEnrollCourseResponse{Enrollment: enrollmentToProto(value)}, nil
}

func (s *Server) GetAcademyCatalog(ctx context.Context, request *academyv1.GetAcademyCatalogRequest) (*academyv1.GetAcademyCatalogResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	page, err := s.application.GetAcademyCatalog(ctx, actor, application.CatalogQuery{
		Search: request.Search, Page: int32(request.GetPage()), PageSize: int32(request.GetPageSize()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return catalogPageToProto(page), nil
}

func (s *Server) GetCatalogCourseVersion(ctx context.Context, request *academyv1.GetCatalogCourseVersionRequest) (*academyv1.GetCatalogCourseVersionResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	courseID, err := parseUUID(request.GetCourseId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetCatalogCourseVersion(ctx, actor, courseID)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := learnerPublishedCourseVersionToProto(value)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetCatalogCourseVersionResponse{Version: converted}, nil
}

func (s *Server) GetEnrollment(ctx context.Context, request *academyv1.GetEnrollmentRequest) (*academyv1.GetEnrollmentResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetEnrollmentId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetEnrollment(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetEnrollmentResponse{Enrollment: enrollmentToProto(value)}, nil
}

func (s *Server) GetEnrollmentOutline(ctx context.Context, request *academyv1.GetEnrollmentOutlineRequest) (*academyv1.GetEnrollmentOutlineResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetEnrollmentId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetEnrollmentOutline(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	return &academyv1.GetEnrollmentOutlineResponse{Outline: enrollmentOutlineToProto(value)}, nil
}

func (s *Server) GetEnrollmentLesson(ctx context.Context, request *academyv1.GetEnrollmentLessonRequest) (*academyv1.GetEnrollmentLessonResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	enrollmentID, err := parseUUID(request.GetEnrollmentId())
	if err != nil {
		return nil, err
	}
	lessonID, err := parseUUID(request.GetLessonVersionId())
	if err != nil {
		return nil, err
	}
	value, err := s.application.GetEnrollmentLesson(ctx, actor, enrollmentID, lessonID)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := enrollmentLessonToProto(value)
	if err != nil {
		return nil, enrollmentMappingError(err)
	}
	return &academyv1.GetEnrollmentLessonResponse{Lesson: converted}, nil
}

func (s *Server) ResumeEnrollment(ctx context.Context, request *academyv1.ResumeEnrollmentRequest) (*academyv1.ResumeEnrollmentResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetEnrollmentId())
	if err != nil {
		return nil, err
	}
	enrollment, lesson, err := s.application.ResumeEnrollment(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	result := &academyv1.ResumeEnrollmentResponse{Enrollment: enrollmentToProto(enrollment)}
	if lesson != nil {
		result.CurrentLesson, err = enrollmentLessonToProto(*lesson)
		if err != nil {
			return nil, enrollmentMappingError(err)
		}
	}
	return result, nil
}

func (s *Server) CompleteEnrollmentLesson(ctx context.Context, request *academyv1.CompleteEnrollmentLessonRequest) (*academyv1.CompleteEnrollmentLessonResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	enrollmentID, err := parseUUID(request.GetEnrollmentId())
	if err != nil {
		return nil, err
	}
	lessonID, err := parseUUID(request.GetLessonVersionId())
	if err != nil {
		return nil, err
	}
	position, err := enrollmentPositionFromProto(request.LastPosition)
	if err != nil {
		return nil, err
	}
	activeSeconds := int64(request.GetActiveSeconds())
	progress, err := s.application.CompleteEnrollmentLesson(ctx, actor, application.CompleteEnrollmentLessonInput{
		EnrollmentID: enrollmentID, LessonID: lessonID, ActiveSeconds: activeSeconds, LastPosition: position,
		IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := enrollmentProgressSnapshotToProto(progress)
	if err != nil {
		return nil, enrollmentMappingError(err)
	}
	return &academyv1.CompleteEnrollmentLessonResponse{Progress: converted}, nil
}

func (s *Server) SubmitEnrollmentQuizAttempt(ctx context.Context, request *academyv1.SubmitEnrollmentQuizAttemptRequest) (*academyv1.SubmitEnrollmentQuizAttemptResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	enrollmentID, err := parseUUID(request.GetEnrollmentId())
	if err != nil {
		return nil, err
	}
	quizID, err := parseUUID(request.GetQuizVersionId())
	if err != nil {
		return nil, err
	}
	answers := make([]application.EnrollmentQuizAnswer, len(request.GetAnswers()))
	for index, answer := range request.GetAnswers() {
		answers[index] = application.EnrollmentQuizAnswer{
			QuestionID: answer.GetQuestionId(), SelectedOptionIDs: append([]string(nil), answer.GetSelectedOptionIds()...), Text: answer.Text,
		}
	}
	position, err := enrollmentPositionFromProto(request.LastPosition)
	if err != nil {
		return nil, err
	}
	attempt, progress, err := s.application.SubmitEnrollmentQuizAttempt(ctx, actor, application.SubmitEnrollmentQuizInput{
		EnrollmentID: enrollmentID, QuizID: quizID, Answers: answers,
		ActiveSeconds: int64(request.GetActiveSeconds()), LastPosition: position,
		IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	convertedAttempt, err := enrollmentQuizAttemptToProto(attempt)
	if err != nil {
		return nil, enrollmentMappingError(err)
	}
	convertedProgress, err := enrollmentProgressSnapshotToProto(progress)
	if err != nil {
		return nil, enrollmentMappingError(err)
	}
	return &academyv1.SubmitEnrollmentQuizAttemptResponse{Attempt: convertedAttempt, Progress: convertedProgress}, nil
}

func (s *Server) ReviewEnrollmentQuizAttempt(ctx context.Context, request *academyv1.ReviewEnrollmentQuizAttemptRequest) (*academyv1.ReviewEnrollmentQuizAttemptResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	enrollmentID, err := parseUUID(request.GetEnrollmentId())
	if err != nil {
		return nil, err
	}
	attemptID, err := parseUUID(request.GetAttemptId())
	if err != nil {
		return nil, err
	}
	attempt, progress, err := s.application.ReviewEnrollmentQuizAttempt(ctx, actor, application.ReviewEnrollmentQuizInput{
		EnrollmentID: enrollmentID, AttemptID: attemptID, Passed: request.GetPassed(), Comment: request.Comment,
	})
	if err != nil {
		return nil, transportError(err)
	}
	convertedAttempt, err := enrollmentQuizAttemptToProto(attempt)
	if err != nil {
		return nil, enrollmentMappingError(err)
	}
	convertedProgress, err := enrollmentProgressSnapshotToProto(progress)
	if err != nil {
		return nil, enrollmentMappingError(err)
	}
	return &academyv1.ReviewEnrollmentQuizAttemptResponse{Attempt: convertedAttempt, Progress: convertedProgress}, nil
}

func (s *Server) GetEnrollmentReport(ctx context.Context, request *academyv1.GetEnrollmentReportRequest) (*academyv1.GetEnrollmentReportResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(request.GetEnrollmentId())
	if err != nil {
		return nil, err
	}
	report, err := s.application.GetEnrollmentReport(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	converted, err := enrollmentReportToProto(report)
	if err != nil {
		return nil, enrollmentMappingError(err)
	}
	return &academyv1.GetEnrollmentReportResponse{Report: converted}, nil
}

func enrollmentPositionFromProto(value *structpb.Struct) (*string, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value.AsMap())
	if err != nil {
		return nil, invalidArgument("Некорректная позиция в уроке")
	}
	result := string(encoded)
	return &result, nil
}

func enrollmentMappingError(error) error {
	return status.Error(codes.Internal, "Внутренняя ошибка сервиса")
}
