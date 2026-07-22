package application

import (
	"encoding/json"
	"testing"

	eventsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/events/v1"
	domainenrollment "github.com/sk1fy/team-os-backend/services/academy/internal/domain/enrollment"
)

func TestSelfEnrollmentSourceTypeToEvent(t *testing.T) {
	t.Parallel()

	if got := enrollmentSourceTypeToEvent(domainenrollment.SourceSelfEnrollment); got != eventsv1.AcademyEnrollmentSourceType_ACADEMY_ENROLLMENT_SOURCE_TYPE_SELF_ENROLLMENT {
		t.Fatalf("enrollmentSourceTypeToEvent(self enrollment) = %s", got)
	}
}

func TestEvaluateEnrollmentQuiz(t *testing.T) {
	questions := json.RawMessage(`[
		{"id":"q1","type":"single","text":"Один","options":[{"id":"a","text":"A","correct":true},{"id":"b","text":"B","correct":false}]},
		{"id":"q2","type":"multiple","text":"Несколько","options":[{"id":"c","text":"C","correct":true},{"id":"d","text":"D","correct":true},{"id":"e","text":"E","correct":false}]},
		{"id":"q3","type":"open","text":"Открытый","options":[]}
	]`)
	tests := []struct {
		name        string
		answers     []EnrollmentQuizAnswer
		wantScore   int
		wantPending bool
		wantError   bool
	}{
		{
			name: "closed answers scored and open answer awaits review",
			answers: []EnrollmentQuizAnswer{
				{QuestionID: "q1", SelectedOptionIDs: []string{"a"}},
				{QuestionID: "q2", SelectedOptionIDs: []string{"d", "c"}},
				{QuestionID: "q3", Text: stringTestPointer("Текст")},
			},
			wantScore: 100, wantPending: true,
		},
		{
			name: "extra option makes multiple answer wrong",
			answers: []EnrollmentQuizAnswer{
				{QuestionID: "q1", SelectedOptionIDs: []string{"a"}},
				{QuestionID: "q2", SelectedOptionIDs: []string{"c", "d", "e"}},
			},
			wantScore: 50, wantPending: true,
		},
		{
			name:      "unknown option rejected",
			answers:   []EnrollmentQuizAnswer{{QuestionID: "q1", SelectedOptionIDs: []string{"unknown"}}},
			wantError: true,
		},
		{
			name:      "duplicate answer rejected",
			answers:   []EnrollmentQuizAnswer{{QuestionID: "q1"}, {QuestionID: "q1"}},
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			score, pending, err := evaluateEnrollmentQuiz(questions, test.answers)
			if test.wantError {
				if err == nil {
					t.Fatal("expected validation error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if score != test.wantScore || pending != test.wantPending {
				t.Fatalf("score=%d pending=%v, want score=%d pending=%v", score, pending, test.wantScore, test.wantPending)
			}
		})
	}
}

func stringTestPointer(value string) *string { return &value }
