package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

func TestExternalOutgoingContextForwardsOnlyExternalCookie(t *testing.T) {
	handler := &Handler{}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/public/academy/enrollments/id", nil)
	request.AddCookie(&http.Cookie{Name: externalSessionCookieName, Value: "opaque-session"})
	ctx := handler.externalOutgoingContext(request)
	values, ok := metadata.FromOutgoingContext(ctx)
	if !ok || len(values.Get("x-external-session")) != 1 || values.Get("x-external-session")[0] != "opaque-session" {
		t.Fatalf("external metadata=%v", values)
	}
}

func TestExternalCSRFMustMatchConfiguredPublicOrigin(t *testing.T) {
	handler := &Handler{cookie: CookieConfig{PublicAppURL: "https://academy.example.test/app"}}
	for _, testCase := range []struct {
		name   string
		origin string
		want   bool
	}{
		{name: "same origin", origin: "https://academy.example.test", want: true},
		{name: "foreign", origin: "https://attacker.example", want: false},
		{name: "missing", want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/public/academy/access/token/activate", nil)
			if testCase.origin != "" {
				request.Header.Set("Origin", testCase.origin)
			}
			response := httptest.NewRecorder()
			if got := handler.requireExternalCSRF(response, request); got != testCase.want {
				t.Fatalf("requireExternalCSRF=%v want=%v", got, testCase.want)
			}
			if !testCase.want && response.Code != http.StatusForbidden {
				t.Fatalf("status=%d", response.Code)
			}
		})
	}
}

func TestExternalSessionCookieIsScopedAndHttpOnly(t *testing.T) {
	handler := &Handler{cookie: CookieConfig{Secure: true}}
	response := httptest.NewRecorder()
	handler.setExternalSessionCookie(response, "opaque-session", time.Now().Add(time.Hour))
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != externalSessionCookieName || cookie.Path != "/api/v1/public/academy" ||
		!cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie=%+v", cookie)
	}
}

func TestAcademyVisitorCookieIsRandomScopedAndForwardedOnlyAsHash(t *testing.T) {
	t.Parallel()

	handler := &Handler{cookie: CookieConfig{Secure: true}}
	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/public/academy/access/token", nil)
	hash, err := handler.academyVisitorHash(response, request)
	if err != nil {
		t.Fatal(err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != academyVisitorCookieName || cookie.Path != "/api/v1/public/academy" ||
		!cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie=%+v", cookie)
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(raw) != academyVisitorBytes {
		t.Fatalf("visitor raw length=%d err=%v", len(raw), err)
	}
	want := sha256.Sum256(raw)
	if !bytes.Equal(hash, want[:]) || bytes.Equal(hash, raw) {
		t.Fatal("gateway did not isolate raw visitor cookie from forwarded hash")
	}

	reusedResponse := httptest.NewRecorder()
	reusedRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/public/academy/access/token", nil)
	reusedRequest.AddCookie(cookie)
	reusedHash, err := handler.academyVisitorHash(reusedResponse, reusedRequest)
	if err != nil || !bytes.Equal(reusedHash, hash) || len(reusedResponse.Result().Cookies()) != 0 {
		t.Fatalf("reused hash/cookies = %x/%d err=%v", reusedHash, len(reusedResponse.Result().Cookies()), err)
	}
}

func TestAcademyAttributionExcludesQueryFromReferrerAndIgnoresIP(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/public/academy/access/token?utm_source=news&utm_medium=email&utm_campaign=july", nil)
	request.Header.Set("Referer", "https://partner.example/path?email=private@example.com#fragment")
	request.Header.Set("X-Forwarded-For", "203.0.113.7")
	source, medium, campaign, content, term, referrer := academyAttribution(request)
	if source == nil || *source != "news" || medium == nil || *medium != "email" ||
		campaign == nil || *campaign != "july" || content != nil || term != nil ||
		referrer == nil || *referrer != "https://partner.example/path" {
		t.Fatalf("attribution=%v/%v/%v/%v/%v/%v", source, medium, campaign, content, term, referrer)
	}
}
