package amoauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxTokenLength = 8192

var (
	ErrNotConfigured = errors.New("проверка входа через amoCRM не настроена")
	ErrInvalidToken  = errors.New("некорректный токен amoCRM")
	ErrForbidden     = errors.New("пользователь amoCRM не имеет доступа")
	ErrUnavailable   = errors.New("сервис проверки amoCRM недоступен")
)

type Identity struct {
	AccountID   int64
	AccountName string
	UserID      int64
	UserEmail   string
	UserName    string
	IsAdmin     bool
	IsOwner     bool
	ClientUUID  string
	JTI         string
	ExpiresAt   time.Time
	Issuer      string
}

type Config struct {
	Secret     string
	ClientUUID string
	Audience   string
	MaxTTL     time.Duration
	ClockSkew  time.Duration
	Now        func() time.Time
}

type Verifier struct {
	secret        string
	clientUUID    string
	audience      string
	maxTTLSeconds int64
	clockSkewSecs int64
	now           func() time.Time
}

func NewVerifier(config Config) *Verifier {
	maxTTL := config.MaxTTL
	if maxTTL < time.Minute {
		maxTTL = time.Minute
	}
	clockSkew := config.ClockSkew
	if clockSkew < 0 {
		clockSkew = 0
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &Verifier{
		secret: strings.TrimSpace(config.Secret), clientUUID: strings.TrimSpace(config.ClientUUID),
		audience:      strings.TrimRight(strings.TrimSpace(config.Audience), "/"),
		maxTTLSeconds: int64(maxTTL / time.Second), clockSkewSecs: int64(clockSkew / time.Second), now: now,
	}
}

func (v *Verifier) Verify(token string) (Identity, error) {
	if v == nil || v.secret == "" || v.clientUUID == "" || v.audience == "" {
		return Identity{}, ErrNotConfigured
	}
	if token == "" || len(token) > maxTokenLength {
		return Identity{}, ErrInvalidToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Identity{}, ErrInvalidToken
	}
	header, err := decodeJSONObject(parts[0])
	if err != nil {
		return Identity{}, ErrInvalidToken
	}
	payload, err := decodeJSONObject(parts[1])
	if err != nil {
		return Identity{}, ErrInvalidToken
	}
	signature, err := decodeBase64URL(parts[2])
	if err != nil {
		return Identity{}, ErrInvalidToken
	}
	algorithm, ok := header["alg"].(string)
	if !ok || algorithm != "HS256" {
		return Identity{}, fmt.Errorf("%w: неподдерживаемый алгоритм", ErrInvalidToken)
	}
	mac := hmac.New(sha256.New, []byte(v.secret))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(mac.Sum(nil), signature) {
		return Identity{}, fmt.Errorf("%w: подпись не прошла проверку", ErrInvalidToken)
	}

	iat, err := positiveInt(payload, "iat")
	if err != nil {
		return Identity{}, err
	}
	nbf, err := positiveInt(payload, "nbf")
	if err != nil {
		return Identity{}, err
	}
	exp, err := positiveInt(payload, "exp")
	if err != nil {
		return Identity{}, err
	}
	now := v.now().Unix()
	if iat > now+v.clockSkewSecs || nbf > now+v.clockSkewSecs || exp <= now-v.clockSkewSecs {
		return Identity{}, fmt.Errorf("%w: токен истёк или ещё не действует", ErrInvalidToken)
	}
	if exp <= iat || exp-iat > v.maxTTLSeconds {
		return Identity{}, fmt.Errorf("%w: некорректный срок действия", ErrInvalidToken)
	}

	clientUUID, err := requiredString(payload, "client_uuid", 191)
	if err != nil {
		return Identity{}, err
	}
	audience, err := requiredString(payload, "aud", 500)
	if err != nil {
		return Identity{}, err
	}
	issuer, err := requiredString(payload, "iss", 500)
	if err != nil {
		return Identity{}, err
	}
	jti, err := requiredString(payload, "jti", 191)
	if err != nil {
		return Identity{}, err
	}
	if !hmac.Equal([]byte(v.clientUUID), []byte(clientUUID)) ||
		!hmac.Equal([]byte(v.audience), []byte(strings.TrimRight(audience, "/"))) {
		return Identity{}, fmt.Errorf("%w: токен выпущен для другой интеграции", ErrInvalidToken)
	}
	parsedIssuer, err := url.ParseRequestURI(issuer)
	if err != nil || parsedIssuer.Scheme != "https" || !isAmoCRMHost(parsedIssuer.Hostname()) {
		return Identity{}, fmt.Errorf("%w: недопустимый издатель", ErrInvalidToken)
	}
	accountID, err := positiveInt(payload, "account_id")
	if err != nil {
		return Identity{}, err
	}
	userID, err := positiveInt(payload, "user_id")
	if err != nil {
		return Identity{}, err
	}
	return Identity{
		AccountID: accountID, UserID: userID, ClientUUID: clientUUID, JTI: jti,
		ExpiresAt: time.Unix(exp, 0).UTC(), Issuer: issuer,
	}, nil
}

func decodeJSONObject(value string) (map[string]any, error) {
	decoded, err := decodeBase64URL(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(decoded)))
	decoder.UseNumber()
	var result map[string]any
	if err = decoder.Decode(&result); err != nil || result == nil {
		return nil, ErrInvalidToken
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidToken
	}
	return result, nil
}

func decodeBase64URL(value string) ([]byte, error) {
	if value == "" {
		return nil, ErrInvalidToken
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' {
			return nil, ErrInvalidToken
		}
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, ErrInvalidToken
	}
	return decoded, nil
}

func positiveInt(payload map[string]any, key string) (int64, error) {
	value, ok := payload[key].(json.Number)
	if !ok {
		return 0, fmt.Errorf("%w: отсутствует поле %s", ErrInvalidToken, key)
	}
	parsed, err := strconv.ParseInt(value.String(), 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%w: некорректное поле %s", ErrInvalidToken, key)
	}
	return parsed, nil
}

func requiredString(payload map[string]any, key string, maxLength int) (string, error) {
	value, ok := payload[key].(string)
	if !ok || value == "" || len(value) > maxLength {
		return "", fmt.Errorf("%w: отсутствует поле %s", ErrInvalidToken, key)
	}
	return value, nil
}

func isAmoCRMHost(host string) bool {
	host = strings.ToLower(host)
	return host == "amocrm.ru" || host == "amocrm.com" ||
		strings.HasSuffix(host, ".amocrm.ru") || strings.HasSuffix(host, ".amocrm.com")
}
