package corebridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPermissionBatchSignedExactResultsAndRolloutFallback(t *testing.T) {
	for _, kind := range []string{"batch", "duplicate", "foreign", "missing", "legacy", "denied", "unavailable"} {
		t.Run(kind, func(t *testing.T) {
			scope := Scope{CompanyID: uuid.New(), BindingID: uuid.New(), BindingRevision: 7, InstallationID: uuid.New()}
			secret := strings.Repeat("s", 32)
			var single atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				if r.Header.Get("X-Distribution-Signature") != Signature("k", secret, r.Method, r.URL.RequestURI(), scope.CompanyID.String(), scope.InstallationID.String(), r.Header.Get("X-Distribution-Timestamp"), r.Header.Get("X-Distribution-Nonce"), raw) || r.URL.Query().Get("bindingRevision") != "7" {
					t.Error("unsigned scope")
				}
				permission := Permission{UserID: "42", CheckedAt: time.Now(), CanViewLead: true}
				if !strings.HasSuffix(r.URL.Path, "/batch") {
					single.Add(1)
					_ = json.NewEncoder(w).Encode(permission)
					return
				}
				switch kind {
				case "legacy":
					w.WriteHeader(404)
					return
				case "denied":
					w.WriteHeader(403)
					return
				case "unavailable":
					w.WriteHeader(503)
					return
				}
				ids := []string{"10", "11"}
				switch kind {
				case "duplicate":
					ids[1] = "10"
				case "foreign":
					ids[1] = "12"
				case "missing":
					ids = ids[:1]
				}
				items := []map[string]any{}
				for _, id := range ids {
					items = append(items, map[string]any{"leadId": id, "userId": "42", "checkedAt": permission.CheckedAt, "canViewLead": true})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
			}))
			defer server.Close()
			client, err := New(server.URL, "k", secret)
			if err != nil {
				t.Fatal(err)
			}
			client.http = server.Client()
			result, err := client.Permissions(context.Background(), scope, PermissionInput{EmployeeID: uuid.New(), UserID: "42"}, []string{"10", "11"})
			success := kind == "batch" || kind == "legacy"
			if (err == nil) != success || success && (len(result) != 2 || !result["10"].CanViewLead) {
				t.Fatalf("%v %v", result, err)
			}
			want := int32(0)
			if kind == "legacy" {
				want = 2
			}
			if single.Load() != want {
				t.Fatal("unsafe fallback", single.Load())
			}
		})
	}
}
