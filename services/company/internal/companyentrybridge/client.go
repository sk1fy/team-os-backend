package companyentrybridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/sk1fy/team-os-backend/services/company/internal/application"
)

type Client struct {
	endpoint, key string
	http          *http.Client
}

func New(endpoint, key string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/internal/v1/company-entry/subject-check" || len(key) < 32 || strings.IndexFunc(key, unicode.IsSpace) >= 0 {
		return nil, errors.New("Некорректная конфигурация проверки входа amoCRM")
	}
	return &Client{endpoint: endpoint, key: key, http: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Check(ctx context.Context, subject application.AmoEntrySubject) (bool, error) {
	payload, err := json.Marshal(subject)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return false, errors.New("Не удалось подготовить проверку доступа amoCRM")
	}
	request.Header.Set("Authorization", "Service "+c.key)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return false, errors.New("Проверка доступа amoCRM недоступна")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusForbidden {
		return false, nil
	}
	if response.StatusCode != http.StatusOK {
		return false, errors.New("Проверка доступа amoCRM недоступна")
	}
	var result struct {
		Allowed bool `json:"allowed"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil {
		return false, errors.New("Некорректный ответ проверки доступа amoCRM")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return false, errors.New("Некорректный ответ проверки доступа amoCRM")
	}
	return result.Allowed, nil
}
