package amochallenge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChallengeRoundTripAndReplay(t *testing.T) {
	manager, err := New(strings.Repeat("s", 32), 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	token, err := manager.Issue("31355990")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := manager.VerifyAndConsume(token)
	if err != nil || claims.AmoAccountID != "31355990" || claims.Purpose != purpose || claims.Version != 1 {
		t.Fatalf("claims=%#v error=%v", claims, err)
	}
	if _, err = manager.VerifyAndConsume(token); !errors.Is(err, ErrInvalid) {
		t.Fatalf("replay error=%v", err)
	}
}

func TestMiddlewareIsExactAndFailsClosedWhenDisabled(t *testing.T) {
	called := false
	handler := Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	other := httptest.NewRecorder()
	handler.ServeHTTP(other, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/public/amocrm/other", nil))
	if other.Code != http.StatusNoContent || !called {
		t.Fatalf("other status=%d called=%v", other.Code, called)
	}
	called = false
	disabled := httptest.NewRecorder()
	handler.ServeHTTP(disabled, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/public/amocrm/admin-self-login", nil))
	if disabled.Code != http.StatusNotFound || called {
		t.Fatalf("disabled status=%d called=%v", disabled.Code, called)
	}
}

func TestChallengeRejectsTamperingAndExpiry(t *testing.T) {
	manager, err := New(strings.Repeat("s", 32), 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	token, err := manager.Issue("31355990")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	parts[1] = "e30"
	if _, err = manager.VerifyAndConsume(strings.Join(parts, ".")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tamper error=%v", err)
	}
	manager.now = func() time.Time { return now.Add(3 * time.Minute) }
	if _, err = manager.VerifyAndConsume(token); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expiry error=%v", err)
	}
}

func TestChallengeConfigurationAndAccountValidation(t *testing.T) {
	if _, err := New("short", time.Minute); err == nil {
		t.Fatal("short secret accepted")
	}
	manager, err := New(strings.Repeat("s", 32), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Issue("account"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("account error=%v", err)
	}
}
