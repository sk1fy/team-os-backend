package externalusers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientFetchAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["amo_account_id"] != "31355990" || body["app_name"] != "rkrs_activity" {
			t.Fatalf("unexpected body: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":true,"message":" ","data":[{"id":"198392","name":"Максим","email":"6001517@gmail.com","avatar":"/v3/users/avatar/","group":{"id":"group_0","name":"Отдел продаж"}}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{APIURL: server.URL, AppName: "rkrs_activity", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	users, err := client.FetchAll(context.Background(), "31355990")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != "198392" || users[0].GroupName != "Отдел продаж" {
		t.Fatalf("unexpected users: %#v", users)
	}
	if users[0].AvatarURL == nil || *users[0].AvatarURL != "https://31355990.amocrm.ru/v3/users/avatar/" {
		t.Fatalf("unexpected avatar: %#v", users[0].AvatarURL)
	}
}

func TestClientFetchAllRejectsBusinessErrorAndMissingData(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "business error", body: `{"result":false,"message":"Нет доступа","data":[]}`},
		{name: "missing data", body: `{"result":true}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client, err := NewClient(Config{APIURL: server.URL, AppName: "rkrs_activity", Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = client.FetchAll(context.Background(), "31355990"); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestClientFetchAllResponseBoundary(t *testing.T) {
	valid := `{"result":true,"data":[]}`
	tests := []struct {
		name   string
		body   string
		status int
		wantOK bool
	}{
		{name: "empty snapshot", body: valid, wantOK: true},
		{name: "whitespace", body: " \n" + valid + "\t\n", wantOK: true},
		{name: "unknown fields", body: `{"result":true,"data":[],"future":{"value":1}}`, wantOK: true},
		{name: "exact limit", body: valid + strings.Repeat(" ", maxResponseBytes-len(valid)), wantOK: true},
		{name: "over limit whitespace", body: valid + strings.Repeat(" ", maxResponseBytes-len(valid)+1)},
		{name: "over limit document", body: `{"result":true,"data":[],"padding":"` + strings.Repeat("x", maxResponseBytes) + `"}`},
		{name: "trailing garbage", body: valid + "garbage"},
		{name: "second document", body: valid + valid},
		{name: "truncated", body: `{"result":true,"data":[`},
		{name: "empty body"},
		{name: "wrong data type", body: `{"result":true,"data":{}}`},
		{name: "null data", body: `{"result":true,"data":null}`},
		{name: "missing data", body: `{"result":true}`},
		{name: "business error", body: `{"result":false,"data":[]}`},
		{name: "rate limited", body: valid, status: http.StatusTooManyRequests},
		{name: "source unavailable", body: valid, status: http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
					t.Error("missing JSON request headers")
				}
				if test.status != 0 {
					w.WriteHeader(test.status)
				}
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client, err := NewClient(Config{APIURL: server.URL, AppName: "rkrs_activity", Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			users, err := client.FetchAll(t.Context(), "31355990")
			if (err == nil) != test.wantOK {
				t.Fatalf("FetchAll error=%v, want success=%t", err, test.wantOK)
			}
			if !test.wantOK && users != nil {
				t.Fatalf("rejected response returned employees: %v", users)
			}
		})
	}
}

func TestClientFetchAllCancellationAndReadFailure(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			close(started)
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}))
		defer server.Close()
		defer close(release)
		client, err := NewClient(Config{APIURL: server.URL, AppName: "rkrs_activity", Timeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		result := make(chan error, 1)
		go func() {
			_, fetchErr := client.FetchAll(ctx, "31355990")
			result <- fetchErr
		}()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("HTTP request did not start")
		}
		cancel()
		if err = <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v, want context cancellation", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}))
		defer server.Close()
		defer close(release)
		client, err := NewClient(Config{APIURL: server.URL, AppName: "rkrs_activity", Timeout: 30 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = client.FetchAll(t.Context(), "31355990"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error=%v, want timeout", err)
		}
	})
	t.Run("short body after valid JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte(`{"result":true,"data":[]}`))
		}))
		defer server.Close()
		client, err := NewClient(Config{APIURL: server.URL, AppName: "rkrs_activity", Timeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = client.FetchAll(t.Context(), "31355990"); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("error=%v, want incomplete response error", err)
		}
	})
}
