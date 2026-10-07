package corebridge

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Scope struct {
	CompanyID       uuid.UUID `json:"companyId"`
	BindingID       uuid.UUID `json:"bindingId"`
	BindingRevision int64     `json:"bindingRevision"`
	InstallationID  uuid.UUID `json:"installationId"`
	IntegrationID   uuid.UUID `json:"integrationId"`
	AccountID       string    `json:"accountId"`
}
type Binding struct {
	Scope
	State string `json:"state"`
}
type Confirmation struct {
	Scope
	IntentID    uuid.UUID `json:"intentId"`
	WidgetToken string    `json:"widgetToken"`
	ExpiresAt   time.Time `json:"expiresAt"`
}
type User struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	IsActive bool    `json:"isActive"`
	GroupID  *string `json:"groupId"`
}
type Status struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Pipeline struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Statuses []Status `json:"statuses"`
}
type References struct {
	AccountDomain     string     `json:"accountDomain,omitempty"`
	Timezone          string     `json:"timezone,omitempty"`
	TimezoneFetchedAt time.Time  `json:"timezoneFetchedAt,omitempty"`
	Users             []User     `json:"users"`
	Pipelines         []Pipeline `json:"pipelines"`
	FetchedAt         time.Time  `json:"fetchedAt"`
	FreshUntil        time.Time  `json:"freshUntil"`
	State             string     `json:"state"`
}
type Mapping struct {
	EmployeeID uuid.UUID `json:"employeeId"`
	UserID     string    `json:"userId"`
}
type PermissionInput struct {
	EmployeeID uuid.UUID `json:"employeeId"`
	UserID     string    `json:"userId"`
	LeadID     string    `json:"leadId"`
}
type Permission struct {
	UserID      string    `json:"userId"`
	CanViewLead bool      `json:"canViewLead"`
	Reason      string    `json:"reason"`
	CheckedAt   time.Time `json:"checkedAt"`
}
type Error struct{ Status int }

func (e *Error) Error() string { return fmt.Sprintf("Core HTTP %d", e.Status) }

type Client struct {
	base, keyID, secret string
	http                *http.Client
	now                 func() time.Time
}

func New(base, keyID, secret string) (*Client, error) {
	u, e := url.Parse(base)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(secret) < 32 || keyID == "" {
		return nil, errors.New("некорректная конфигурация Core distribution")
	}
	return &Client{strings.TrimRight(base, "/"), keyID, secret, &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, time.Now}, nil
}
func Signature(keyID, secret, method, uri, company, installation, timestamp, nonce string, body []byte) string {
	d := sha256.Sum256(body)
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(strings.Join([]string{keyID, method, uri, company, installation, timestamp, nonce, hex.EncodeToString(d[:])}, "\n")))
	return hex.EncodeToString(m.Sum(nil))
}
func Sign(r *http.Request, keyID, secret string, s Scope, body []byte, now time.Time) {
	stamp := strconv.FormatInt(now.Unix(), 10)
	n := uuid.NewString()
	r.Header.Set("Content-Type", "application/json")
	for k, v := range map[string]string{"X-Distribution-Key-Id": keyID, "X-Distribution-Company": s.CompanyID.String(), "X-Distribution-Installation": s.InstallationID.String(), "X-Distribution-Timestamp": stamp, "X-Distribution-Nonce": n, "X-Distribution-Signature": Signature(keyID, secret, r.Method, r.URL.RequestURI(), s.CompanyID.String(), s.InstallationID.String(), stamp, n, body)} {
		r.Header.Set(k, v)
	}
}
func (c *Client) call(ctx context.Context, method, path string, s Scope, in, out any) error {
	if c == nil {
		return errors.New("Core не настроен")
	}
	var b []byte
	var e error
	if in != nil {
		b, e = json.Marshal(in)
		if e != nil {
			return e
		}
	}
	r, e := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(b))
	if e != nil {
		return e
	}
	Sign(r, c.keyID, c.secret, s, b, c.now())
	resp, e := c.http.Do(r)
	if e != nil {
		return errors.New("Core недоступен")
	}
	defer func() { _ = resp.Body.Close() }()
	confirmed := resp.StatusCode == 200 || (method == "POST" && path == "/internal/v1/distribution/bindings" && resp.StatusCode == 201)
	if !confirmed {
		return &Error{resp.StatusCode}
	}

	if out != nil {
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
		if readErr != nil || len(raw) > 4<<20 {
			return errors.New("Некорректный или слишком большой ответ Core")
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		switch out.(type) {
		case *LeadObservation, *Operation:
			decoder.DisallowUnknownFields()
		}
		if decoder.Decode(out) != nil {
			return errors.New("Некорректный ответ Core")
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return errors.New("Лишние данные в ответе Core")
		}
	}

	return nil
}
func (c *Client) Confirm(ctx context.Context, in Confirmation) (Binding, error) {
	var o Binding
	e := c.call(ctx, "POST", "/internal/v1/distribution/bindings", in.Scope, in, &o)
	return o, e
}
func (c *Client) GetBinding(ctx context.Context, s Scope) (Binding, error) {
	var o Binding
	e := c.call(ctx, "GET", "/internal/v1/distribution/bindings/"+s.BindingID.String()+"?bindingRevision="+strconv.FormatInt(s.BindingRevision, 10), s, nil, &o)
	return o, e
}
func (c *Client) References(ctx context.Context, s Scope) (References, error) {
	var o References
	e := c.call(ctx, "GET", "/internal/v1/distribution/bindings/"+s.BindingID.String()+"/references?bindingRevision="+strconv.FormatInt(s.BindingRevision, 10), s, nil, &o)
	return o, e
}

