package externalverification

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

var challengeNow = time.Date(2026, time.July, 22, 11, 0, 0, 0, time.UTC)

func TestSixDigitCodePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code string
		want bool
	}{
		{code: "000000", want: true},
		{code: "123456", want: true},
		{code: "12345"},
		{code: "1234567"},
		{code: "12345a"},
		{code: "１２３４５６"},
	}
	for _, test := range tests {
		if got := ValidSixDigitCode(test.code); got != test.want {
			t.Errorf("ValidSixDigitCode(%q) = %v, want %v", test.code, got, test.want)
		}
	}
}

func TestNewChallengeUsesFixedPolicyAndHashesOnly(t *testing.T) {
	t.Parallel()

	challenge, err := New(NewParams{
		ID: "challenge-1", CompanyID: "company-1", NormalizedEmail: " Ivan@Example.com ",
		Purpose: PurposePersonalAccess, SourceID: idPtr("access-1"), CodeHash: testHash("derived"), CreatedAt: challengeNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := challenge.Snapshot()
	if snapshot.NormalizedEmail != "ivan@example.com" || snapshot.MaxAttempts != 5 ||
		!snapshot.ExpiresAt.Equal(challengeNow.Add(10*time.Minute)) || !bytes.Equal(snapshot.CodeHash, testHash("derived")) {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	snapshot.CodeHash[0] ^= 0xff
	if bytes.Equal(snapshot.CodeHash, challenge.Snapshot().CodeHash) {
		t.Fatal("aggregate hash mutated through snapshot")
	}
}

func TestRehydrateChallengeRejectsBrokenInvariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Snapshot)
		want   error
	}{
		{name: "id", mutate: func(s *Snapshot) { s.ID = "" }, want: ErrChallengeIDRequired},
		{name: "company", mutate: func(s *Snapshot) { s.CompanyID = "" }, want: ErrCompanyRequired},
		{name: "normalized email", mutate: func(s *Snapshot) { s.NormalizedEmail = "UPPER@example.com" }, want: ErrEmailRequired},
		{name: "purpose", mutate: func(s *Snapshot) { s.Purpose = "other" }, want: ErrUnknownPurpose},
		{name: "personal source missing", mutate: func(s *Snapshot) { s.SourceID = nil }, want: ErrSourceShapeInvalid},
		{name: "bootstrap source forbidden", mutate: func(s *Snapshot) { s.Purpose = PurposeSessionBootstrap }, want: ErrSourceShapeInvalid},
		{name: "hash", mutate: func(s *Snapshot) { s.CodeHash = nil }, want: ErrCodeHashRequired},
		{name: "ip hash", mutate: func(s *Snapshot) { s.RequestIPHash = []byte{1} }, want: ErrRequestIPHashInvalid},
		{name: "created", mutate: func(s *Snapshot) { s.CreatedAt = time.Time{} }, want: ErrCreatedAtRequired},
		{name: "expired before creation", mutate: func(s *Snapshot) { s.ExpiresAt = s.CreatedAt }, want: ErrExpiryInvalid},
		{name: "ttl too long", mutate: func(s *Snapshot) { s.ExpiresAt = s.CreatedAt.Add(ChallengeTTL + time.Second) }, want: ErrExpiryInvalid},
		{name: "too many allowed attempts", mutate: func(s *Snapshot) { s.MaxAttempts = 6 }, want: ErrMaxAttemptsInvalid},
		{name: "negative attempts", mutate: func(s *Snapshot) { s.Attempts = -1 }, want: ErrAttemptsInvalid},
		{name: "attempts above maximum", mutate: func(s *Snapshot) { s.Attempts = 6 }, want: ErrAttemptsInvalid},
		{name: "consumed at expiry", mutate: func(s *Snapshot) { s.ConsumedAt = timePtr(s.ExpiresAt) }, want: ErrConsumedAtInvalid},
		{name: "invalidation pair", mutate: func(s *Snapshot) { s.InvalidatedAt = timePtr(s.CreatedAt.Add(time.Minute)) }, want: ErrInvalidationInvalid},
		{name: "consumed and invalidated", mutate: func(s *Snapshot) {
			s.ConsumedAt = timePtr(s.CreatedAt.Add(time.Minute))
			reason := InvalidationReplaced
			s.InvalidatedAt = timePtr(s.CreatedAt.Add(time.Minute))
			s.InvalidationReason = &reason
		}, want: ErrInvalidationInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := validChallengeSnapshot()
			test.mutate(&value)
			_, err := Rehydrate(value)
			if !errors.Is(err, test.want) {
				t.Fatalf("Rehydrate() = %v, want %v", err, test.want)
			}
		})
	}
}

