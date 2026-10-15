package taskcontrolbridge

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
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
	defer resp.Body.Close()
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
