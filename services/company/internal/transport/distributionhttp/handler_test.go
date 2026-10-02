package distributionhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type testStore struct {
	grant  bool
	nonces map[uuid.UUID]bool
}

func (s *testStore) GetDistributionServiceGrant(context.Context, db.GetDistributionServiceGrantParams) (bool, error) {
	return s.grant, nil
}
func (s *testStore) ClaimDistributionNonce(_ context.Context, p db.ClaimDistributionNonceParams) (int64, error) {
	if s.nonces[p.Nonce] {
		return 0, nil
	}
	s.nonces[p.Nonce] = true
	return 1, nil
}

type testService struct{ calls int }

func (s *testService) DistributionWidgetAccess(_ context.Context, in application.DistributionWidgetAccessInput) (application.DistributionWidgetAccess, error) {
	s.calls++
	return application.DistributionWidgetAccess{Allowed: true, EmployeeID: uuid.New(), Reason: "allowed"}, nil
}
func TestWidgetCallbackSignatureReplayAndGrant(t *testing.T) {
	now := time.Now()
	secret := strings.Repeat("s", 32)
	scope := corebridge.Scope{CompanyID: uuid.New(), BindingID: uuid.New(), BindingRevision: 1, InstallationID: uuid.New(), IntegrationID: uuid.New(), AccountID: "2"}
	body, _ := json.Marshal(application.DistributionWidgetAccessInput{Scope: scope, UserID: "3", LeadID: "4"})
	store := &testStore{grant: true, nonces: map[uuid.UUID]bool{}}
	svc := &testService{}
	h := &Handler{Keys: map[string]string{"old": secret, "new": strings.Repeat("n", 32)}, Store: store, Service: svc, Now: func() time.Time { return now }}
	makeReq := func() *http.Request {
		r := httptest.NewRequestWithContext(context.Background(), "POST", "https://company.example/internal/v1/distribution/widget-access", bytes.NewReader(body))
		corebridge.Sign(r, "old", secret, scope, body, now)
		return r
	}
	request := makeReq()
	headers := request.Header.Clone()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request)
	if w.Code != 200 || svc.calls != 1 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	replay := makeReq()
	replay.Header = headers
	w = httptest.NewRecorder()
	h.ServeHTTP(w, replay)
	if w.Code != 401 {
		t.Fatalf("replay %d", w.Code)
	}
	for _, alter := range []func(*http.Request){func(r *http.Request) { r.Header.Set("X-Distribution-Key-Id", "new") }, func(r *http.Request) { r.Header.Set("X-Distribution-Company", uuid.NewString()) }, func(r *http.Request) { r.Header.Add("X-Distribution-Nonce", uuid.NewString()) }, func(r *http.Request) { corebridge.Sign(r, "old", secret, scope, body, now.Add(-2*time.Minute)) }, func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader("{}")) }} {
		r := makeReq()
		alter(r)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("bad request %d", w.Code)
		}
	}
	store.grant = false
	w = httptest.NewRecorder()
	h.ServeHTTP(w, makeReq())
	if w.Code != 403 || svc.calls != 1 {
		t.Fatalf("grant %d calls %d", w.Code, svc.calls)
	}
}
