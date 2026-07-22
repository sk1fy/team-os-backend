package application

import (
	"testing"

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
