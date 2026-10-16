package taskcontrol

import (
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const ReferenceMaxBytes = 4 << 20

// ReferenceScope identifies the installation owning this complete CRM snapshot.
type ReferenceScope struct {
	CompanyID      uuid.UUID `json:"companyId"`
	AccountID      string    `json:"accountId"`
	IntegrationID  uuid.UUID `json:"integrationId"`
	InstallationID uuid.UUID `json:"installationId"`
	Generation     int64     `json:"generation"`
	Product        string    `json:"product"`
}
type ReferenceProvenance struct {
	Source         string    `json:"source"`
	FetchedAt      time.Time `json:"fetchedAt"`
	FetchStartedAt time.Time `json:"fetchStartedAt"`
}
type ReferenceUser struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	IsActive bool    `json:"isActive"`
	GroupID  *string `json:"groupId"`
}
type ReferenceGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type ReferenceStatus struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Sort  int     `json:"sort"`
	Color *string `json:"color"`
	Kind  string  `json:"kind"`
}
type ReferencePipeline struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Sort     int               `json:"sort"`
	Statuses []ReferenceStatus `json:"statuses"`
}
type ReferenceCustomerStatus struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Sort  int     `json:"sort"`
	Color *string `json:"color"`
}
type ReferenceCustomers struct {
	Availability string                    `json:"availability"`
	Statuses     []ReferenceCustomerStatus `json:"statuses"`
	CheckedAt    time.Time                 `json:"checkedAt"`
	ErrorCode    *string                   `json:"errorCode"`
}
type ReferenceSnapshot struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Scope         ReferenceScope      `json:"scope"`
	Revision      string              `json:"revision"`
	Provenance    ReferenceProvenance `json:"provenance"`
	FreshUntil    time.Time           `json:"freshUntil"`
	Complete      bool                `json:"complete"`
	Users         []ReferenceUser     `json:"users"`
	Groups        []ReferenceGroup    `json:"groups"`
	Pipelines     []ReferencePipeline `json:"pipelines"`
	Customers     ReferenceCustomers  `json:"customers"`
}

// Validate rejects partial snapshots even when they claim complete=true. Freshness
// is deliberately separate: the last full snapshot may still be shown as stale.
func (s ReferenceSnapshot) Validate(scope ReferenceScope, now time.Time) error {
	bad := errors.New("некорректный или неполный справочник CRM")
	if s.Scope != scope || s.Scope.CompanyID == uuid.Nil || s.Scope.IntegrationID == uuid.Nil || s.Scope.InstallationID == uuid.Nil || s.Scope.Generation < 1 || !ValidCRMID(s.Scope.AccountID) || s.Scope.Product != Product || s.SchemaVersion != 1 || !s.Complete || len(s.Revision) != 71 || !strings.HasPrefix(s.Revision, "sha256:") {
		return bad
	}
	if _, e := hex.DecodeString(s.Revision[7:]); e != nil || strings.ToLower(s.Revision) != s.Revision {
		return bad
	}
	if s.Provenance.Source != "coreAmocrm" || s.Provenance.FetchStartedAt.IsZero() || s.Provenance.FetchStartedAt.After(now.Add(5*time.Second)) || s.Provenance.FetchedAt.Before(s.Provenance.FetchStartedAt) || s.Provenance.FetchedAt.After(now.Add(5*time.Second)) || !s.FreshUntil.Equal(s.Provenance.FetchStartedAt.Add(5*time.Minute)) {
		return bad
	}
	if s.Users == nil || s.Groups == nil || s.Pipelines == nil || s.Customers.Statuses == nil || len(s.Users) > 1000 || len(s.Groups) > 1000 || len(s.Pipelines) > 1000 || len(s.Customers.Statuses) > 1000 {
		return bad
	}
	groups := map[string]bool{}
	users := map[string]bool{}
	pipelines := map[string]bool{}
	statuses := map[string]bool{}
	for _, g := range s.Groups {
		if !ValidCRMID(g.ID) || groups[g.ID] {
			return bad
		}
		groups[g.ID] = true
	}
	for _, u := range s.Users {
		if !ValidCRMID(u.ID) || users[u.ID] || (u.GroupID != nil && !groups[*u.GroupID]) {
			return bad
		}
		users[u.ID] = true
	}
	for _, p := range s.Pipelines {
		if !ValidCRMID(p.ID) || pipelines[p.ID] || p.Statuses == nil || len(p.Statuses) > 1000 {
			return bad
		}
		pipelines[p.ID] = true
		for _, v := range p.Statuses {
			key := p.ID + ":" + v.ID
			if !ValidCRMID(v.ID) || statuses[key] || (v.Kind != "open" && v.Kind != "won" && v.Kind != "lost") || (v.ID == "142" && v.Kind != "won") || (v.ID == "143" && v.Kind != "lost") || (v.Kind == "won" && v.ID != "142") || (v.Kind == "lost" && v.ID != "143") {
				return bad
			}
			statuses[key] = true
		}
	}
	if s.Customers.CheckedAt.IsZero() || s.Customers.CheckedAt.After(now.Add(5*time.Second)) {
		return bad
	}
	if s.Customers.Availability != "supported" && s.Customers.Availability != "unsupported" && s.Customers.Availability != "unknown" {
		return bad
	}
	if s.Customers.Availability != "supported" && len(s.Customers.Statuses) != 0 {
		return bad
	}
	statuses = map[string]bool{}
	for _, v := range s.Customers.Statuses {
		if !ValidCRMID(v.ID) || statuses[v.ID] {
			return bad
		}
		statuses[v.ID] = true
	}
	expected, e := ReferenceRevision(s)
	if e != nil || expected != s.Revision {
		return bad
	}
	return nil
}
