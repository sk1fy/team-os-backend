// Package distributionhttp exposes only the private, signed Core callback.
package distributionhttp

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type Store interface {
	GetDistributionServiceGrant(context.Context, db.GetDistributionServiceGrantParams) (bool, error)
	ClaimDistributionNonce(context.Context, db.ClaimDistributionNonceParams) (int64, error)
}
type Service interface {
	DistributionWidgetAccess(context.Context, application.DistributionWidgetAccessInput) (application.DistributionWidgetAccess, error)
}
type Handler struct {
	Keys    map[string]string
	Store   Store
	Service Service
	Now     func() time.Time
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	fail := func(code int) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "Межсервисный доступ не подтверждён", "status": code}})
	}
	if r.Method != "POST" || r.URL.Path != "/internal/v1/distribution/widget-access" {
		fail(404)
		return
	}
	now := time.Now()
	if h.Now != nil {
		now = h.Now()
	}
	for _, k := range []string{"X-Distribution-Key-Id", "X-Distribution-Company", "X-Distribution-Installation", "X-Distribution-Timestamp", "X-Distribution-Nonce", "X-Distribution-Signature"} {
		if len(r.Header.Values(k)) != 1 {
			fail(401)
			return
		}
	}
	kid := r.Header.Get("X-Distribution-Key-Id")
	secret := h.Keys[kid]
	company, e := uuid.Parse(r.Header.Get("X-Distribution-Company"))
	if e != nil || company == uuid.Nil {
		fail(401)
		return
	}
	installation, e := uuid.Parse(r.Header.Get("X-Distribution-Installation"))
	if e != nil || installation == uuid.Nil {
		fail(401)
		return
	}
	stamp := r.Header.Get("X-Distribution-Timestamp")
	sec, e := strconv.ParseInt(stamp, 10, 64)
	if e != nil || sec <= 0 || now.Unix()-sec > 60 || sec-now.Unix() > 60 || len(secret) < 32 {
		fail(401)
		return
	}
	nonce, e := uuid.Parse(r.Header.Get("X-Distribution-Nonce"))
	if e != nil || nonce == uuid.Nil {
		fail(401)
		return
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 16384))
	if e != nil {
		fail(400)
		return
	}
	actual, e := hex.DecodeString(r.Header.Get("X-Distribution-Signature"))
	expected, _ := hex.DecodeString(corebridge.Signature(kid, secret, r.Method, r.URL.RequestURI(), company.String(), installation.String(), stamp, nonce.String(), body))
	if e != nil || !hmac.Equal(actual, expected) {
		fail(401)
		return
	}
	granted, e := h.Store.GetDistributionServiceGrant(r.Context(), db.GetDistributionServiceGrantParams{KeyID: kid, CompanyID: company, InstallationID: installation})
	if e != nil || !granted {
		fail(403)
		return
	}
	claimed, e := h.Store.ClaimDistributionNonce(r.Context(), db.ClaimDistributionNonceParams{KeyID: kid, Nonce: nonce, ExpiresAt: now.Add(5 * time.Minute)})
	if e != nil {
		fail(503)
		return
	}
	if claimed != 1 {
		fail(401)
		return
	}
	var in application.DistributionWidgetAccessInput
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if e = d.Decode(&in); e != nil {
		fail(400)
		return
	}
	var trailing any
	if e = d.Decode(&trailing); !errors.Is(e, io.EOF) {
		fail(400)
		return
	}
	if in.CompanyID != company || in.InstallationID != installation {
		fail(403)
		return
	}
	out, e := h.Service.DistributionWidgetAccess(r.Context(), in)
	if e != nil {
		fail(503)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}
