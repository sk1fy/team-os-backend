package widgetentitlement

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientCheck(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		body          string
		wantInstalled bool
		wantPaid      bool
		wantErr       bool
	}{
		{name: "paid widget", body: `{"success":true,"data":[{"widget_code":"rkrs_activity","status":1,"paid_to":"2026-06-18 23:59:59","widget_paid_to":"2027-12-22 23:59:59"}]}`, wantInstalled: true, wantPaid: true},
		{name: "missing widget", body: `{"success":true,"data":[{"widget_code":"other","status":1,"widget_paid_to":"2027-12-22 23:59:59"}]}`},
		{name: "disabled widget", body: `{"success":true,"data":[{"widget_code":"rkrs_activity","status":0,"widget_paid_to":"2027-12-22 23:59:59"}]}`},
		{name: "expired payment", body: `{"success":true,"data":[{"widget_code":"rkrs_activity","status":1,"widget_paid_to":"2026-08-10 14:59:59"}]}`, wantInstalled: true},
		{name: "invalid payment", body: `{"success":true,"data":[{"widget_code":"rkrs_activity","status":1,"widget_paid_to":"tomorrow"}]}`, wantErr: true},
		{name: "rejected envelope", body: `{"success":false,"data":[]}`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			client, err := NewClient(Config{
				BaseURL: server.URL, AppName: "rkrs_activity", Timeout: time.Second,
				CacheTTL: 5 * time.Minute, Timezone: "Europe/Moscow", Now: func() time.Time { return now },
			})
			if err != nil {
				t.Fatal(err)
			}
			installed, paid, err := client.Check(context.Background(), "31355990")
			if (err != nil) != test.wantErr || installed != test.wantInstalled || paid != test.wantPaid {
				t.Fatalf("installed=%v paid=%v error=%v", installed, paid, err)
			}
		})
	}
}

func TestClientCachesAccountResult(t *testing.T) {
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = fmt.Fprint(w, `{"success":true,"data":[{"widget_code":"rkrs_activity","status":1,"widget_paid_to":"2027-12-22 23:59:59"}]}`)
	}))
	defer server.Close()
	client, err := NewClient(Config{
		BaseURL: server.URL, AppName: "rkrs_activity", Timeout: time.Second,
		CacheTTL: 5 * time.Minute, Timezone: "Europe/Moscow", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, _, err = client.Check(context.Background(), "31355990"); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d, want 1", calls.Load())
	}
}
