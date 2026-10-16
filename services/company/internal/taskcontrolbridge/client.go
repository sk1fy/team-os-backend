package taskcontrolbridge

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
)

type Client struct {
	URL, KeyID, Secret string
	HTTP               *http.Client
}

func New(base, key, secret string) (*Client, error) {
	u, e := url.Parse(base)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || key == "" || len(secret) < 32 {
		return nil, errors.New("некорректная конфигурация Core Контроля задач")
	}
	return &Client{URL: base, KeyID: key, Secret: secret}, nil
}
func (c *Client) Installation(ctx context.Context, company uuid.UUID, account string) (tc.Installation, error) {
	var out tc.Installation
	if !tc.ValidCRMID(account) {
		return out, errors.New("некорректный аккаунт")
	}
	r, e := http.NewRequestWithContext(ctx, "GET", c.URL+"/internal/v1/task-control/accounts/"+account+"/installation", nil)
	if e != nil {
		return out, e
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := uuid.NewString()
	hash := sha256.Sum256(nil)
	m := hmac.New(sha256.New, []byte(c.Secret))
	m.Write([]byte(strings.Join([]string{c.KeyID, r.Method, r.URL.RequestURI(), company.String(), account, tc.Product, stamp, nonce, hex.EncodeToString(hash[:])}, "\n")))
	for k, v := range map[string]string{"Key-Id": c.KeyID, "Company": company.String(), "Account": account, "Product": tc.Product, "Timestamp": stamp, "Nonce": nonce, "Signature": hex.EncodeToString(m.Sum(nil))} {
		r.Header.Set("X-Task-Control-"+k, v)
	}
	client := http.Client{Timeout: 3 * time.Second}
	if c.HTTP != nil {
		client = *c.HTTP
		client.Timeout = 3 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, e := client.Do(r)
	if e != nil {
		return out, e
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return out, errors.New("Core не подтвердил установку")
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 65537))
	if e = decoder.Decode(&out); e != nil {
		return out, e
	}
	if decoder.Decode(new(any)) != io.EOF || !out.Valid(company, account, time.Now()) {
		return out, errors.New("некорректное подтверждение установки")
	}
	return out, nil
}

// References uses the references capability; installation.read never implies it.
func (c *Client) References(ctx context.Context, company uuid.UUID, account string) (tc.ReferenceSnapshot, error) {
	var out tc.ReferenceSnapshot
	if !tc.ValidCRMID(account) {
		return out, errors.New("некорректный аккаунт")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/internal/task-control/accounts/"+account+"/references", nil)
	if e != nil {
		return out, e
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := uuid.NewString()
	hash := sha256.Sum256(nil)
	m := hmac.New(sha256.New, []byte(c.Secret))
	m.Write([]byte(strings.Join([]string{c.KeyID, r.Method, r.URL.RequestURI(), company.String(), account, tc.Product, stamp, nonce, hex.EncodeToString(hash[:])}, "\n")))
	for k, v := range map[string]string{"Key-Id": c.KeyID, "Company": company.String(), "Account": account, "Product": tc.Product, "Timestamp": stamp, "Nonce": nonce, "Signature": hex.EncodeToString(m.Sum(nil))} {
		r.Header.Set("X-Task-Control-"+k, v)
	}
	client := http.Client{}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	client.Timeout = 30 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, e := client.Do(r)
	if e != nil {
		return out, e
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		code := "references_unavailable"
		switch resp.StatusCode {
		case 401, 403:
			code = "references_denied"
		case 413:
			code = "references_too_large"
		}
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
			Code string `json:"code"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&body) == nil {
			if resp.StatusCode == 503 && (body.Error.Code == "references_incomplete" || body.Code == "references_incomplete") {
				code = "references_incomplete"
			}
			if resp.StatusCode == 401 && body.Error.Code == "invalid_signature" {
				code = "invalid_signature"
			}
		}
		return out, ReferenceError{Code: code}
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, tc.ReferenceMaxBytes+1))
	if e != nil {
		return out, e
	}
	if len(body) > tc.ReferenceMaxBytes {
		return out, ReferenceError{Code: "references_too_large"}
	}
	if e = json.Unmarshal(body, &out); e != nil {
		return out, ReferenceError{Code: "references_incomplete"}
	}
	if out.Scope.CompanyID != company || out.Scope.AccountID != account {
		return out, ReferenceError{Code: "references_scope_mismatch"}
	}
	if e = out.Validate(out.Scope, time.Now()); e != nil {
		return out, ReferenceError{Code: "references_incomplete"}
	}
	return out, nil
}

type ReferenceError struct{ Code string }

func (e ReferenceError) Error() string { return e.Code }

func (e ReferenceError) ReferenceCode() string { return e.Code }
