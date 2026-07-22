package application

import "testing"

func TestNormalizeEnrollmentIdempotencyKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "trim", value: "  retry-key  ", want: "retry-key"},
		{name: "too short", value: "short", wantErr: true},
		{name: "empty", value: "   ", wantErr: true},
		{name: "too long", value: string(make([]byte, 256)), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeEnrollmentIdempotencyKey(test.value)
			if test.wantErr {
				if err == nil {
					t.Fatal("ожидалась ошибка валидации")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("ключ = %q, ожидался %q", got, test.want)
			}
		})
	}
}

func TestEnrollmentMutationRequestHashBindsRequest(t *testing.T) {
	t.Parallel()

	first := struct {
		EnrollmentID string `json:"enrollmentId"`
		LessonID     string `json:"lessonId"`
	}{EnrollmentID: "enrollment-1", LessonID: "lesson-1"}
	same := first
	different := first
	different.LessonID = "lesson-2"

	if enrollmentMutationRequestHash(first) != enrollmentMutationRequestHash(same) {
		t.Fatal("одинаковые запросы должны иметь одинаковый hash")
	}
	if enrollmentMutationRequestHash(first) == enrollmentMutationRequestHash(different) {
		t.Fatal("разные запросы не должны иметь одинаковый hash")
	}
}
