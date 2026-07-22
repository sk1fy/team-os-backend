package personalaccess

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"
	"time"
)

var accessNow = time.Date(2026, time.July, 22, 13, 0, 0, 0, time.UTC)

func TestNewPersonalAccessNormalizesAndStoresOnlyHash(t *testing.T) {
	t.Parallel()

	firstName := " Иван "
	access, err := New(NewParams{
		ID: "access-1", CompanyID: "company-1", CourseID: "course-1", CourseVersionID: "version-1",
		PartnerOwnerID: "partner-1", IssuedByID: "partner-1", ExpectedEmail: " Ivan+One@Example.COM ",
		RecipientFirstName: &firstName, DeadlineDays: 3, TokenHash: accessHash("token"), TokenPrefix: "abc123",
		IssuanceIdempotencyKey: "issue-1", IssuedAt: accessNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := access.Snapshot()
	if snapshot.ExpectedEmail != "Ivan+One@Example.COM" || snapshot.NormalizedExpectedEmail != "ivan+one@example.com" || snapshot.RecipientFirstName == nil ||
		*snapshot.RecipientFirstName != "Иван" || !bytes.Equal(snapshot.TokenHash, accessHash("token")) || snapshot.Status != StatusIssued {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	*snapshot.RecipientFirstName = "Изменено"
	snapshot.TokenHash[0] ^= 0xff
	if *access.Snapshot().RecipientFirstName != "Иван" {
		t.Fatal("aggregate mutated through snapshot")
	}
	if bytes.Equal(snapshot.TokenHash, access.Snapshot().TokenHash) {
		t.Fatal("aggregate hash mutated through snapshot")
	}
}

func TestRehydratePersonalAccessRejectsBrokenInvariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Snapshot)
		want   error
	}{
		{name: "id", mutate: func(s *Snapshot) { s.ID = "" }, want: ErrAccessIDRequired},
		{name: "company", mutate: func(s *Snapshot) { s.CompanyID = "" }, want: ErrCompanyRequired},
		{name: "course", mutate: func(s *Snapshot) { s.CourseID = "" }, want: ErrCourseRequired},
		{name: "version", mutate: func(s *Snapshot) { s.CourseVersionID = "" }, want: ErrCourseVersionRequired},
		{name: "partner owner", mutate: func(s *Snapshot) { s.PartnerOwnerID = "" }, want: ErrPartnerOwnerRequired},
		{name: "issuer", mutate: func(s *Snapshot) { s.IssuedByID = "" }, want: ErrIssuerRequired},
		{name: "issuer mismatch", mutate: func(s *Snapshot) { s.IssuedByID = "partner-2" }, want: ErrIssuerNotPartnerOwner},
		{name: "email", mutate: func(s *Snapshot) { s.ExpectedEmail = "invalid" }, want: ErrInvalidExpectedEmail},
		{name: "normalized email mismatch", mutate: func(s *Snapshot) { s.NormalizedExpectedEmail = "other@example.com" }, want: ErrInvalidExpectedEmail},
		{name: "short deadline", mutate: func(s *Snapshot) { s.DeadlineDays = 0 }, want: ErrDeadlineDaysInvalid},
		{name: "long deadline", mutate: func(s *Snapshot) { s.DeadlineDays = 8 }, want: ErrDeadlineDaysInvalid},
		{name: "hash", mutate: func(s *Snapshot) { s.TokenHash = nil }, want: ErrTokenHashRequired},
		{name: "prefix", mutate: func(s *Snapshot) { s.TokenPrefix = " " }, want: ErrTokenPrefixRequired},
		{name: "idempotency", mutate: func(s *Snapshot) { s.IssuanceIdempotencyKey = "" }, want: ErrIdempotencyKeyRequired},
		{name: "issued at", mutate: func(s *Snapshot) { s.IssuedAt = time.Time{} }, want: ErrIssuedAtRequired},
		{name: "updated at", mutate: func(s *Snapshot) { s.UpdatedAt = time.Time{} }, want: ErrUpdatedAtInvalid},
		{name: "root shape", mutate: func(s *Snapshot) { s.RootAccessID = "other" }, want: ErrRepeatShapeInvalid},
		{name: "unknown status", mutate: func(s *Snapshot) { s.Status = "other" }, want: ErrUnknownStatus},
		{name: "issued has learner", mutate: func(s *Snapshot) { s.ExternalLearnerID = idPtr("learner-1"); s.EnrollmentID = idPtr("enrollment-1") }, want: ErrActivationStateInvalid},
		{name: "learner without enrollment", mutate: func(s *Snapshot) { s.ExternalLearnerID = idPtr("learner-1") }, want: ErrActivationStateInvalid},
		{name: "activated without identity", mutate: func(s *Snapshot) { s.Status = StatusActivated }, want: ErrActivationStateInvalid},
		{name: "revoked without timestamp", mutate: func(s *Snapshot) { s.Status = StatusRevoked }, want: ErrRevocationStateInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := validIssuedSnapshot()
			test.mutate(&value)
			_, err := Rehydrate(value)
			if !errors.Is(err, test.want) {
				t.Fatalf("Rehydrate() = %v, want %v", err, test.want)
			}
		})
	}
}