func (c *Client) Mappings(ctx context.Context, s Scope, revision int64, m []Mapping) error {
	var ack struct {
		State           string `json:"state"`
		Count           int    `json:"count"`
		MappingRevision int64  `json:"mappingRevision"`
	}
	err := c.call(ctx, "PUT", "/internal/v1/distribution/bindings/"+s.BindingID.String()+"/mappings?bindingRevision="+strconv.FormatInt(s.BindingRevision, 10), s, struct {
		MappingRevision int64     `json:"mappingRevision"`
		Mappings        []Mapping `json:"mappings"`
	}{revision, m}, &ack)
	if err != nil {
		return err
	}
	if ack.State != "synced" || ack.Count != len(m) || ack.MappingRevision != revision {
		return errors.New("Core не подтвердил версию снимка сопоставлений")
	}
	return nil
}

func (c *Client) Permission(ctx context.Context, s Scope, in PermissionInput) (Permission, error) {
	var o Permission
	e := c.call(ctx, "POST", "/internal/v1/distribution/bindings/"+s.BindingID.String()+"/permissions?bindingRevision="+strconv.FormatInt(s.BindingRevision, 10), s, in, &o)
	return o, e
}

// DPCredential is the per-group Digital Pipeline credential minted by Core.
// Core persists only sha256(key) + a sealed copy, so re-issuing returns the
// same value without storing plaintext at rest.
type DPCredential struct {
	Key             string `json:"key"`
	GroupID         string `json:"groupId"`
	BindingRevision int64  `json:"bindingRevision"`
}

func (c *Client) DPCredential(ctx context.Context, s Scope, groupID uuid.UUID) (DPCredential, error) {
	var o DPCredential
	e := c.call(ctx, "POST", "/internal/v1/distribution/bindings/"+s.BindingID.String()+"/dp-credentials?bindingRevision="+strconv.FormatInt(s.BindingRevision, 10), s, struct {
		GroupID uuid.UUID `json:"groupId"`
	}{groupID}, &o)
	return o, e
}

func (c *Client) Revoke(ctx context.Context, s Scope) error {
	var ack struct {
		BindingID uuid.UUID `json:"bindingId"`
		State     string    `json:"state"`
	}
	err := c.call(ctx, "POST", "/internal/v1/distribution/bindings/"+s.BindingID.String()+"/revoke", s, nil, &ack)
	if err != nil {
		return err
	}
	if ack.BindingID != s.BindingID || ack.State != "revoked" {
		return errors.New("Core не подтвердил отзыв связи")
	}
	return nil
}
