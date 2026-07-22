package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func TestLegacyAcademyDeprecationHeadersDoNotBlockByDefault(t *testing.T) {
	t.Setenv(legacyAcademyWritesReadOnlyEnv, "false")

	response := httptest.NewRecorder()
	if guardLegacyAcademyWrite(response, academyCoursesSuccessor) {
		t.Fatal("legacy write was blocked before the cutover toggle")
	}
	assertLegacyAcademyHeaders(t, response, academyCoursesSuccessor)
}

func TestLegacyAcademyWriteGuardBlocksEveryLegacyMutationAfterCutover(t *testing.T) {
	t.Setenv(legacyAcademyWritesReadOnlyEnv, "true")

	handler := &Handler{}
	id := api.ID(uuid.MustParse("11111111-1111-4111-8111-111111111111"))
	testCases := []struct {
		name      string
		successor string
		call      func(http.ResponseWriter, *http.Request)
	}{
		{name: "course from kb", successor: academyCoursesSuccessor, call: handler.CreateCourseFromKb},
		{name: "course metadata", successor: academyCoursesSuccessor, call: func(w http.ResponseWriter, r *http.Request) { handler.UpdateCourse(w, r, id) }},
		{name: "create section", successor: academyCoursesSuccessor, call: func(w http.ResponseWriter, r *http.Request) { handler.CreateCourseSection(w, r, id) }},
		{name: "update section", successor: academyCoursesSuccessor, call: func(w http.ResponseWriter, r *http.Request) { handler.UpdateCourseSection(w, r, id) }},
		{name: "delete section", successor: academyCoursesSuccessor, call: func(w http.ResponseWriter, r *http.Request) { handler.DeleteCourseSection(w, r, id) }},
		{name: "create lesson", successor: academyCoursesSuccessor, call: handler.CreateLesson},
		{name: "update lesson", successor: academyCoursesSuccessor, call: func(w http.ResponseWriter, r *http.Request) { handler.UpdateLesson(w, r, id) }},
		{name: "delete lesson", successor: academyCoursesSuccessor, call: func(w http.ResponseWriter, r *http.Request) { handler.DeleteLesson(w, r, id) }},
		{name: "move lesson", successor: academyCoursesSuccessor, call: func(w http.ResponseWriter, r *http.Request) { handler.MoveLesson(w, r, id) }},
		{name: "upsert quiz", successor: academyCoursesSuccessor, call: handler.UpsertQuiz},
		{name: "complete legacy lesson", successor: academyEnrollmentsSuccessor, call: func(w http.ResponseWriter, r *http.Request) { handler.MarkLessonComplete(w, r, id) }},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/academy/legacy", nil)
			testCase.call(response, request)

			if response.Code != http.StatusGone {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), "Запись через устаревший API Академии отключена") {
				t.Fatalf("body=%s", response.Body.String())
			}
			assertLegacyAcademyHeaders(t, response, testCase.successor)
		})
	}
}

func TestLegacyAcademyWriteGuardFailsClosedForMalformedToggle(t *testing.T) {
	t.Setenv(legacyAcademyWritesReadOnlyEnv, "treu")

	response := httptest.NewRecorder()
	if !guardLegacyAcademyWrite(response, academyCoursesSuccessor) {
		t.Fatal("malformed non-empty toggle reopened legacy writes")
	}
	if response.Code != http.StatusGone {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPublicCourseByIDDeprecationPointsToTokenAccess(t *testing.T) {
	response := httptest.NewRecorder()
	markLegacyAcademyEndpoint(response, publicAcademyAccessSuccessor)
	assertLegacyAcademyHeaders(t, response, publicAcademyAccessSuccessor)
}

func assertLegacyAcademyHeaders(t *testing.T, response *httptest.ResponseRecorder, successor string) {
	t.Helper()
	if got := response.Header().Get("Deprecation"); got != legacyAcademyDeprecatedAt {
		t.Fatalf("Deprecation=%q", got)
	}
	if got := response.Header().Get("Sunset"); got != legacyAcademySunset {
		t.Fatalf("Sunset=%q", got)
	}
	if got := response.Header().Get("Warning"); got != legacyAcademyWarning {
		t.Fatalf("Warning=%q", got)
	}
	wantLink := "<" + successor + ">; rel=\"successor-version\""
	if got := response.Header().Get("Link"); got != wantLink {
		t.Fatalf("Link=%q want=%q", got, wantLink)
	}
	if got := response.Header().Values("Access-Control-Expose-Headers"); len(got) != 1 || got[0] != "Deprecation, Sunset, Link, Warning" {
		t.Fatalf("Access-Control-Expose-Headers=%q", got)
	}
}
