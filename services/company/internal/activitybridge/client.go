package activitybridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	dto "github.com/sk1fy/team-os-backend/services/company/internal/activitycontract"
)

type Client struct {
	base, token string
	http        *http.Client
}
type Scope = dto.ActivityScope
type Error struct {
	Status     int
	RetryAfter string
	Cause      error
}

func (e *Error) Unwrap() error { return e.Cause }
func (e *Error) Error() string { return fmt.Sprintf("Activity upstream status %d", e.Status) }
func New(base, token string) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || len(token) < 32 {
		return nil, errors.New("invalid Activity server configuration")
	}
	return &Client{base: base, token: token, http: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) call(ctx context.Context, scope Scope, method, path, command string, input, out any, required ...string) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1/activity/"+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Activity-Installation-Id", scope.InstallationId.String())
	req.Header.Set("X-Activity-Integration-Id", scope.IntegrationId.String())
	req.Header.Set("X-Activity-Account-Id", scope.AccountId)
	req.Header.Set("Content-Type", "application/json")
	if command != "" {
		req.Header.Set("Idempotency-Key", command)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &Error{Status: 503, Cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{Status: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After")}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return &Error{Status: 502}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return &Error{Status: 502}
	}
	for _, name := range required {
		if value, ok := fields[name]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return &Error{Status: 502}
		}
	}
	if json.Unmarshal(raw, out) != nil {
		return &Error{Status: 502}
	}
	return nil
}
func (c *Client) Verify(ctx context.Context, s Scope) error {
	var out struct {
		InstallationID uuid.UUID `json:"installation_id"`
		IntegrationID  uuid.UUID `json:"integration_id"`
		AccountID      string    `json:"account_id"`
		State          string    `json:"state"`
	}
	if err := c.call(ctx, s, "GET", "connection", "", nil, &out, "installation_id", "integration_id", "account_id", "state"); err != nil {
		return err
	}
	if out.InstallationID != s.InstallationId || out.IntegrationID != s.IntegrationId || out.AccountID != s.AccountId || out.State != "active" {
		return &Error{Status: 403}
	}
	return nil
}
func (c *Client) Panels(ctx context.Context, s Scope) (dto.ActivityPanels, error) {
	var out struct {
		Panels []panel `json:"panels"`
	}
	err := c.call(ctx, s, "GET", "panels", "", nil, &out, "panels")
	result := dto.ActivityPanels{Panels: []dto.ActivityPanel{}}
	for _, p := range out.Panels {
		result.Panels = append(result.Panels, p.public())
	}
	return result, err
}
func (c *Client) Create(ctx context.Context, s Scope, key string, in dto.ActivityPanelInput) (dto.ActivityPanel, error) {
	var out panel
	err := c.call(ctx, s, "POST", "panels", key, panelInput(in), &out, "id", "revision", "name", "employee_ids", "options", "display_window", "share_url")
	return out.public(), err
}
func (c *Client) Update(ctx context.Context, s Scope, id uuid.UUID, in dto.ActivityPanelPatch) (dto.ActivityPanel, error) {
	body := panelInput(dto.ActivityPanelInput{Name: in.Name, EmployeeIds: in.EmployeeIds, DisplayWindow: in.DisplayWindow, Enabled: in.Enabled, Options: in.Options})
	body.Revision = in.Revision
	var out panel
	err := c.call(ctx, s, "PATCH", "panels/"+id.String(), "", body, &out, "id", "revision", "name", "employee_ids", "options", "display_window")
	return out.public(), err
}
func (c *Client) Share(ctx context.Context, s Scope, id uuid.UUID, key string) (dto.ActivityPanel, error) {
	var out panel
	err := c.call(ctx, s, "POST", "panels/"+id.String()+"/share-link", key, nil, &out, "id", "revision", "share_url")
	return out.public(), err
}
func (c *Client) Employees(ctx context.Context, s Scope) (dto.ActivityDirectory, error) {
	var out directory
	err := c.call(ctx, s, "GET", "employees", "", nil, &out, "users", "timezone")
	return out.public(), err
}
func (c *Client) TaskPanel(ctx context.Context, s Scope) (dto.ActivityTaskPanel, error) {
	var out taskPanel
	err := c.call(ctx, s, "GET", "task-panel", "", nil, &out, "revision", "enabled", "employee_ids", "priority_keys")
	return out.public(), err
}
func (c *Client) SaveTaskPanel(ctx context.Context, s Scope, in dto.ActivityTaskPanel) (dto.ActivityTaskPanel, error) {
	var out taskPanel
	err := c.call(ctx, s, "PATCH", "task-panel", "", taskPanelInput(in), &out, "revision", "enabled", "employee_ids", "priority_keys")
	return out.public(), err
}
