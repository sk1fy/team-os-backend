package externalsession

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

var sessionNow = time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)

func TestNewSessionContainsNoInternalIdentityOrRawToken(t *testing.T) {
	t.Parallel()

	session, err := New(NewParams{
		ID: "session-1", CompanyID: "company-1", ExternalLearnerID: "learner-1",
		TokenHash: sessionHash("token"), CreatedAt: sessionNow, ExpiresAt: sessionNow.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := session.Snapshot()
	if !bytes.Equal(snapshot.TokenHash, sessionHash("token")) || snapshot.CompanyID != "company-1" || snapshot.ExternalLearnerID != "learner-1" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if !session.MatchesTokenHash(sessionHash("token")) || session.MatchesTokenHash(sessionHash("other")) {
		t.Fatal("constant-time hash matching returned wrong result")
	}
	snapshot.TokenHash[0] ^= 0xff
	if bytes.Equal(snapshot.TokenHash, session.Snapshot().TokenHash) {
		t.Fatal("aggregate hash mutated through snapshot")
	}
}

func TestRehydrateSessionRejectsBrokenInvariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Snapshot)
		want   error
	}{
		{name: "id", mutate: func(s *Snapshot) { s.ID = "" }, want: ErrSessionIDRequired},
		{name: "company", mutate: func(s *Snapshot) { s.CompanyID = "" }, want: ErrCompanyRequired},
		{name: "learner", mutate: func(s *Snapshot) { s.ExternalLearnerID = "" }, want: ErrLearnerRequired},
		{name: "hash", mutate: func(s *Snapshot) { s.TokenHash = nil }, want: ErrTokenHashRequired},
		{name: "created", mutate: func(s *Snapshot) { s.CreatedAt = time.Time{} }, want: ErrCreatedAtRequired},
		{name: "expiry", mutate: func(s *Snapshot) { s.ExpiresAt = s.CreatedAt }, want: ErrExpiryInvalid},
		{name: "last use before creation", mutate: func(s *Snapshot) { s.LastUsedAt = timePtr(s.CreatedAt.Add(-time.Second)) }, want: ErrLastUsedInvalid},
		{name: "last use at expiry", mutate: func(s *Snapshot) { s.LastUsedAt = timePtr(s.ExpiresAt) }, want: ErrLastUsedInvalid},
		{name: "revoke before creation", mutate: func(s *Snapshot) { s.RevokedAt = timePtr(s.CreatedAt.Add(-time.Second)) }, want: ErrRevokedAtInvalid},
		{name: "reason without revoke", mutate: func(s *Snapshot) { reason := RevocationManual; s.RevocationReason = &reason }, want: ErrRevocationInvalid},
		{name: "unknown reason", mutate: func(s *Snapshot) {
			reason := RevocationReason("other")
			s.RevokedAt = timePtr(s.CreatedAt.Add(time.Hour))
			s.RevocationReason = &reason
		}, want: ErrRevocationInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := validSessionSnapshot()
			test.mutate(&value)
			_, err := Rehydrate(value)
			if !errors.Is(err, test.want) {
				t.Fatalf("Rehydrate() = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSessionAuthorizationIsTenantAndLearnerScoped(t *testing.T) {
	t.Parallel()

	session, err := Rehydrate(validSessionSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		companyID ID
		learnerID ID
		at        time.Time
		want      error
	}{
		{name: "valid", companyID: "company-1", learnerID: "learner-1", at: sessionNow.Add(time.Hour)},
		{name: "other company", companyID: "company-2", learnerID: "learner-1", at: sessionNow.Add(time.Hour), want: ErrSessionScopeMismatch},
		{name: "other learner", companyID: "company-1", learnerID: "learner-2", at: sessionNow.Add(time.Hour), want: ErrSessionScopeMismatch},
		{name: "expiry boundary", companyID: "company-1", learnerID: "learner-1", at: sessionNow.Add(24 * time.Hour), want: ErrSessionExpired},
		{name: "before creation", companyID: "company-1", learnerID: "learner-1", at: sessionNow.Add(-time.Second), want: ErrSessionTimeInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := session.Authorize(test.companyID, test.learnerID, test.at)
			if !errors.Is(err, test.want) {
				t.Fatalf("Authorize() = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSessionTouchAndRevoke(t *testing.T) {
	t.Parallel()

	session, err := Rehydrate(validSessionSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	firstUse := sessionNow.Add(time.Hour)
	if err := session.Touch(firstUse); err != nil {
		t.Fatal(err)
	}
	if err := session.Touch(firstUse.Add(-time.Second)); !errors.Is(err, ErrSessionTimeInvalid) {
		t.Fatalf("backdated Touch() = %v", err)
	}
	if !session.Snapshot().LastUsedAt.Equal(firstUse) {
		t.Fatal("failed touch changed last use")
	}
	revokedAt := firstUse.Add(time.Hour)
	if err := session.Revoke(revokedAt, RevocationManual); err != nil {
		t.Fatal(err)
	}
	if err := session.Revoke(revokedAt.Add(time.Hour), RevocationRotated); err != nil {
		t.Fatalf("idempotent Revoke() = %v", err)
	}
	if got := session.Snapshot(); got.RevocationReason == nil || *got.RevocationReason != RevocationManual {
		t.Fatalf("revoke reason = %#v", got.RevocationReason)
	}
	if err := session.Authorize("company-1", "learner-1", revokedAt.Add(time.Minute)); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("revoked Authorize() = %v", err)
	}
	if err := session.Touch(revokedAt.Add(time.Minute)); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("revoked Touch() = %v", err)
	}
}

func TestSessionExpiryBoundary(t *testing.T) {
	t.Parallel()

	session, err := Rehydrate(validSessionSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if session.Expired(sessionNow.Add(24*time.Hour - time.Nanosecond)) {
		t.Fatal("expired too early")
	}
	if !session.Expired(sessionNow.Add(24 * time.Hour)) {
		t.Fatal("not expired at boundary")
	}
	changed, err := session.Expire(sessionNow.Add(24 * time.Hour))
	if err != nil || !changed {
		t.Fatalf("Expire() = %v, %v", changed, err)
	}
	if got := session.Snapshot(); got.RevocationReason == nil || *got.RevocationReason != RevocationExpired {
		t.Fatalf("expiry reason = %#v", got)
	}
}

func validSessionSnapshot() Snapshot {
	return Snapshot{
		ID: "session-1", CompanyID: "company-1", ExternalLearnerID: "learner-1",
		TokenHash: sessionHash("token"), CreatedAt: sessionNow, ExpiresAt: sessionNow.Add(24 * time.Hour),
	}
}

func timePtr(value time.Time) *time.Time { return &value }

func sessionHash(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return digest[:]
}
