package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

type staticExternalEmployees []ExternalEmployee

func (employees staticExternalEmployees) FetchAll(context.Context, string) ([]ExternalEmployee, error) {
	return employees, nil
}

type failingExternalEmployees struct {
	err error
}

func (provider failingExternalEmployees) FetchAll(context.Context, string) ([]ExternalEmployee, error) {
	return nil, provider.err
}

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

func TestCollectAmoGroupsDeduplicatesAndSorts(t *testing.T) {
	groups, err := collectAmoGroups([]normalizedExternalEmployee{
		{ID: "3", GroupID: "group_2", GroupName: "Поддержка"},
		{ID: "1", GroupID: "group_1", GroupName: "Продажи"},
		{ID: "2", GroupID: "group_1", GroupName: "Продажи"},
		{ID: "4"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []normalizedAmoGroup{
		{ID: "group_1", Name: "Продажи"},
		{ID: "group_2", Name: "Поддержка"},
	}
	if len(groups) != len(want) {
		t.Fatalf("groups=%#v, want %#v", groups, want)
	}
	for index := range want {
		if groups[index] != want[index] {
			t.Fatalf("groups[%d]=%#v, want %#v", index, groups[index], want[index])
		}
	}
}

func TestCollectAmoGroupsRejectsIncompleteAndConflictingGroups(t *testing.T) {
	tests := []struct {
		name      string
		employees []normalizedExternalEmployee
	}{
		{
			name:      "missing name",
			employees: []normalizedExternalEmployee{{ID: "1", GroupID: "group_1"}},
		},
		{
			name: "conflicting names",
			employees: []normalizedExternalEmployee{
				{ID: "1", GroupID: "group_1", GroupName: "Продажи"},
				{ID: "2", GroupID: "group_1", GroupName: "Маркетинг"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := collectAmoGroups(test.employees); err == nil {
				t.Fatal("collectAmoGroups() error = nil")
			}
		})
	}
}

func TestEnsureAmoGroupDepartmentsCreatesDepartment(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)

	companyID := uuid.New()
	departmentID := uuid.New()
	now := time.Now()
	externalID := pgtype.Text{String: "group_1", Valid: true}
	mock.ExpectQuery("INSERT INTO departments").
		WithArgs(pgxmock.AnyArg(), companyID, "Отдел продаж", externalID).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "company_id", "name", "parent_id", "head_user_id", "valuable_final_product",
			"order", "created_at", "updated_at", "source", "external_id",
		}).AddRow(
			departmentID, companyID, "Отдел продаж", nil, nil, nil,
			int32(0), now, now, "amo", "group_1",
		))
	departments, err := ensureAmoGroupDepartments(
		context.Background(), db.New(mock), companyID,
		[]normalizedExternalEmployee{
			{ID: "1", GroupID: "group_1", GroupName: "Отдел продаж"},
			{ID: "2", GroupID: "group_1", GroupName: "Отдел продаж"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	createdDepartmentID := departments["group_1"]
	if createdDepartmentID == nil || *createdDepartmentID != departmentID {
		t.Fatalf("department=%#v", createdDepartmentID)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncAmoUserDepartmentAssignsDepartmentToPreviouslyImportedUser(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)

	companyID := uuid.New()
	userID := uuid.New()
	departmentID := uuid.New()
	groupID := pgtype.Text{String: "group_1", Valid: true}
	groupName := pgtype.Text{String: "Отдел продаж", Valid: true}
	mock.ExpectExec("INSERT INTO user_departments").
		WithArgs(companyID, userID, departmentID).
		WillReturnResult(pgconn.NewCommandTag("INSERT 0 1"))

	changed, err := syncAmoUserDepartment(
		context.Background(), db.New(mock), companyID,
		db.User{
			ID: userID, CompanyID: companyID, Source: "amo",
			ExternalGroupID: groupID, ExternalGroupName: groupName,
		},
		normalizedExternalEmployee{ID: "1", GroupID: groupID.String, GroupName: groupName.String},
		&departmentID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed=false, want true")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAmoUpstreamFailureDoesNotStartReconciliationTransaction(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	companyID := uuid.New()
	now := time.Now()
	mock.ExpectQuery("SELECT id, name, logo_url.+FROM companies").
		WithArgs(companyID).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "name", "logo_url", "owner_id", "created_at", "updated_at", "amo_account_id",
			"status", "onboarding_completed_at",
		}).AddRow(companyID, "Компания", nil, nil, now, now, "31355990", "active", now))

	service := &Service{
		pool: mock, externalUsers: failingExternalEmployees{err: errors.New("upstream unavailable")},
	}
	if err = service.syncAmoUsersNow(context.Background(), Actor{CompanyID: companyID}); err == nil {
		t.Fatal("syncAmoUsersNow() error = nil")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database mutation after upstream failure: %v", err)
	}
}

func TestAmoImportDoesNotChangeExistingUserStatusOrProfile(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)

	companyID := uuid.New()
	userID := uuid.New()
	actor := Actor{CompanyID: companyID, UserID: uuid.New()}
	now := time.Date(2026, time.July, 18, 15, 0, 0, 0, time.UTC)
	email := "employee@example.com"
	service := &Service{
		pool: mock,
		externalUsers: staticExternalEmployees{{
			ID: "42", Name: "Новое Имя", Email: &email,
		}},
	}

	mock.ExpectQuery("SELECT id, name, logo_url.+FROM companies").
		WithArgs(companyID).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "name", "logo_url", "owner_id", "created_at", "updated_at", "amo_account_id",
			"status", "onboarding_completed_at",
		}).AddRow(companyID, "Компания", nil, nil, now, now, "31355990", "active", now))
	mock.ExpectBeginTx(pgx.TxOptions{IsoLevel: pgx.Serializable})
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs(companyID).
		WillReturnResult(pgconn.NewCommandTag("SELECT 1"))
	userColumns := []string{
		"id", "company_id", "email", "first_name", "last_name", "phone", "avatar_url",
		"role", "status", "birth_date", "hired_at", "vacation_allowance", "created_at", "updated_at",
		"source", "external_id", "external_group_id", "external_group_name", "avatar_source",
		"external_deleted_at", "show_in_schedule",
	}
	userRow := []any{
		userID, companyID, email, "Старое", "Имя", nil, nil,
		"employee", "deactivated", nil, nil, nil, now, now,
		"amo", "42", nil, nil, nil, nil, true,
	}
	mock.ExpectQuery("SELECT id, company_id, email.+source = 'amo'").
		WithArgs(companyID).
		WillReturnRows(pgxmock.NewRows(userColumns).AddRow(userRow...))
	mock.ExpectQuery("SELECT id, company_id, email.+FROM users").
		WithArgs(companyID, pgtype.Text{String: "42", Valid: true}, email).
		WillReturnRows(pgxmock.NewRows(userColumns).AddRow(userRow...))
	mock.ExpectCommit()

	if err = service.syncAmoUsersNow(context.Background(), actor); err != nil {
		t.Fatal(err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected database mutation during import: %v", err)
	}
}
