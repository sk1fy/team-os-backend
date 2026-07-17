package application

import "testing"

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{name: "normalizes", input: " User@Example.COM ", want: "user@example.com", ok: true},
		{name: "rejects single-label domain", input: "a@b"},
		{name: "rejects missing domain", input: "a@"},
		{name: "rejects spaces", input: "a b@example.com"},
		{name: "rejects empty domain label", input: "a@example..com"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeEmail(test.input)
			if test.ok {
				if err != nil || got != test.want {
					t.Fatalf("normalizeEmail(%q) = %q, %v", test.input, got, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("normalizeEmail(%q) = %q, want error", test.input, got)
			}
		})
	}
}

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		name  string
		input *string
		want  *string
		ok    bool
	}{
		{name: "international", input: stringPointer(" +7 (999) 000-00-00 "), want: stringPointer("+7 (999) 000-00-00"), ok: true},
		{name: "digits", input: stringPointer("89990000000"), want: stringPointer("89990000000"), ok: true},
		{name: "clear", input: stringPointer("   "), ok: true},
		{name: "rejects letters", input: stringPointer("abcdef")},
		{name: "rejects short number", input: stringPointer("12345")},
		{name: "rejects misplaced plus", input: stringPointer("8+9990000000")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizePhone(test.input)
			if !test.ok {
				if err == nil {
					t.Fatalf("normalizePhone(%v) = %v, want error", test.input, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.want == nil {
				if got != nil {
					t.Fatalf("normalizePhone(%v) = %v, want nil", test.input, got)
				}
				return
			}
			if got == nil || *got != *test.want {
				t.Fatalf("normalizePhone(%v) = %v, want %q", test.input, got, *test.want)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }
