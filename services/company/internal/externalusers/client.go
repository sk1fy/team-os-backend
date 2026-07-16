package externalusers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sk1fy/team-os-backend/services/company/internal/application"
)

const maxResponseBytes = 4 << 20

type Config struct {
	APIURL  string
	AppName string
	Timeout time.Duration
}

type Client struct {
	apiURL     string
	appName    string
	httpClient *http.Client
}

type apiResponse struct {
	Result  bool       `json:"result"`
	Message *string    `json:"message"`
	Data    []employee `json:"data"`
}

type employee struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Email  *string `json:"email"`
	Avatar *string `json:"avatar"`
	Group  *group  `json:"group"`
}

type group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func NewClient(config Config) (*Client, error) {
	if _, err := url.ParseRequestURI(config.APIURL); err != nil {
		return nil, fmt.Errorf("некорректный EXTERNAL_API_URL: %w", err)
	}
	if strings.TrimSpace(config.AppName) == "" {
		return nil, errors.New("не задан APP_NAME")
	}
	if config.Timeout <= 0 {
		return nil, errors.New("таймаут внешнего API должен быть положительным")
	}
	return &Client{
		apiURL:     config.APIURL,
		appName:    strings.TrimSpace(config.AppName),
		httpClient: &http.Client{Timeout: config.Timeout},
	}, nil
}

func (c *Client) FetchAll(ctx context.Context, amoAccountID string) ([]application.ExternalEmployee, error) {
	amoAccountID = strings.TrimSpace(amoAccountID)
	if amoAccountID == "" {
		return nil, errors.New("не задан amo_account_id")
	}
	body, err := json.Marshal(map[string]string{
		"amo_account_id": amoAccountID,
		"app_name":       c.appName,
	})
	if err != nil {
		return nil, fmt.Errorf("сформировать запрос сотрудников: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("создать запрос сотрудников: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("получить сотрудников amoCRM: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return nil, fmt.Errorf("внешний API вернул статус %d", response.StatusCode)
	}

	var envelope apiResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes))
	if err = decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("прочитать ответ со списком сотрудников: %w", err)
	}
	if !envelope.Result {
		message := "внешний API отклонил запрос"
		if envelope.Message != nil && strings.TrimSpace(*envelope.Message) != "" {
			message = strings.TrimSpace(*envelope.Message)
		}
		return nil, errors.New(message)
	}
	if envelope.Data == nil {
		return nil, errors.New("внешний API не вернул массив сотрудников")
	}

	result := make([]application.ExternalEmployee, len(envelope.Data))
	for index, value := range envelope.Data {
		result[index] = application.ExternalEmployee{
			ID: value.ID, Name: value.Name, Email: clone(value.Email), AvatarURL: c.avatarURL(amoAccountID, value.Avatar),
		}
		if value.Group != nil {
			result[index].GroupID = strings.TrimSpace(value.Group.ID)
			result[index].GroupName = strings.TrimSpace(value.Group.Name)
		}
	}
	return result, nil
}

func (c *Client) avatarURL(amoAccountID string, value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	raw := strings.TrimSpace(*value)
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	if parsed.IsAbs() {
		return &raw
	}
	if !strings.HasPrefix(raw, "/") {
		return nil
	}
	absolute := "https://" + amoAccountID + ".amocrm.ru" + raw
	return &absolute
}

func clone(value *string) *string {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
