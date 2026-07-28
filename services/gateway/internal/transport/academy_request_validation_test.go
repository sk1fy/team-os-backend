package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUpdateCourseRejectsUnappliedFieldsBeforeRPC(t *testing.T) {
	t.Setenv(legacyAcademyWritesReadOnlyEnv, "false")

	id := api.ID(uuid.MustParse("11111111-1111-4111-8111-111111111111"))
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"visibility":"company","futureVisibility":true}`},
		{name: "misnamed field", body: `{"course_visibility":"company"}`},
		{name: "wrong case", body: `{"Visibility":"company"}`},
		{name: "empty update", body: `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(
				context.Background(), http.MethodPatch, "/api/v1/academy/courses/"+id.String(), strings.NewReader(tt.body),
			)
			response := httptest.NewRecorder()

			(&Handler{}).UpdateCourse(response, request, id)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestUpdateCourseDraftRejectsUnappliedFieldsBeforeRPC(t *testing.T) {
	courseID := api.CourseId(uuid.MustParse("11111111-1111-4111-8111-111111111111"))
	tests := []struct {
		name string
		body string
	}{
		{name: "course visibility", body: `{"visibility":"company"}`},
		{name: "unknown field alongside metadata", body: `{"title":"Курс","visibility":"restricted"}`},
		{name: "misnamed field", body: `{"cover_file_id":"22222222-2222-4222-8222-222222222222"}`},
		{name: "wrong case", body: `{"Title":"Курс"}`},
		{name: "empty update", body: `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(
				context.Background(), http.MethodPatch,
				"/api/v1/academy/courses/"+courseID.String()+"/draft", strings.NewReader(tt.body),
			)
			response := httptest.NewRecorder()

			(&Handler{}).UpdateCourseDraft(response, request, courseID)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAcademyPermissionDeniedMapsToForbidden(t *testing.T) {
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/", http.NoBody)
	response := httptest.NewRecorder()

	(&Handler{}).writeAcademyRPCError(
		response,
		request,
		status.Error(codes.PermissionDenied, "Недостаточно прав для изменения видимости этого курса"),
	)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
