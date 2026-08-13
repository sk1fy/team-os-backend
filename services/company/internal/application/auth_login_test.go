package application

import "testing"

func TestNormalizeLoginIdentifier(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		valid bool
	}{
		{name: "TeamOS login", input: " TM8901912 ", want: "tm8901912", valid: true},
		{name: "email is not a login", input: " Owner@Example.com ", valid: false},
		{name: "short login", input: "tm123", valid: false},
		{name: "letters in login", input: "tm123456a", valid: false},
		{name: "empty", input: " ", valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, valid := normalizeLoginIdentifier(test.input)
			if got != test.want || valid != test.valid {
				t.Fatalf("normalizeLoginIdentifier(%q) = %q, %v", test.input, got, valid)
			}
		})
	}
}
