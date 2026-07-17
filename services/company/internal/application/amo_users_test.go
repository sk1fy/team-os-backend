package application

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTryStartAmoSyncSkipsInFlightAndHonorsTTL(t *testing.T) {
	companyID := uuid.New()
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	service := &Service{
		now:           func() time.Time { return now },
		amoSyncTTL:    5 * time.Minute,
		amoSyncStates: make(map[uuid.UUID]*amoSyncState),
	}

	unlock, ok := service.tryStartAmoSync(companyID)
	if !ok {
		t.Fatal("first sync attempt must start")
	}

	result := make(chan bool, 1)
	go func() {
		secondUnlock, started := service.tryStartAmoSync(companyID)
		if started {
			secondUnlock()
		}
		result <- started
	}()
	select {
	case started := <-result:
		if started {
			t.Fatal("parallel sync attempt must be skipped")
		}
	case <-time.After(time.Second):
		unlock()
		<-result
		t.Fatal("parallel sync attempt blocked instead of returning immediately")
	}
	unlock()

	if _, started := service.tryStartAmoSync(companyID); started {
		t.Fatal("sync attempt inside TTL must be skipped")
	}
	now = now.Add(5 * time.Minute)
	unlock, ok = service.tryStartAmoSync(companyID)
	if !ok {
		t.Fatal("sync attempt after TTL must start")
	}
	unlock()
}

func TestNormalizeExternalEmployees(t *testing.T) {
	companyID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	email := " USER@Example.COM "
	avatar := " https://example.com/avatar.jpg "
	users, err := normalizeExternalEmployees(companyID, []ExternalEmployee{
		{ID: " 42 ", Name: " Иван Петров ", Email: &email, AvatarURL: &avatar, GroupID: " group_0 ", GroupName: " Продажи "},
		{ID: "43", Name: "Анна"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if users[0].Email != "user@example.com" || users[0].FirstName != "Иван" || users[0].LastName != "Петров" {
		t.Fatalf("unexpected first user: %#v", users[0])
	}
	if users[1].FirstName != "Анна" || users[1].LastName != "" || !strings.HasSuffix(users[1].Email, "@users.invalid") {
		t.Fatalf("unexpected second user: %#v", users[1])
	}
}

func TestNormalizeExternalEmployeesDoesNotAddAmoCRMToName(t *testing.T) {
	users, err := normalizeExternalEmployees(uuid.New(), []ExternalEmployee{
		{ID: "1", Name: ""},
		{ID: "2", Name: "Анна"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if users[0].FirstName != "Сотрудник" || users[0].LastName != "" {
		t.Fatalf("unexpected unnamed user: %#v", users[0])
	}
	if users[1].FirstName != "Анна" || users[1].LastName != "" {
		t.Fatalf("unexpected single-name user: %#v", users[1])
	}
	if got := preservedAmoLastName("amoCRM"); got != "" {
		t.Fatalf("legacy amoCRM surname was not removed: %q", got)
	}
	if got := preservedAmoLastName("Петрова"); got != "Петрова" {
		t.Fatalf("real surname was not preserved: %q", got)
	}
}

func TestNormalizeExternalEmployeesRejectsDuplicates(t *testing.T) {
	email := "same@example.com"
	_, err := normalizeExternalEmployees(uuid.New(), []ExternalEmployee{
		{ID: "1", Name: "Первый", Email: &email},
		{ID: "2", Name: "Второй", Email: &email},
	})
	if err == nil {
		t.Fatal("expected duplicate email error")
	}
}
