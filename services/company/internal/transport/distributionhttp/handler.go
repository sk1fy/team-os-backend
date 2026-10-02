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
	ValidateDistributionDecision(context.Context, application.DistributionDecisionValidationInput) (application.DistributionDecisionValidation, error)
}
type DeliveryService interface {
	ReceiveDistributionEvent(context.Context, application.DistributionEventEnvelope) (application.DistributionDeliveryReceipt, error)
	ReceiveDistributionResult(context.Context, application.DistributionResultEnvelope) (application.DistributionDeliveryReceipt, error)
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
	capability := ""
	switch r.URL.Path {
	case "/internal/v1/distribution/widget-access":
		capability = "widget-access"
	case "/internal/v1/distribution/events":
		capability = "event-delivery"
	case "/internal/v1/distribution/results":
		capability = "result-delivery"
	case "/internal/v1/distribution/validate-decision":
		capability = "decision-validation"
	}
	if r.Method != "POST" || capability == "" {
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
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 262144))
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
	granted, e := h.Store.GetDistributionServiceGrant(r.Context(), db.GetDistributionServiceGrantParams{KeyID: kid, CompanyID: company, InstallationID: installation, Capability: capability})
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

	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if capability == "event-delivery" || capability == "result-delivery" {
		svc, ok := h.Service.(DeliveryService)
		if !ok {
			fail(503)
			return
		}
		var scope corebridge.Scope
		var receipt application.DistributionDeliveryReceipt
		var err error
		var event application.DistributionEventEnvelope
		var result application.DistributionResultEnvelope
		if capability == "event-delivery" {
			err = d.Decode(&event)
			scope = event.Scope
		} else {
			err = d.Decode(&result)
			scope = result.Scope
		}
		var trailing any
		if err != nil || !errors.Is(d.Decode(&trailing), io.EOF) {
			fail(400)
			return
		}
		if scope.CompanyID != company || scope.InstallationID != installation {
			fail(403)
			return
		}
		if capability == "event-delivery" {
			receipt, err = svc.ReceiveDistributionEvent(r.Context(), event)
		} else {
			receipt, err = svc.ReceiveDistributionResult(r.Context(), result)
		}
		if err != nil {
			status := 503
			var app *application.Error
			if errors.As(err, &app) {
				switch app.Kind {
				case application.ErrorValidation:
					status = 400
				case application.ErrorForbidden:
					status = 403
				case application.ErrorConflict:
					status = 409
				case application.ErrorNotFound:
					status = 404
				}
			}
			fail(status)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(receipt)
		return
	}
	if capability == "decision-validation" {
		var in application.DistributionDecisionValidationInput
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
		out, err := h.Service.ValidateDistributionDecision(r.Context(), in)
		if err != nil {
			fail(503)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	var in application.DistributionWidgetAccessInput
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
