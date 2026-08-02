package transport

import (
	"testing"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
)

func TestAcademyEnrollmentFromProtoKeepsCourseReadModel(t *testing.T) {
	t.Parallel()

	title := "Управление приоритетами"
	coverURL := "https://cdn.example.test/course-cover.png"
	completedLessons := uint32(1)
	totalLessons := uint32(4)
	value, err := academyEnrollmentFromProto(&academyv1.CourseEnrollment{
		Id:                   uuid.NewString(),
		CompanyId:            uuid.NewString(),
		CourseId:             uuid.NewString(),
		CourseVersionId:      uuid.NewString(),
		CourseTitle:          &title,
		CourseCoverUrl:       &coverURL,
		CompletedLessonCount: &completedLessons,
		TotalLessonCount:     &totalLessons,
	})
	if err != nil {
		t.Fatalf("academyEnrollmentFromProto() error = %v", err)
	}
	if value.CourseTitle == nil || *value.CourseTitle != title {
		t.Fatalf("course title = %v, want %q", value.CourseTitle, title)
	}
	if value.CourseCoverUrl == nil || *value.CourseCoverUrl != coverURL {
		t.Fatalf("course cover url = %v, want %q", value.CourseCoverUrl, coverURL)
	}
	if value.CompletedLessons == nil || *value.CompletedLessons != 1 {
		t.Fatalf("completed lessons = %v, want 1", value.CompletedLessons)
	}
	if value.TotalLessons == nil || *value.TotalLessons != 4 {
		t.Fatalf("total lessons = %v, want 4", value.TotalLessons)
	}
}
