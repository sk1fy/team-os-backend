package transport

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCourseVersionLearnerDetailStripsCorrectAnswers(t *testing.T) {
	t.Parallel()
	courseID, versionID, sectionID := uuid.New(), uuid.New(), uuid.New()
	lessonID, quizID, questionID, optionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	content, err := structpb.NewStruct(map[string]any{"type": "doc"})
	if err != nil {
		t.Fatal(err)
	}
	response := &academyv1.GetCourseVersionResponse{
		Version: &academyv1.CourseVersion{
			Id: versionID.String(), CourseId: courseID.String(), Number: 2,
			Status: academyv1.CourseVersionStatus_COURSE_VERSION_STATUS_RETIRED,
			Title:  "Версия", CreatedAt: timestamppb.New(time.Now()),
		},
		Sections: []*academyv1.CourseVersionSection{{Id: sectionID.String(), CourseVersionId: versionID.String(), Title: "Раздел"}},
		Lessons: []*academyv1.CourseVersionLesson{{
			Id: lessonID.String(), CourseVersionId: versionID.String(), SectionVersionId: sectionID.String(),
			Title: "Урок", Content: content, QuizVersionId: protoStringPointer(quizID.String()),
		}},
		Quizzes: []*academyv1.CourseVersionQuiz{{
			Id: quizID.String(), CourseVersionId: versionID.String(), LessonVersionId: lessonID.String(), PassingScore: 80,
			Questions: []*academyv1.QuizQuestion{{
				Id: questionID.String(), Type: academyv1.QuizQuestionType_QUIZ_QUESTION_TYPE_SINGLE, Text: "Вопрос",
				Options: []*academyv1.QuizOption{{Id: optionID.String(), Text: "Секрет", Correct: true}},
			}},
		}},
	}
	author, err := courseVersionAuthorDetailFromProto(response)
	if err != nil {
		t.Fatal(err)
	}
	learner := courseVersionLearnerDetailFromAuthor(author)
	if len(learner.Sections) != 1 || len(learner.Sections[0].Lessons) != 1 {
		t.Fatalf("unexpected learner outline: %#v", learner.Sections)
	}
	quiz := learner.Sections[0].Lessons[0].Quiz
	if quiz == nil || len(quiz.Questions) != 1 || len(quiz.Questions[0].Options) != 1 {
		t.Fatalf("unexpected learner quiz: %#v", quiz)
	}
	if quiz.Questions[0].Options[0].Text != "Секрет" {
		t.Fatalf("option text = %q", quiz.Questions[0].Options[0].Text)
	}
}

func TestCSVSafeNeutralizesSpreadsheetFormulas(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ input, want string }{
		{"=1+1", "'=1+1"}, {" +SUM(A1:A2)", "' +SUM(A1:A2)"}, {"-10", "'-10"}, {"@cmd", "'@cmd"}, {"Обычный текст", "Обычный текст"},
	} {
		if got := csvSafe(test.input); got != test.want {
			t.Errorf("csvSafe(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestPaginateEnrollmentsOutOfRangeIsEmpty(t *testing.T) {
	t.Parallel()
	values := []api.EnrollmentSummary{{Id: uuid.New()}, {Id: uuid.New()}}
	page := paginateEnrollments(values, 3, 1)
	if len(page.Items) != 0 || page.Total != 2 || page.TotalPages != 2 {
		t.Fatalf("unexpected page: %#v", page)
	}
}

func TestCompanyDirectoryUserOrgMatchesAnyPosition(t *testing.T) {
	t.Parallel()
	firstPositionID, secondPositionID := uuid.New(), uuid.New()
	firstDepartmentID, secondDepartmentID := uuid.New(), uuid.New()
	directory := companyDirectory{
		positions: map[uuid.UUID]*companyv1.Position{
			firstPositionID:  {Id: firstPositionID.String(), Name: "Первая", DepartmentId: firstDepartmentID.String()},
			secondPositionID: {Id: secondPositionID.String(), Name: "Нужная", DepartmentId: secondDepartmentID.String()},
		},
		departments: map[uuid.UUID]*companyv1.Department{
			firstDepartmentID:  {Id: firstDepartmentID.String(), Name: "Первый отдел"},
			secondDepartmentID: {Id: secondDepartmentID.String(), Name: "Нужный отдел"},
		},
	}
	user := &companyv1.User{PositionIds: []string{firstPositionID.String(), secondPositionID.String()}}

	positionID, departmentID, positionName, departmentName, ok := directory.userOrg(user, &secondPositionID, nil)
	if !ok || positionID == nil || *positionID != secondPositionID || departmentID == nil || *departmentID != secondDepartmentID ||
		positionName == nil || *positionName != "Нужная" || departmentName == nil || *departmentName != "Нужный отдел" {
		t.Fatalf("position match = %v %v %v %v %v", positionID, departmentID, positionName, departmentName, ok)
	}

	positionID, departmentID, _, _, ok = directory.userOrg(user, nil, &secondDepartmentID)
	if !ok || positionID == nil || *positionID != secondPositionID || departmentID == nil || *departmentID != secondDepartmentID {
		t.Fatalf("department match = %v %v %v", positionID, departmentID, ok)
	}
}

func TestRequireAcademyManagerUsesNeutralMessage(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/academy/courses/id/assignments", nil)
	if requireAcademyManager(recorder, request) {
		t.Fatal("guard unexpectedly allowed request without claims")
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Действие доступно") || strings.Contains(body, "Отчёт") {
		t.Fatalf("unexpected guard response: %s", body)
	}
}
