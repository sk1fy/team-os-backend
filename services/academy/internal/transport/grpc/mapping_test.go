package grpc

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestEnrollmentSourceTypeToProto(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  academyv1.EnrollmentSourceType
	}{
		{name: "self enrollment", value: "self_enrollment", want: academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_SELF_ENROLLMENT},
		{name: "legacy", value: "legacy", want: academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_LEGACY},
		{name: "unknown", value: "unknown", want: academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_UNSPECIFIED},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := enrollmentSourceTypeToProto(test.value); got != test.want {
				t.Fatalf("enrollmentSourceTypeToProto(%q) = %s, want %s", test.value, got, test.want)
			}
		})
	}
}

func TestLearnerPublishedCourseVersionDoesNotExposeCorrectAnswers(t *testing.T) {
	versionID := uuid.New()
	sectionID := uuid.New()
	lessonID := uuid.New()
	quizID := uuid.New()
	value := application.CourseVersionContent{
		Version: application.CourseVersion{
			ID: versionID, CourseID: uuid.New(), Number: 1, Status: "published",
			Title: "Безопасный курс", CreatedByID: uuid.New(), CreatedAt: time.Now(),
		},
		Sections: []application.CourseVersionSection{{
			ID: sectionID, CourseVersionID: versionID, Title: "Раздел",
		}},
		Lessons: []application.CourseVersionLesson{{
			ID: lessonID, CourseVersionID: versionID, SectionVersionID: sectionID,
			StableKey: uuid.NewString(), Title: "Урок", Content: json.RawMessage(`{"type":"doc"}`),
			QuizVersionID: &quizID,
		}},
		Quizzes: []application.CourseVersionQuiz{{
			ID: quizID, CourseVersionID: versionID, LessonVersionID: lessonID,
			PassingScore: 100,
			Questions:    json.RawMessage(`[{"id":"q1","type":"single","text":"Вопрос","options":[{"id":"a","text":"Да","correct":true},{"id":"b","text":"Нет","correct":false}]}]`),
		}},
	}

	converted, err := learnerPublishedCourseVersionToProto(value)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := protojson.Marshal(converted)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte("correct")) {
		t.Fatalf("learner projection leaked correct answers: %s", payload)
	}
	options := converted.GetSections()[0].GetLessons()[0].GetQuiz().GetQuestions()[0].GetOptions()
	if len(options) != 2 || options[0].GetText() != "Да" {
		t.Fatalf("learner options were not preserved: %#v", options)
	}
}
