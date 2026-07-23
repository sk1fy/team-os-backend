package externallearner

import (
	"errors"
	"testing"
	"time"
)

var learnerNow = time.Date(2026, time.July, 22, 10, 0, 0, 0, time.UTC)

func TestNormalizeEmailPreservesProviderSpecificParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{input: " Ivan.Petrov+Course@Example.COM ", want: "ivan.petrov+course@example.com"},
		{input: "a.b@example.com", want: "a.b@example.com"},
		{input: "User+tag@example.com", want: "user+tag@example.com"},
	}
	for _, test := range tests {
		if got := NormalizeEmail(test.input); got != test.want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestNewLearnerAndSnapshotIsolation(t *testing.T) {
	t.Parallel()

	lastName := " Петров "
	firstName := " Иван "
	phone := " +7 900 000-00-00 "
	learner, err := New(NewParams{
		ID: "learner-1", CompanyID: "company-1", Email: " Ivan+Course@Example.com ",
		FirstName: &firstName, LastName: &lastName, Phone: &phone,
		CreatedAt: learnerNow.In(time.FixedZone("MSK", 3*60*60)),
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := learner.Snapshot()
	if snapshot.Email != "Ivan+Course@Example.com" || snapshot.NormalizedEmail != "ivan+course@example.com" ||
		snapshot.FirstName == nil || *snapshot.FirstName != "Иван" || snapshot.LastName == nil || *snapshot.LastName != "Петров" ||
		snapshot.CreatedAt.Location() != time.UTC {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	*snapshot.LastName = "Изменено"
	if got := learner.Snapshot(); got.LastName == nil || *got.LastName != "Петров" {
		t.Fatal("aggregate mutated through snapshot")
	}
}

func TestLearnerNameIsOptional(t *testing.T) {
	t.Parallel()

	learner, err := New(NewParams{
		ID: "learner-without-name", CompanyID: "company-1",
		Email: "anonymous@example.com", CreatedAt: learnerNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if learner.Snapshot().FirstName != nil || learner.Snapshot().LastName != nil {
		t.Fatalf("optional names = %#v/%#v", learner.Snapshot().FirstName, learner.Snapshot().LastName)
	}
}

func TestRehydrateLearnerRejectsBrokenInvariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Snapshot)
		want   error
	}{
		{name: "id", mutate: func(s *Snapshot) { s.ID = "" }, want: ErrLearnerIDRequired},
		{name: "company", mutate: func(s *Snapshot) { s.CompanyID = "" }, want: ErrCompanyRequired},
		{name: "email", mutate: func(s *Snapshot) { s.Email = "invalid" }, want: ErrInvalidEmail},
		{name: "normalized email", mutate: func(s *Snapshot) { s.NormalizedEmail = "other@example.com" }, want: ErrInvalidEmail},
		{name: "created", mutate: func(s *Snapshot) { s.CreatedAt = time.Time{} }, want: ErrCreatedAtRequired},
		{name: "updated", mutate: func(s *Snapshot) { s.UpdatedAt = time.Time{} }, want: ErrUpdatedAtRequired},
		{name: "updated before created", mutate: func(s *Snapshot) { s.UpdatedAt = s.CreatedAt.Add(-time.Second) }, want: ErrInvalidTimeline},
		{name: "verification before created", mutate: func(s *Snapshot) { s.EmailVerifiedAt = timePtr(s.CreatedAt.Add(-time.Second)) }, want: ErrVerificationBeforeCreate},
		{name: "deletion before created", mutate: func(s *Snapshot) { s.DeletedAt = timePtr(s.CreatedAt.Add(-time.Second)) }, want: ErrDeletionBeforeCreate},
		{name: "deletion differs from update", mutate: func(s *Snapshot) { s.DeletedAt = timePtr(s.CreatedAt.Add(time.Hour)) }, want: ErrInvalidTimeline},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := validLearnerSnapshot()
			test.mutate(&value)
			_, err := Rehydrate(value)
			if !errors.Is(err, test.want) {
				t.Fatalf("Rehydrate() = %v, want %v", err, test.want)
			}
		})
	}
}

func TestVerificationCorrectionAndTenantIdentity(t *testing.T) {
	t.Parallel()

	learner, err := Rehydrate(validLearnerSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := learner.MatchesIdentity("company-2", "ivan@example.com"); !errors.Is(err, ErrCompanyMismatch) {
		t.Fatalf("cross tenant = %v", err)
	}
	if err := learner.MatchesIdentity("company-1", "other@example.com"); !errors.Is(err, ErrEmailMismatch) {
		t.Fatalf("other email = %v", err)
	}
	if err := learner.VerifyEmail(learnerNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	verifiedAt := *learner.Snapshot().EmailVerifiedAt
	if err := learner.VerifyEmail(learnerNow.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !learner.Snapshot().EmailVerifiedAt.Equal(verifiedAt) {
		t.Fatal("idempotent verification changed timestamp")
	}

	changed, err := learner.CorrectContact(ContactCorrection{
		Email: "IVAN@example.com", At: learnerNow.Add(3 * time.Minute),
	})
	if err != nil || changed || learner.Snapshot().EmailVerifiedAt == nil {
		t.Fatalf("case-only correction = %v, %v", changed, err)
	}
	changed, err = learner.CorrectContact(ContactCorrection{
		Email: "ivan+new@example.com", At: learnerNow.Add(4 * time.Minute),
	})
	if err != nil || !changed || learner.Snapshot().EmailVerifiedAt != nil {
		t.Fatalf("identity correction = %v, %v, %#v", changed, err, learner.Snapshot())
	}
}

func TestLearnerTransitionsAreMonotonicAndDeleteIsIrreversible(t *testing.T) {
	t.Parallel()

	learner, err := Rehydrate(validLearnerSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	before := learner.Snapshot()
	if err := learner.VerifyEmail(learnerNow.Add(-time.Second)); !errors.Is(err, ErrTransitionTimeInvalid) {
		t.Fatalf("backdated verify = %v", err)
	}
	if learner.Snapshot().EmailVerifiedAt != before.EmailVerifiedAt {
		t.Fatal("failed verification mutated learner")
	}
	deletedAt := learnerNow.Add(time.Hour)
	if err := learner.Delete(deletedAt); err != nil {
		t.Fatal(err)
	}
	if err := learner.Delete(deletedAt.Add(time.Hour)); err != nil {
		t.Fatalf("idempotent delete = %v", err)
	}
	if _, err := learner.CorrectContact(ContactCorrection{Email: "new@example.com", At: deletedAt.Add(time.Hour)}); !errors.Is(err, ErrLearnerDeleted) {
		t.Fatalf("correct deleted = %v", err)
	}
	if err := learner.VerifyEmail(deletedAt.Add(time.Hour)); !errors.Is(err, ErrLearnerDeleted) {
		t.Fatalf("verify deleted = %v", err)
	}
}

func validLearnerSnapshot() Snapshot {
	return Snapshot{
		ID: "learner-1", CompanyID: "company-1", Email: "ivan@example.com",
		NormalizedEmail: "ivan@example.com",
		CreatedAt:       learnerNow, UpdatedAt: learnerNow,
	}
}

func timePtr(value time.Time) *time.Time { return &value }
