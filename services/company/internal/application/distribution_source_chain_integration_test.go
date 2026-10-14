//go:build integration

package application

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/corebridge"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

func TestDistributionFastSourceChainSurvivesDelayedSnapshotsAndReorderedDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, ownerID := uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, company, ownerID, []accessTestUser{{ownerID, "owner", "active"}})
	links := &fakeDistributionCore{bindings: map[uuid.UUID]corebridge.Binding{}, revoked: map[uuid.UUID]bool{}}
	crm := &deliveryFake{}
	s := &Service{pool: pool, now: time.Now, distributionCore: links, deliveryCore: crm}
	b, err := s.LinkDistributionConnection(ctx, Actor{CompanyID: company, UserID: ownerID, Role: "owner"}, DistributionLinkInput{InstallationID: uuid.New(), IntegrationID: uuid.New(), IntentID: uuid.New(), AccountID: "123", WidgetToken: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	scope := corebridge.Scope{CompanyID: company, BindingID: b.BindingID, BindingRevision: 1, InstallationID: b.InstallationID, IntegrationID: b.IntegrationID, AccountID: "123"}
	q := db.New(pool)
	group, err := s.CreateDistributionGroup(ctx, Actor{CompanyID: company, UserID: ownerID, Role: "owner"}, CreateDistributionGroupInput{Name: "Source chain", MemberIDs: []uuid.UUID{ownerID}})
	if err != nil {
		t.Fatal(err)
	}
	rule, err := q.CreateDistributionRule(ctx, db.CreateDistributionRuleParams{ID: uuid.New(), CompanyID: company, BindingID: b.BindingID, BindingRevision: 1, AccountID: "123", PipelineID: "20", StatusID: "30", GroupID: group.ID, Active: true, ExecutionMode: "live"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE distribution_rules SET first_activation_at=now()-interval '2 minutes',live_started_at=now()-interval '2 minutes' WHERE id=$1`, rule.ID); err != nil {
		t.Fatal(err)
	}
	for index, test := range []struct {
		delay            time.Duration
		reverse          bool
		createdOutside   bool
		confirmedOutside bool
	}{{time.Second, false, false, false}, {5 * time.Second, false, false, false}, {12 * time.Second, false, false, false}, {time.Second, true, false, false}, {12 * time.Second, false, true, false}, {12 * time.Second, false, true, true}} {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			lead := strconv.Itoa(10 + index)
			base := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
			p, target := "20", "30"
			crm.observation = corebridge.LeadObservation{Snapshot: &corebridge.LeadSnapshot{LeadID: lead, PipelineID: p, StatusID: target, ResponsibleUserID: "1", SourceUpdatedAt: &base}}
			send := func(event DistributionEventEnvelope) {
				t.Helper()
				event.SchemaVersion, event.Scope, event.MessageID, event.CorrelationID = 1, scope, uuid.New(), uuid.New()
				event.ReceivedAt, event.EmittedAt = time.Now().UTC(), time.Now().UTC()
				event.Event.LeadID, event.Event.ObservationRevision = lead, 1
				if _, e := s.ReceiveDistributionEvent(ctx, event); e != nil {
					t.Fatal(e)
				}
				if _, e := s.ProcessDistributionDelivery(ctx); e != nil {
					t.Fatal(e)
				}
			}
			createdStatus := target
			if test.createdOutside {
				createdStatus = "40"
				if test.confirmedOutside {
					crm.observation.Snapshot.StatusID = "40"
				}
			}
			send(DistributionEventEnvelope{EventID: uuid.New(), SourceOccurredAt: &base, Event: DistributionCRMEvent{Kind: "lead.created", SourceEvidence: &DistributionSourceEvidence{PipelineID: &p, StatusID: &createdStatus}}})
			exit := stageSourceEvent(base.Add(time.Second), "30", "40")
			back := stageSourceEvent(base.Add(time.Second+test.delay), "40", "30")
			// Even the exit GET already sees the final returned stage.
			crm.observation.Snapshot.StatusID = target
			crm.observation.Snapshot.SourceUpdatedAt = back.SourceOccurredAt
			if test.createdOutside {
				send(back)
			} else if test.reverse {
				send(back)
				send(exit)
			} else {
				send(exit)
				send(back)
			}
			var sequence int64
			var state, evidence string
			var entryEvent uuid.UUID
			var occurred time.Time
			if e := pool.QueryRow(ctx, `SELECT h.last_sequence,e.state,e.evidence,e.entry_event_id,e.source_occurred_at FROM distribution_lead_heads h JOIN distribution_observed_entries e ON e.id=h.current_entry_id WHERE h.account_id='123' AND h.lead_id=$1`, lead).Scan(&sequence, &state, &evidence, &entryEvent, &occurred); e != nil {
				t.Fatal(e)
			}
			if sequence != 2 || state != "checking" || evidence != "observed_transition" || entryEvent != back.EventID || !occurred.Equal(*back.SourceOccurredAt) {
				t.Fatalf("not a genuine new return: seq=%d state=%s evidence=%s event=%s time=%s", sequence, state, evidence, entryEvent, occurred)
			}
			var cancelled int
			if e := pool.QueryRow(ctx, `SELECT count(*) FROM distribution_observed_entries WHERE account_id='123' AND lead_id=$1 AND sequence=1 AND state='cancelled'`, lead).Scan(&cancelled); e != nil || cancelled != 1 {
				t.Fatalf("old episode retained: %d %v", cancelled, e)
			}
			if n, err := q.AdmitDistributionEntries(ctx); err != nil || n != 1 {
				t.Fatalf("genuine entry admission: %d %v", n, err)
			}
			duplicate := back
			duplicate.EventID = uuid.New()
			send(duplicate)
			if n, err := q.AdmitDistributionEntries(ctx); err != nil || n != 0 {
				t.Fatalf("duplicate admission: %d %v", n, err)
			}
			var queues int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM distribution_queue WHERE company_id=$1 AND lead_id=$2`, company, lead).Scan(&queues); err != nil || queues != 1 {
				t.Fatalf("duplicate queue identity: %d %v", queues, err)
			}
			if e := pool.QueryRow(ctx, `SELECT h.last_sequence,e.state FROM distribution_lead_heads h JOIN distribution_observed_entries e ON e.id=h.current_entry_id WHERE h.account_id='123' AND h.lead_id=$1`, lead).Scan(&sequence, &state); e != nil || sequence != 2 || state != "checking" {
				t.Fatalf("duplicate paused/recreated entry: %d %s %v", sequence, state, e)
			}
		})
	}
}

