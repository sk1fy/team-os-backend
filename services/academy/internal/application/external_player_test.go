package application

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestExternalMutationOperationsMatchDatabaseContract(t *testing.T) {
	if externalOperationCompleteLesson != "complete_lesson" {
		t.Fatalf("complete operation = %q", externalOperationCompleteLesson)
	}
	if externalOperationSubmitQuiz != "submit_quiz" {
		t.Fatalf("quiz operation = %q", externalOperationSubmitQuiz)
	}
}

func TestExternalMutationRequestHashIsStableAndRequestSpecific(t *testing.T) {
	enrollmentID := uuid.New()
	first := externalMutationRequestHash(struct {
		EnrollmentID uuid.UUID `json:"enrollmentId"`
		Value        string    `json:"value"`
	}{enrollmentID, "first"})
	again := externalMutationRequestHash(struct {
		EnrollmentID uuid.UUID `json:"enrollmentId"`
		Value        string    `json:"value"`
	}{enrollmentID, "first"})
	different := externalMutationRequestHash(struct {
		EnrollmentID uuid.UUID `json:"enrollmentId"`
		Value        string    `json:"value"`
	}{enrollmentID, "second"})
	if first != again || len(first) != 64 {
		t.Fatalf("нестабильный request hash: %q, затем %q", first, again)
	}
	if first == different {
		t.Fatal("разные внешние mutation requests получили одинаковый hash")
	}
}

func TestDecodeExternalQuizAttemptReplay(t *testing.T) {
	t.Parallel()
	attemptID, enrollmentID := uuid.New(), uuid.New()
	createdAt := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	attempt := ExternalQuizAttemptResult{
		ID: attemptID, Score: 100, Passed: true, CreatedAt: createdAt,
	}
	tests := []struct {
		name           string
		value          any
		wantEnrollment bool
	}{
		{
			name: "atomic envelope",
			value: ExternalQuizAttemptSubmitted{
				Attempt: attempt, Enrollment: Enrollment{ID: enrollmentID},
			},
			wantEnrollment: true,
		},
		{
			name:  "legacy flat attempt",
			value: attempt,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload, err := json.Marshal(test.value)
			if err != nil {
				t.Fatalf("marshal replay payload: %v", err)
			}
			got, err := decodeExternalQuizAttemptReplay(payload)
			if err != nil {
				t.Fatalf("decode replay payload: %v", err)
			}
			if got.Attempt.ID != attemptID || got.Attempt.Score != 100 || !got.Attempt.Passed {
				t.Fatalf("attempt = %+v", got.Attempt)
			}
			if (got.Enrollment.ID != uuid.Nil) != test.wantEnrollment {
				t.Fatalf("enrollment id = %s, want present=%v", got.Enrollment.ID, test.wantEnrollment)
			}
		})
	}
	if _, err := decodeExternalQuizAttemptReplay([]byte(`{"score":100}`)); err == nil {
		t.Fatal("payload without attempt id accepted")
	}
}
