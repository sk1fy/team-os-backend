package application

import "testing"

func TestCanReadAcademy(t *testing.T) {
	for _, role := range []string{"owner", "admin", "employee", "partner"} {
		if !canReadAcademy(Actor{Role: role}) {
			t.Fatalf("роль %q должна иметь доступ к академии", role)
		}
	}
	for _, role := range []string{"", "unknown"} {
		if canReadAcademy(Actor{Role: role}) {
			t.Fatalf("роль %q не должна иметь доступ к академии", role)
		}
	}
}

func TestAssigneeTypeToEvent(t *testing.T) {
	for _, value := range []string{"user", "position", "department", "external"} {
		if assigneeTypeToEvent(value) == 0 {
			t.Fatalf("тип назначения %q не преобразован", value)
		}
	}
	if assigneeTypeToEvent("unknown") != 0 {
		t.Fatal("неизвестный тип назначения должен оставаться unspecified")
	}
}
