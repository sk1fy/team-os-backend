package application

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func normalizedDistributionPoint(source, pipeline, status string) (string, string) {
	switch source {
	case "creation":
		return pipeline, ""
	case "digital_pipeline":
		return "", ""
	}
	return pipeline, status
}
func validAccountTimezone(timezone string) bool {
	_, e := time.LoadLocation(timezone)
	return e == nil && timezone != "" && timezone != "Local"
}
func (s *Service) cacheDistributionTimezone(ctx context.Context, scope corebridge.Scope, r corebridge.References) error {
	if !validAccountTimezone(r.Timezone) || r.TimezoneFetchedAt.IsZero() || r.TimezoneFetchedAt.After(s.now().Add(time.Minute)) {
		return nil
	}
	q := db.New(s.pool)
	b, e := q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: scope.CompanyID, ID: scope.BindingID})
	if e != nil {
		return e
	}
	if b.State != "active" || bindingScope(b) != scope {
		return conflict("Подключение изменилось")
	}
	if e = q.SaveDistributionBindingTimezone(ctx, db.SaveDistributionBindingTimezoneParams{AccountDomain: distributionAccountDomain(r.AccountDomain), CompanyID: scope.CompanyID, ID: scope.BindingID, AccountTimezone: pgtype.Text{String: r.Timezone, Valid: true}, TimezoneFetchedAt: pgtype.Timestamptz{Time: r.TimezoneFetchedAt, Valid: true}, Revision: scope.BindingRevision}); e != nil {
		return e
	}
	// Company settings remain the compatibility view, while scheduling uses binding cache.
	current, e := q.GetDistributionSettings(ctx, scope.CompanyID)
	if e != nil && !isNoRows(e) {
		return e
	}
	if current.Timezone != r.Timezone && (!b.TimezoneFetchedAt.Valid || !r.TimezoneFetchedAt.Before(b.TimezoneFetchedAt.Time)) {
		_, e = q.SaveDistributionSettings(ctx, db.SaveDistributionSettingsParams{CompanyID: scope.CompanyID, Timezone: r.Timezone})
	}
	return e
}
func (s *Service) distributionBindingSettings(ctx context.Context, q *db.Queries, b db.DistributionBinding) (db.DistributionSetting, error) {
	if !b.AccountTimezone.Valid || !b.TimezoneFetchedAt.Valid || !validAccountTimezone(b.AccountTimezone.String) {
		return db.DistributionSetting{}, validation("Не удалось получить часовой пояс аккаунта amoCRM")
	}
	return db.DistributionSetting{CompanyID: b.CompanyID, Timezone: b.AccountTimezone.String}, nil
}
func (s *Service) distributionAccountSettings(ctx context.Context, actor Actor) (json.RawMessage, error) {
	q := db.New(s.pool)
	bindings, e := q.ListDistributionBindings(ctx, actor.CompanyID)
	if e != nil {
		return nil, e
	}
	for _, b := range bindings {
		if b.State != "active" {
			continue
		}
		status := "confirmed"
		if !b.TimezoneFetchedAt.Valid || s.now().Sub(b.TimezoneFetchedAt.Time) > 5*time.Minute {
			refreshCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			refs, re := s.readDistributionReferences(refreshCtx, bindingScope(b))
			cancel()
			if re != nil || !validAccountTimezone(refs.Timezone) || refs.TimezoneFetchedAt.IsZero() || refs.TimezoneFetchedAt.After(s.now().Add(time.Minute)) || s.now().Sub(refs.TimezoneFetchedAt) > 5*time.Minute {
				status = "cached"
			}
		}
		b, e = q.GetDistributionBinding(ctx, db.GetDistributionBindingParams{CompanyID: actor.CompanyID, ID: b.ID})
		if e != nil {
			return nil, e
		}
		if b.AccountTimezone.Valid && b.TimezoneFetchedAt.Valid && validAccountTimezone(b.AccountTimezone.String) {
			return json.Marshal(map[string]any{"timezone": b.AccountTimezone.String, "revision": b.Revision, "timezoneSource": "amocrm", "timezoneStatus": status, "timezoneFetchedAt": b.TimezoneFetchedAt.Time})
		}
	}
	v, e := q.GetDistributionSettings(ctx, actor.CompanyID)
	if e != nil && !isNoRows(e) {
		return nil, e
	}
	return json.Marshal(map[string]any{"timezone": v.Timezone, "revision": int64(1), "timezoneSource": "legacy", "timezoneStatus": "unavailable"})
}

func distributionAccountDomain(domain string) pgtype.Text {
	domain = strings.ToLower(domain)
	if !distributionCRMHost.MatchString(domain) {
		return pgtype.Text{}
	}
	return pgtype.Text{String: domain, Valid: true}
}