func TestConfirmChallengeAttemptsExpiryAndConsumption(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		at           time.Time
		candidate    string
		initial      func(*Snapshot)
		want         error
		wantAttempts int
		wantConsumed bool
	}{
		{name: "success", at: challengeNow.Add(time.Minute), candidate: "correct", wantConsumed: true},
		{name: "wrong increments", at: challengeNow.Add(time.Minute), candidate: "wrong", want: ErrCodeMismatch, wantAttempts: 1},
		{name: "fifth wrong locks", at: challengeNow.Add(time.Minute), candidate: "wrong", initial: func(s *Snapshot) { s.Attempts = 4 }, want: ErrChallengeAttemptsUsed, wantAttempts: 5},
		{name: "already locked", at: challengeNow.Add(time.Minute), candidate: "correct", initial: func(s *Snapshot) { s.Attempts = 5 }, want: ErrChallengeAttemptsUsed, wantAttempts: 5},
		{name: "expiry boundary", at: challengeNow.Add(ChallengeTTL), candidate: "correct", want: ErrChallengeExpired},
		{name: "before creation", at: challengeNow.Add(-time.Second), candidate: "correct", want: ErrConfirmationTime},
		{name: "consumed", at: challengeNow.Add(2 * time.Minute), candidate: "correct", initial: func(s *Snapshot) { s.ConsumedAt = timePtr(challengeNow.Add(time.Minute)) }, want: ErrChallengeConsumed, wantConsumed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := validChallengeSnapshot()
			if test.initial != nil {
				test.initial(&value)
			}
			challenge, err := Rehydrate(value)
			if err != nil {
				t.Fatal(err)
			}
			err = challenge.Confirm(testHash(test.candidate), test.at)
			if !errors.Is(err, test.want) {
				t.Fatalf("Confirm() = %v, want %v", err, test.want)
			}
			snapshot := challenge.Snapshot()
			if snapshot.Attempts != test.wantAttempts || (snapshot.ConsumedAt != nil) != test.wantConsumed {
				t.Fatalf("state = %#v", snapshot)
			}
		})
	}
}

func TestChallengeAvailableAtBoundaries(t *testing.T) {
	t.Parallel()

	challenge, err := Rehydrate(validChallengeSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !challenge.Available(challengeNow.Add(ChallengeTTL - time.Nanosecond)) {
		t.Fatal("challenge unavailable before expiry")
	}
	if challenge.Available(challengeNow.Add(ChallengeTTL)) {
		t.Fatal("challenge available at expiry boundary")
	}
}

func TestChallengeInvalidationIsIrreversible(t *testing.T) {
	t.Parallel()

	challenge, err := Rehydrate(validChallengeSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	at := challengeNow.Add(time.Minute)
	if err := challenge.Invalidate(at, InvalidationReplaced); err != nil {
		t.Fatal(err)
	}
	if challenge.Available(at) {
		t.Fatal("invalidated challenge remains available")
	}
	if err := challenge.Confirm(testHash("correct"), at); !errors.Is(err, ErrChallengeInvalidated) {
		t.Fatalf("Confirm(invalidated) = %v", err)
	}
	if err := challenge.Invalidate(at.Add(time.Minute), InvalidationExpired); err != nil {
		t.Fatalf("idempotent Invalidate() = %v", err)
	}
	if got := challenge.Snapshot(); got.InvalidationReason == nil || *got.InvalidationReason != InvalidationReplaced || !got.InvalidatedAt.Equal(at) {
		t.Fatalf("invalidation state = %#v", got)
	}
}

func validChallengeSnapshot() Snapshot {
	return Snapshot{
		ID: "challenge-1", CompanyID: "company-1", NormalizedEmail: "ivan@example.com",
		Purpose: PurposePersonalAccess, SourceID: idPtr("access-1"), CodeHash: testHash("correct"), CreatedAt: challengeNow,
		ExpiresAt: challengeNow.Add(ChallengeTTL), MaxAttempts: DefaultMaxAttempts,
	}
}

func timePtr(value time.Time) *time.Time { return &value }
func idPtr(value ID) *ID                 { return &value }

func testHash(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return digest[:]
}