func TestPersonalAccessActivationIsEmailBoundAndIdempotent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params Activation
		want   error
	}{
		{name: "valid", params: validActivation()},
		{name: "other company", params: func() Activation { value := validActivation(); value.CompanyID = "company-2"; return value }(), want: ErrAccessCompanyMismatch},
		{name: "other email", params: func() Activation {
			value := validActivation()
			value.NormalizedEmail = "other@example.com"
			return value
		}(), want: ErrAccessEmailMismatch},
		{name: "learner missing", params: func() Activation { value := validActivation(); value.ExternalLearnerID = ""; return value }(), want: ErrLearnerRequired},
		{name: "enrollment missing", params: func() Activation { value := validActivation(); value.EnrollmentID = ""; return value }(), want: ErrEnrollmentRequired},
		{name: "backdated", params: func() Activation { value := validActivation(); value.At = accessNow.Add(-time.Second); return value }(), want: ErrActivationTimeInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			access, err := Rehydrate(validIssuedSnapshot())
			if err != nil {
				t.Fatal(err)
			}
			before := access.Snapshot()
			err = access.Activate(test.params)
			if !errors.Is(err, test.want) {
				t.Fatalf("Activate() = %v, want %v", err, test.want)
			}
			if test.want != nil {
				if !reflect.DeepEqual(access.Snapshot(), before) {
					t.Fatal("failed activation mutated access")
				}
				return
			}
			if access.Snapshot().Status != StatusActivated {
				t.Fatalf("status = %s", access.Snapshot().Status)
			}
			if err := access.Activate(test.params); err != nil {
				t.Fatalf("idempotent Activate() = %v", err)
			}
			other := test.params
			other.EnrollmentID = "enrollment-2"
			if err := access.Activate(other); !errors.Is(err, ErrAccessAlreadyActivated) {
				t.Fatalf("other enrollment = %v", err)
			}
		})
	}
}