func TestDistributionSameSecondBoundaryVisibleWithoutHistoricalAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	company, ownerID := uuid.New(), uuid.New()
	seedAccessCompany(t, ctx, pool, company, ownerID, []accessTestUser{{ownerID, "owner", "active"}})
	links := &fakeDistributionCore{bindings: map[uuid.UUID]corebridge.Binding{}, revoked: map[uuid.UUID]bool{}}
	s := &Service{pool: pool, now: time.Now, distributionCore: links}
	owner := Actor{CompanyID: company, UserID: ownerID, Role: "owner"}
	b, e := s.LinkDistributionConnection(ctx, owner, DistributionLinkInput{InstallationID: uuid.New(), IntegrationID: uuid.New(), IntentID: uuid.New(), AccountID: "123", WidgetToken: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	g, e := s.CreateDistributionGroup(ctx, owner, CreateDistributionGroupInput{Name: "Boundary", MemberIDs: []uuid.UUID{ownerID}})
	if e != nil {
		t.Fatal(e)
	}
	q := db.New(pool)
	r, e := q.CreateDistributionRule(ctx, db.CreateDistributionRuleParams{ID: uuid.New(), CompanyID: company, BindingID: b.BindingID, BindingRevision: 1, AccountID: "123", PipelineID: "20", StatusID: "30", GroupID: g.ID, Active: true, ExecutionMode: "live"})
	if e != nil {
		t.Fatal(e)
	}
	boundary := time.Now().UTC().Truncate(time.Second).Add(-10*time.Second + 123456*time.Microsecond)
	if _, e = pool.Exec(ctx, `UPDATE distribution_rules SET live_started_at=$2,first_activation_at=$2 WHERE id=$1`, r.ID, boundary); e != nil {
		t.Fatal(e)
	}
	for i, tc := range []struct {
		source, received, created time.Time
		admitted                  bool
		state                     string
	}{
		{boundary.Truncate(time.Second), boundary.Add(time.Second), boundary.Add(time.Second), true, "requires_configuration"},
		{boundary.Truncate(time.Second).Add(time.Second), boundary.Add(time.Second), boundary.Add(time.Second), true, "waiting"},
		// Historical observe entry, including a delayed old webhook from the
		// activation second, must not turn into live work.
		{boundary.Truncate(time.Second), boundary.Add(-time.Microsecond), boundary.Add(-time.Microsecond), false, ""},
		{boundary.Add(-time.Second), boundary.Add(time.Second), boundary.Add(time.Second), false, ""},
	} {
		lead, id := strconv.Itoa(20+i), uuid.New()
		if _, e = pool.Exec(ctx, `INSERT INTO distribution_lead_heads(account_id,lead_id,company_id,binding_id,binding_revision,current_entry_id) VALUES('123',$1,$2,$3,1,$4)`, lead, company, b.BindingID, id); e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `INSERT INTO distribution_observed_entries(id,company_id,account_id,lead_id,binding_id,binding_revision,sequence,pipeline_id,status_id,entry_event_id,evidence,state,source_received_at,source_occurred_at,created_at) VALUES($1,$2,'123',$3,$4,1,1,'20','30',$5,'created_in_stage','checking',$6,$7,$8)`, id, company, lead, b.BindingID, uuid.New(), tc.received, tc.source, tc.created); e != nil {
			t.Fatal(e)
		}
		want := int64(0)
		if tc.admitted {
			want = 1
		}
		if n, err := q.AdmitDistributionEntries(ctx); err != nil || n != want {
			t.Fatalf("case %d admitted=%d want=%d error=%v", i, n, want, err)
		}
		if tc.admitted {
			var state, reason string
			if err := pool.QueryRow(ctx, `SELECT state,reason FROM distribution_queue WHERE entry_id=$1`, id).Scan(&state, &reason); err != nil || state != tc.state {
				t.Fatal(state, reason, err)
			}
			if state == "requires_configuration" && reason != "live_boundary_uncertain" {
				t.Fatal(reason)
			}
		}
	}
}
