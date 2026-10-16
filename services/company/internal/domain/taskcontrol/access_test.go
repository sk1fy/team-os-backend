package taskcontrol

import "testing"

func TestAccess(t *testing.T) {
	for _, test := range []struct {
		role     string
		override *Permissions
		want     Permissions
	}{
		{"owner", nil, Permissions{true, true}}, {"admin", nil, Permissions{true, true}},
		{"employee", nil, Permissions{}}, {"employee", &Permissions{true, true}, Permissions{true, false}},
		{"admin", &Permissions{true, false}, Permissions{true, false}}, {"owner", &Permissions{false, true}, Permissions{}},
	} {
		if got := Access(test.role, test.override); got != test.want {
			t.Fatalf("%s: %+v", test.role, got)
		}
	}
}
func TestOperationAccess(t *testing.T) {
	for _, op := range []string{"draft", "publications", "references", "history", "application"} {
		if manage, known := RequiredAccess(op); manage || !known {
			t.Fatal(op)
		}
	}
	for _, op := range []string{"publish", "validate", "rollback", "imports", "mode", "sourceRequests"} {
		if manage, known := RequiredAccess(op); !manage || !known {
			t.Fatal(op)
		}
	}
	if _, known := RequiredAccess("forged"); known {
		t.Fatal("unknown operation accepted")
	}
}
