package taskcontrol

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const Product = "task-control"

var ErrAssertion = errors.New("недействительное подтверждение Core")

type Assertion struct {
	Audience       string    `json:"aud"`
	ID             string    `json:"jti"`
	IssuedAt       int64     `json:"iat"`
	ExpiresAt      int64     `json:"exp"`
	CompanyID      uuid.UUID `json:"companyId"`
	InstallationID uuid.UUID `json:"installationId"`
	IntegrationID  uuid.UUID `json:"integrationId"`
	AccountID      string    `json:"accountId"`
	ActorID        string    `json:"actorId"`
	Product        string    `json:"product"`
	Generation     int64     `json:"generation"`
	Admin          bool      `json:"admin"`
	Purpose        string    `json:"purpose"`
	RequestID      uuid.UUID `json:"requestId"`
}

func ValidCRMID(s string) bool {
	v, e := strconv.ParseInt(s, 10, 64)
	return e == nil && v > 0 && v <= 9007199254740991 && strconv.FormatInt(v, 10) == s
}
func Verify(raw string, keys map[string]ed25519.PublicKey, now time.Time) (Assertion, string, error) {
	var a Assertion
	if len(raw) > 8192 {
		return a, "", ErrAssertion
	}
	p := strings.Split(raw, ".")
	if len(p) != 3 {
		return a, "", ErrAssertion
	}
	key := keys[p[0]]
	sig, e := base64.RawURLEncoding.DecodeString(p[2])
	if e != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, []byte("teamos-task-control/v1\n"+p[0]+"."+p[1]), sig) {
		return a, "", ErrAssertion
	}
	body, e := base64.RawURLEncoding.DecodeString(p[1])
	if e != nil || json.Unmarshal(body, &a) != nil {
		return a, "", ErrAssertion
	}
	jti, e := uuid.Parse(a.ID)
	if e != nil || jti == uuid.Nil || a.CompanyID == uuid.Nil || a.InstallationID == uuid.Nil || a.IntegrationID == uuid.Nil || a.RequestID == uuid.Nil || a.Audience != "teamos-task-control" || a.Product != Product || !ValidCRMID(a.AccountID) || !ValidCRMID(a.ActorID) || a.Generation <= 0 || a.ExpiresAt <= now.Unix() || a.IssuedAt > now.Unix()+5 || a.ExpiresAt <= a.IssuedAt || a.ExpiresAt-a.IssuedAt > 60 || (a.Purpose != "runtime" && a.Purpose != "link" && a.Purpose != "revoke" && a.Purpose != "mode" && a.Purpose != "adminBridge") {
		return a, "", ErrAssertion
	}
	return a, p[0], nil
}

type Installation struct {
	CompanyID      uuid.UUID  `json:"companyId"`
	AccountID      string     `json:"accountId"`
	InstallationID *uuid.UUID `json:"installationId,omitempty"`
	IntegrationID  *uuid.UUID `json:"integrationId,omitempty"`
	Product        string     `json:"product"`
	State          string     `json:"state"`
	Revision       int64      `json:"revision,omitempty"`
	Generation     int64      `json:"generation,omitempty"`
	CheckedAt      time.Time  `json:"checkedAt"`
	FreshUntil     time.Time  `json:"freshUntil"`
}

func (s Installation) Valid(company uuid.UUID, account string, now time.Time) bool {
	if s.CompanyID != company || s.AccountID != account || s.Product != Product || (s.State != "installed" && s.State != "notInstalled" && s.State != "unknown") || s.CheckedAt.After(now.Add(5*time.Second)) || !s.FreshUntil.After(now) || s.FreshUntil.Sub(s.CheckedAt) > 5*time.Minute {
		return false
	}
	return s.State != "installed" || (s.InstallationID != nil && *s.InstallationID != uuid.Nil && s.IntegrationID != nil && *s.IntegrationID != uuid.Nil && s.Revision > 0 && s.Generation > 0)
}
