package amoverifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sk1fy/team-os-backend/services/company/internal/domain/amoauth"
)

const (
	maxTokenLength    = 8192
	maxResponseLength = 64 << 10
)

type Config struct {
	URL          string
	ServiceToken string
	AppName      string
	Timeout      time.Duration
	HTTPClient   *http.Client
	Now          func() time.Time
}

type Client struct {
	url          string
	serviceToken string
	appName      string
	httpClient   *http.Client
	now          func() time.Time
}

func NewClient(config Config) (*Client, error) {
	endpoint := strings.TrimSpace(config.URL)
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("AMOCRM_VERIFY_URL: ожидается абсолютный HTTP(S) URL")
	}
	appName := strings.TrimSpace(config.AppName)
	if appName == "" || len(appName) > 255 {
		return nil, errors.New("не задан корректный APP_NAME")
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		url: endpoint, serviceToken: strings.TrimSpace(config.ServiceToken),
		appName: appName, httpClient: httpClient, now: now,
	}, nil
}

func (c *Client) Verify(ctx context.Context, token string) (amoauth.Identity, error) {
	if c == nil || c.url == "" || c.serviceToken == "" || c.appName == "" || c.httpClient == nil {
		return amoauth.Identity{}, amoauth.ErrNotConfigured
	}
	token = strings.TrimSpace(token)
	if len(token) < 32 || len(token) > maxTokenLength {
		return amoauth.Identity{}, amoauth.ErrInvalidToken
	}
	body, err := json.Marshal(struct {
		AppName string `json:"appName"`
	}{AppName: c.appName})
	if err != nil {
		return amoauth.Identity{}, fmt.Errorf("%w: подготовить запрос", amoauth.ErrUnavailable)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return amoauth.Identity{}, fmt.Errorf("%w: подготовить запрос", amoauth.ErrUnavailable)
	}
	request.Header.Set("Authorization", "Service "+c.serviceToken)
	request.Header.Set("X-Auth-Token", token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return amoauth.Identity{}, fmt.Errorf("%w: %v", amoauth.ErrUnavailable, err)
	}
	defer func() { _ = response.Body.Close() }()
	switch response.StatusCode {
	case http.StatusOK:
		return c.decodeVerified(response.Body)
	case http.StatusUnauthorized:
		return amoauth.Identity{}, amoauth.ErrInvalidToken
	case http.StatusForbidden:
		return amoauth.Identity{}, amoauth.ErrForbidden
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseLength))
		return amoauth.Identity{}, fmt.Errorf("%w: HTTP %d", amoauth.ErrUnavailable, response.StatusCode)
	}
}

func (c *Client) decodeVerified(body io.Reader) (amoauth.Identity, error) {
	encoded, err := io.ReadAll(io.LimitReader(body, maxResponseLength+1))
	if err != nil || len(encoded) > maxResponseLength {
		return amoauth.Identity{}, malformedResponse(errors.New("ответ verifier слишком большой"))
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var payload struct {
		Account struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"account"`
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"user"`
		Rights struct {
			IsAdmin bool `json:"isAdmin"`
			IsOwner bool `json:"isOwner"`
		} `json:"rights"`
		Token struct {
			JTI       string `json:"jti"`
			ExpiresAt string `json:"expiresAt"`
		} `json:"token"`
	}
	if err = decoder.Decode(&payload); err != nil {
		return amoauth.Identity{}, malformedResponse(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return amoauth.Identity{}, malformedResponse(errors.New("лишние данные после JSON"))
	}
	accountID, accountErr := parsePositiveID(payload.Account.ID)
	userID, userErr := parsePositiveID(payload.User.ID)
	expiresAt, expiresErr := time.Parse(time.RFC3339, strings.TrimSpace(payload.Token.ExpiresAt))
	accountName := strings.TrimSpace(payload.Account.Name)
	userEmail := strings.ToLower(strings.TrimSpace(payload.User.Email))
	userName := strings.TrimSpace(payload.User.Name)
	jti := strings.TrimSpace(payload.Token.JTI)
	if accountErr != nil || userErr != nil || expiresErr != nil || !expiresAt.After(c.now().UTC()) ||
		accountName == "" || len([]rune(accountName)) > 255 ||
		userName == "" || len([]rune(userName)) > 255 ||
		userEmail == "" || len(userEmail) > 320 || !strings.Contains(userEmail, "@") ||
		jti == "" || len(jti) > 191 {
		return amoauth.Identity{}, malformedResponse(errors.New("некорректные проверенные данные"))
	}
	if !payload.Rights.IsAdmin {
		return amoauth.Identity{}, amoauth.ErrForbidden
	}
	return amoauth.Identity{
		AccountID: accountID, AccountName: accountName,
		UserID: userID, UserEmail: userEmail, UserName: userName,
		IsAdmin: payload.Rights.IsAdmin, IsOwner: payload.Rights.IsOwner,
		JTI: jti, ExpiresAt: expiresAt.UTC(),
	}, nil
}

func parsePositiveID(value string) (int64, error) {
	value = strings.TrimSpace(value)
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return 0, errors.New("некорректный ID")
	}
	return parsed, nil
}

func malformedResponse(err error) error {
	return fmt.Errorf("%w: некорректный ответ verifier: %v", amoauth.ErrUnavailable, err)
}
