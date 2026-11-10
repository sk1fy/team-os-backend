//go:build integration

package application

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/taskcontrolbridge"
)

type taskControlStatusReply struct {
	body string
	code int
}

func TestTaskControlHTTPStatusGuardsLinkTransaction(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, ownerID, installation, integration := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	actor := Actor{CompanyID: company, UserID: ownerID, Role: "owner"}
	seedAccessCompany(t, ctx, pool, company, ownerID, []accessTestUser{{ownerID, "owner", "active"}})
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO company_integrations(id,company_id,provider,external_account_id,status) VALUES($1,$2,'rakurs','123','active')`, uuid.New(), company)
	neighbor, neighborOwner, neighborBinding := uuid.New(), uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, neighbor, neighborOwner, []accessTestUser{{neighborOwner, "owner", "active"}})
	exec(`INSERT INTO task_control_connections(company_id,id,installation_id,integration_id,account_id,generation,intent_id,linked_by,crm_admin_id)
		VALUES($1,$2,$3,$4,'456',1,$5,$6,'8')`, neighbor, neighborBinding, uuid.New(), uuid.New(), uuid.New(), neighborOwner)
	neighborState := func() string {
		t.Helper()
		var raw string
		if err := pool.QueryRow(ctx, `SELECT to_jsonb(c)::text FROM task_control_connections c WHERE company_id=$1 AND id=$2`, neighbor, neighborBinding).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	beforeNeighbor := neighborState()
	now := time.Now().UTC().Truncate(time.Second)
	status := tc.Installation{CompanyID: company, AccountID: "123", InstallationID: &installation, IntegrationID: &integration, Product: tc.Product, State: "installed", Revision: 1, Generation: 1, CheckedAt: now, FreshUntil: now.Add(5 * time.Minute), RuntimeVersion: "1.2.5", RuntimeCapabilities: []string{"dynamicRulesV2"}}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("s", 32)
	var reply atomic.Pointer[taskControlStatusReply]
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		bodyHash := sha256.Sum256(nil)
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(strings.Join([]string{"service", http.MethodGet, r.URL.RequestURI(), company.String(), "123", tc.Product, r.Header.Get("X-Task-Control-Timestamp"), r.Header.Get("X-Task-Control-Nonce"), hex.EncodeToString(bodyHash[:])}, "\n")))
		if r.URL.Path != "/internal/v1/task-control/accounts/123/installation" || r.Header.Get("X-Task-Control-Company") != company.String() || r.Header.Get("X-Task-Control-Account") != "123" || r.Header.Get("X-Task-Control-Product") != tc.Product || r.Header.Get("X-Task-Control-Key-Id") != "service" || r.Header.Get("X-Task-Control-Signature") != hex.EncodeToString(mac.Sum(nil)) {
			t.Error("Core status request lost signed scope")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		value := reply.Load()
		w.WriteHeader(value.code)
		_, _ = w.Write([]byte(value.body))
	}))
	defer server.Close()
	client, err := taskcontrolbridge.New(server.URL, "service", secret)
	if err != nil {
		t.Fatal(err)
	}
	client.HTTP = server.Client()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{pool: pool, now: func() time.Time { return now }, taskControlCore: client, taskControlKeys: map[string]ed25519.PublicKey{"signer": publicKey}}
	intent := uuid.New()
	proof := tc.Assertion{Audience: "teamos-task-control", ID: uuid.NewString(), IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), CompanyID: company, InstallationID: installation, IntegrationID: integration, AccountID: "123", ActorID: "7", Product: tc.Product, Generation: 1, Admin: true, Purpose: "link", RequestID: intent}
	sign := func(a tc.Assertion) string {
		t.Helper()
		body, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		prefix := "signer." + base64.RawURLEncoding.EncodeToString(body)
		return prefix + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, []byte("teamos-task-control/v1\n"+prefix)))
	}
	input := TaskControlLinkInput{IntentID: intent, InstallationID: installation, IntegrationID: integration, AccountID: "123", CoreAssertion: sign(proof)}
	assertNoLink := func() {
		t.Helper()
		var bindings, uses int
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM task_control_connections WHERE company_id=$1),(SELECT count(*) FROM task_control_assertion_uses WHERE company_id=$1)`, company).Scan(&bindings, &uses); err != nil {
			t.Fatal(err)
		}
		if bindings != 0 || uses != 0 {
			t.Fatalf("failed status created bindings=%d or consumed assertions=%d", bindings, uses)
		}
		if neighborState() != beforeNeighbor {
			t.Fatal("neighbor binding changed")
		}
	}
	for _, test := range []struct {
		name, body string
		code       int
	}{
		{name: "oversized status", body: string(raw) + strings.Repeat(" ", 65537), code: http.StatusOK},
		{name: "extra document", body: string(raw) + `{}`, code: http.StatusOK},
		{name: "source outage", body: string(raw), code: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			reply.Store(&taskControlStatusReply{body: test.body, code: test.code})
			before := requests.Load()
			if _, err := service.LinkTaskControl(ctx, actor, input); !isCompanyError(err, ErrorUpstream) {
				t.Fatalf("invalid Core status accepted: %v", err)
			}
			if requests.Load() != before+1 {
				t.Fatal("link did not request fresh Core status")
			}
			assertNoLink()
		})
	}
	reply.Store(&taskControlStatusReply{body: string(raw), code: http.StatusOK})
	for _, scenario := range []string{"expired", "foreign company", "generation", "non-admin"} {
		t.Run(scenario, func(t *testing.T) {
			invalid := proof
			switch scenario {
			case "expired":
				invalid.ExpiresAt = now.Unix()
			case "foreign company":
				invalid.CompanyID = neighbor
			case "generation":
				invalid.Generation++
			case "non-admin":
				invalid.Admin = false
			}
			bad := input
			bad.CoreAssertion = sign(invalid)
			if _, err := service.LinkTaskControl(ctx, actor, bad); err == nil {
				t.Fatal("invalid assertion linked installation")
			}
			assertNoLink()
		})
	}
	linked, err := service.LinkTaskControl(ctx, actor, input)
	if err != nil {
		t.Fatal(err)
	}
	if linked.Scope.CompanyID != company || linked.Scope.InstallationID != installation || linked.Scope.IntegrationID != integration || linked.Scope.AccountID != "123" || linked.Scope.Generation != 1 {
		t.Fatalf("wrong binding scope: %+v", linked.Scope)
	}
	repeated, err := service.LinkTaskControl(ctx, actor, input)
	if err != nil || repeated.Scope != linked.Scope {
		t.Fatalf("same intent was not idempotent: %+v, %v", repeated, err)
	}
	var uses int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_control_assertion_uses WHERE company_id=$1`, company).Scan(&uses); err != nil {
		t.Fatal(err)
	}
	if uses != 1 || neighborState() != beforeNeighbor {
		t.Fatal("successful link replay changed assertion or neighbor state")
	}
}
