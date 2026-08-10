package widgetentitlement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxResponseBytes = 4 << 20

type Config struct {
	BaseURL  string
	AppName  string
	Timeout  time.Duration
	CacheTTL time.Duration
	Timezone string
	Now      func() time.Time
}

type Client struct {
	baseURL    string
	appName    string
	httpClient *http.Client
	cacheTTL   time.Duration
	location   *time.Location
	now        func() time.Time
	mu         sync.Mutex
	cache      map[string]cacheEntry
}

type cacheEntry struct {
	installed bool
	paidTo    time.Time
	expiresAt time.Time
}

type responseEnvelope struct {
	Success bool     `json:"success"`
	Data    []widget `json:"data"`
}

type widget struct {
	WidgetCode   string `json:"widget_code"`
	Status       int    `json:"status"`
	PaidTo       string `json:"paid_to"`
	WidgetPaidTo string `json:"widget_paid_to"`
}

func NewClient(config Config) (*Client, error) {
	parsedURL, err := url.ParseRequestURI(strings.TrimSpace(config.BaseURL))
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return nil, errors.New("некорректный AMOCRM_WIDGET_LIST_URL")
	}
	appName := strings.TrimSpace(config.AppName)
	if appName == "" {
		return nil, errors.New("не задан APP_NAME")
	}
	if config.Timeout <= 0 {
		return nil, errors.New("AMOCRM_WIDGET_LIST_TIMEOUT должен быть положительным")
	}
	if config.CacheTTL <= 0 {
		return nil, errors.New("AMOCRM_WIDGET_LIST_CACHE_TTL должен быть положительным")
	}
	location, err := time.LoadLocation(strings.TrimSpace(config.Timezone))
	if err != nil {
		return nil, fmt.Errorf("AMOCRM_WIDGET_LIST_TZ: %w", err)
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		baseURL: strings.TrimRight(parsedURL.String(), "/"), appName: appName,
		httpClient: &http.Client{Timeout: config.Timeout}, cacheTTL: config.CacheTTL,
		location: location, now: now, cache: make(map[string]cacheEntry),
	}, nil
}

func (c *Client) Check(ctx context.Context, accountID string) (installed, paid bool, err error) {
	accountID = strings.TrimSpace(accountID)
	if !digitsOnly(accountID) || len(accountID) > 255 {
		return false, false, errors.New("некорректный amoCRM Account ID")
	}
	now := c.now()
	if cached, ok := c.cached(accountID, now); ok {
		return cached.installed, cached.installed && cached.paidTo.After(now), nil
	}
	entry, err := c.fetch(ctx, accountID, now)
	if err != nil {
		return false, false, err
	}
	c.mu.Lock()
	c.cache[accountID] = entry
	c.mu.Unlock()
	return entry.installed, entry.installed && entry.paidTo.After(now), nil
}

func (c *Client) cached(accountID string, now time.Time) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[accountID]
	if !ok || !entry.expiresAt.After(now) {
		delete(c.cache, accountID)
		return cacheEntry{}, false
	}
	return entry, true
}

func (c *Client) fetch(ctx context.Context, accountID string, now time.Time) (cacheEntry, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/"+url.PathEscape(accountID), nil)
	if err != nil {
		return cacheEntry{}, fmt.Errorf("создать запрос списка виджетов: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return cacheEntry{}, fmt.Errorf("получить список виджетов: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return cacheEntry{}, fmt.Errorf("прочитать список виджетов: %w", err)
	}
	if len(body) > maxResponseBytes {
		return cacheEntry{}, errors.New("ответ списка виджетов превышает 4 МБ")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return cacheEntry{}, fmt.Errorf("сервис списка виджетов вернул статус %d", response.StatusCode)
	}
	var envelope responseEnvelope
	if err = json.Unmarshal(body, &envelope); err != nil {
		return cacheEntry{}, fmt.Errorf("разобрать список виджетов: %w", err)
	}
	if !envelope.Success || envelope.Data == nil {
		return cacheEntry{}, errors.New("сервис списка виджетов вернул некорректный ответ")
	}
	entry := cacheEntry{expiresAt: now.Add(c.cacheTTL)}
	for _, item := range envelope.Data {
		if item.WidgetCode != c.appName {
			continue
		}
		if item.Status != 1 {
			return entry, nil
		}
		paidToValue := strings.TrimSpace(item.WidgetPaidTo)
		if paidToValue == "" {
			paidToValue = strings.TrimSpace(item.PaidTo)
		}
		paidTo, parseErr := parsePaidTo(paidToValue, c.location)
		if parseErr != nil {
			return cacheEntry{}, fmt.Errorf("разобрать срок оплаты виджета: %w", parseErr)
		}
		entry.installed = true
		entry.paidTo = paidTo
		return entry, nil
	}
	return entry, nil
}

func parsePaidTo(value string, location *time.Location) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("пустое значение widget_paid_to")
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, value, location); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("неподдерживаемый формат %s", strconv.Quote(value))
}

func digitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
