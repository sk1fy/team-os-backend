package application

import (
	"testing"
	"time"
)

func TestEqualOptionalTime(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Microsecond)
	same := now
	later := now.Add(time.Second)
	for _, test := range []struct {
		name        string
		left, right *time.Time
		want        bool
	}{
		{name: "both absent", want: true},
		{name: "same", left: &now, right: &same, want: true},
		{name: "different", left: &now, right: &later},
		{name: "one absent", left: &now},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := equalOptionalTime(test.left, test.right); got != test.want {
				t.Fatalf("equalOptionalTime() = %v, want %v", got, test.want)
			}
		})
	}
}