func TestRotationPreservesActivationAndDeadlineMetadata(t *testing.T) {
	t.Parallel()

	access := mustActivatedAccess(t)
	before := access.Snapshot()
	if !access.MatchesTokenHash(accessHash("old")) || !access.TokenUsable() {
		t.Fatal("issued token not usable")
	}
	if err := access.RotateToken(accessHash("new"), "newpref", accessNow.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	after := access.Snapshot()
	if access.MatchesTokenHash(accessHash("old")) || !access.MatchesTokenHash(accessHash("new")) {
		t.Fatal("rotation did not invalidate old hash")
	}
	if after.CourseVersionID != before.CourseVersionID || after.EnrollmentID == nil || before.EnrollmentID == nil ||
		*after.EnrollmentID != *before.EnrollmentID || after.DeadlineDays != before.DeadlineDays ||
		!after.ActivatedAt.Equal(*before.ActivatedAt) {
		t.Fatalf("rotation changed learning state: before=%#v after=%#v", before, after)
	}
	if err := access.RotateToken(accessHash("new"), "samepref", accessNow.Add(3*time.Hour)); !errors.Is(err, ErrTokenRotationInvalid) {
		t.Fatalf("same token rotation = %v", err)
	}
}

func TestDeadlinePolicyAndRevocation(t *testing.T) {
	t.Parallel()

	for _, days := range []int{0, 8} {
		access := mustActivatedAccess(t)
		if err := access.SetDeadlineDays(days, accessNow.Add(2*time.Hour)); !errors.Is(err, ErrDeadlineDaysInvalid) {
			t.Errorf("SetDeadlineDays(%d) = %v", days, err)
		}
	}
	access := mustActivatedAccess(t)
	if err := access.SetDeadlineDays(7, accessNow.Add(2*time.Hour)); err != nil || access.Snapshot().DeadlineDays != 7 {
		t.Fatalf("valid deadline = %v, %#v", err, access.Snapshot())
	}
	if err := access.Revoke(accessNow.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if access.TokenUsable() || access.Snapshot().Status != StatusRevoked {
		t.Fatal("revoked token remains usable")
	}
	if err := access.Revoke(accessNow.Add(3 * time.Hour)); err != nil {
		t.Fatalf("idempotent revoke = %v", err)
	}
	if err := access.RotateToken(accessHash("another"), "newpref", accessNow.Add(4*time.Hour)); !errors.Is(err, ErrAccessRevoked) {
		t.Fatalf("rotate revoked = %v", err)
	}
	if err := access.SetDeadlineDays(3, accessNow.Add(4*time.Hour)); !errors.Is(err, ErrAccessRevoked) {
		t.Fatalf("extend revoked = %v", err)
	}
}

func TestRepeatCreatesIndependentAccessPinnedToVersion(t *testing.T) {
	t.Parallel()

	access := mustActivatedAccess(t)
	repeat, err := access.PlanRepeat(RepeatParams{
		ID: "access-2", TokenHash: accessHash("repeat"), TokenPrefix: "repeat01",
		IssuanceIdempotencyKey: "repeat-1", IssuedAt: accessNow.Add(2 * time.Hour), PreviousCompleted: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := repeat.Snapshot()
	if snapshot.ID != "access-2" || snapshot.CourseVersionID != "version-1" || snapshot.RootAccessID != "access-1" ||
		snapshot.RepeatOfAccessID == nil || *snapshot.RepeatOfAccessID != "access-1" || snapshot.AttemptNumber != 2 ||
		snapshot.ExternalLearnerID == nil || *snapshot.ExternalLearnerID != "learner-1" || snapshot.Status != StatusIssued {
		t.Fatalf("repeat = %#v", snapshot)
	}
	if access.Snapshot().EnrollmentID == nil || *access.Snapshot().EnrollmentID != "enrollment-1" {
		t.Fatal("repeat creation mutated current enrollment")
	}
	_, err = access.PlanRepeat(RepeatParams{
		ID: "access-3", TokenHash: accessHash("repeat-2"), TokenPrefix: "repeat02",
		IssuanceIdempotencyKey: "repeat-2", IssuedAt: accessNow.Add(2 * time.Hour), PreviousCompleted: false,
	})
	if !errors.Is(err, ErrRepeatNotAvailable) {
		t.Fatalf("repeat incomplete enrollment = %v", err)
	}
}

func TestClosedAccessIsIrreversible(t *testing.T) {
	t.Parallel()

	access := mustActivatedAccess(t)
	if err := access.Close(accessNow.Add(2 * time.Hour)); err != nil || access.Snapshot().Status != StatusClosed || access.TokenUsable() {
		t.Fatalf("Close() = %v, %#v", err, access.Snapshot())
	}
	if err := access.Close(accessNow.Add(3 * time.Hour)); err != nil {
		t.Fatalf("idempotent Close() = %v", err)
	}
	if err := access.Activate(validActivation()); !errors.Is(err, ErrAccessClosed) {
		t.Fatalf("activate closed = %v", err)
	}
	if err := access.Revoke(accessNow.Add(time.Hour)); !errors.Is(err, ErrAccessClosed) {
		t.Fatalf("revoke closed = %v", err)
	}
}

func validIssuedSnapshot() Snapshot {
	return Snapshot{
		ID: "access-1", CompanyID: "company-1", CourseID: "course-1", CourseVersionID: "version-1",
		PartnerOwnerID: "partner-1", ExpectedEmail: "ivan@example.com", NormalizedExpectedEmail: "ivan@example.com", DeadlineDays: 3,
		Status: StatusIssued, TokenHash: accessHash("old"), TokenPrefix: "oldpref",
		RootAccessID: "access-1", AttemptNumber: 1, IssuanceIdempotencyKey: "issue-1",
		IssuedByID: "partner-1", IssuedAt: accessNow, UpdatedAt: accessNow,
	}
}

func validActivation() Activation {
	return Activation{
		CompanyID: "company-1", ExternalLearnerID: "learner-1", NormalizedEmail: "IVAN@example.com",
		EnrollmentID: "enrollment-1", At: accessNow.Add(time.Hour),
	}
}

func mustActivatedAccess(t *testing.T) *Access {
	t.Helper()
	access, err := Rehydrate(validIssuedSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := access.Activate(validActivation()); err != nil {
		t.Fatal(err)
	}
	return access
}

func idPtr(value ID) *ID { return &value }

func accessHash(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return digest[:]
}
