package application

import (
	"testing"

	"github.com/google/uuid"
)

func TestNextMemberOrderPreservesBoundaryAfterRemoval(t *testing.T) {
	a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	old := []uuid.UUID{a, b, c}
	cases := []struct {
		current []uuid.UUID
		next    int
		want    uuid.UUID
	}{{[]uuid.UUID{a, b, c}, 1, b}, {[]uuid.UUID{b, c}, 1, b}, {[]uuid.UUID{a, c}, 1, c}, {[]uuid.UUID{a, b}, 2, a}, {[]uuid.UUID{d, a, c}, 1, c}, {[]uuid.UUID{d}, 1, d}}
	for _, tc := range cases {
		got := nextMemberOrder(tc.current, old, tc.next)
		if len(got) != len(tc.current) || got[0] != tc.want {
			t.Fatalf("got%v wantfirst%s", got, tc.want)
		}
	}
}
