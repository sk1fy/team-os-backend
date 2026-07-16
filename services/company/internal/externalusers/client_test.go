package externalusers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
