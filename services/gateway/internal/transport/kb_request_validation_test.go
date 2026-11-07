package transport

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	kbv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/kb/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"google.golang.org/grpc"
)

func TestKbRejectsInvalidRequestEnumsBeforeRPC(t *testing.T) {
	id := api.Id(uuid.New())
	h := &Handler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	tests := []struct {
		name, body string
		call       func(http.ResponseWriter, *http.Request)
	}{
		{"create section access", `{"name":"Раздел","parentId":null,"access":{"scope":"invalid"}}`, h.CreateSection},
		{"update section access", `{"access":{"scope":"invalid"}}`, func(w http.ResponseWriter, r *http.Request) { h.UpdateSection(w, r, id) }},
		{"create article status", `{"sectionId":"` + id.String() + `","title":"Статья","content":{"type":"doc"},"status":"invalid"}`, h.CreateArticle},
		{"update article status", `{"status":"invalid"}`, func(w http.ResponseWriter, r *http.Request) { h.UpdateArticle(w, r, id, api.UpdateArticleParams{}) }},
		{"create partner access", `{"sectionId":"` + id.String() + `","title":"Статья","content":{"type":"doc"},"status":"draft","partnerAccess":{"mode":"invalid"}}`, h.CreateArticle},
		{"update partner access", `{"partnerAccess":{"mode":"invalid"}}`, func(w http.ResponseWriter, r *http.Request) { h.UpdateArticle(w, r, id, api.UpdateArticleParams{}) }},
		{"create reuse policy", `{"sectionId":"` + id.String() + `","title":"Статья","content":{"type":"doc"},"status":"draft","partnerReusePolicy":"invalid"}`, h.CreateArticle},
		{"update reuse policy", `{"partnerReusePolicy":"invalid"}`, func(w http.ResponseWriter, r *http.Request) { h.UpdateArticle(w, r, id, api.UpdateArticleParams{}) }},
		{"partner policy access", `{"access":{"mode":"invalid"},"reusePolicy":"copy_allowed"}`, func(w http.ResponseWriter, r *http.Request) { h.UpdateArticlePartnerPolicy(w, r, id) }},
		{"partner policy reuse", `{"access":{"mode":"none"},"reusePolicy":"invalid"}`, func(w http.ResponseWriter, r *http.Request) { h.UpdateArticlePartnerPolicy(w, r, id) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			tt.call(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tt.body)))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "unsupported") || strings.Contains(response.Body.String(), "unknown") {
				t.Fatalf("ошибка не локализована: %s", response.Body.String())
			}
		})
	}
}

type invalidKbResponseClient struct{ kbv1.KbServiceClient }

func (invalidKbResponseClient) GetSections(context.Context, *kbv1.GetSectionsRequest, ...grpc.CallOption) (*kbv1.GetSectionsResponse, error) {
	return &kbv1.GetSectionsResponse{Sections: []*kbv1.ArticleSection{nil}}, nil
}
func TestKbInvalidBackendResponseRemainsInternalError(t *testing.T) {
	h := &Handler{kb: invalidKbResponseClient{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	response := httptest.NewRecorder()
	h.GetSections(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
