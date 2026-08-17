//go:build integration

package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEmployeeSectionsAndLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool := companyAccessTestPool(t, ctx)
	now := time.Date(2026, time.July, 28, 14, 0, 0, 0, time.UTC)
	service := &Service{pool: pool, now: func() time.Time { return now }}
	companyID := uuid.New()
	owner := Actor{
		UserID: uuid.New(), CompanyID: companyID, Role: "owner", RequestID: "request-lifecycle",
	}
	seedAccessCompany(t, ctx, pool, companyID, owner.UserID, []accessTestUser{
		{id: owner.UserID, role: "owner", status: "active"},
	})

	employee, err := service.CreateUser(ctx, owner, CreateUserInput{
		FirstName: "Иван", Email: "ivan.lifecycle@example.com", Role: "employee",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !employeeSectionsEqual(employee.SectionAccess, defaultEmployeeSections) {
		t.Fatalf("default sections = %#v", employee.SectionAccess)
	}
	if employee.ShowInSchedule {
		t.Fatal("new employee must be inactive")
	}
	colleague, err := service.CreateUser(ctx, owner, CreateUserInput{
		FirstName: "Анна", Email: "anna.lifecycle@example.com", Role: "employee",
	})
	if err != nil {
		t.Fatal(err)
	}

	sessionID := seedAccessSession(t, ctx, pool, companyID, employee.ID)
	updated, err := service.UpdateUser(ctx, owner, UpdateUserInput{
		ID: employee.ID, SetSectionAccess: true,
		SectionAccess: []string{"schedule", "distribution"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !employeeSectionsEqual(updated.SectionAccess, []string{"schedule", "distribution"}) {
		t.Fatalf("updated sections = %#v", updated.SectionAccess)
	}
	assertSessionRevoked(t, ctx, pool, sessionID)

	for _, invalid := range [][]string{nil, {"academy", "academy"}, {"unknown"}} {
		if _, err = service.UpdateUser(ctx, owner, UpdateUserInput{
			ID: employee.ID, SetSectionAccess: true, SectionAccess: invalid,
		}); !isCompanyError(err, ErrorValidation) {
			t.Fatalf("sections %#v error = %v, want validation", invalid, err)
		}
	}
	partnerRole := "partner"
	if _, err = service.UpdateUser(ctx, owner, UpdateUserInput{
		ID: employee.ID, Role: &partnerRole, SetSectionAccess: true,
		SectionAccess: []string{"academy"},
	}); !isCompanyError(err, ErrorValidation) {
		t.Fatalf("partner sections error = %v, want validation", err)
	}

	if _, err = pool.Exec(ctx, `
		INSERT INTO distribution_groups (id, company_id, name, member_ids)
		VALUES ($1, $2, 'Основная', $3)`,
		uuid.New(), companyID, []uuid.UUID{employee.ID, colleague.ID},
	); err != nil {
		t.Fatal(err)
	}
	sessionID = seedAccessSession(t, ctx, pool, companyID, employee.ID)
	deactivated := "deactivated"
	updated, err = service.UpdateUser(ctx, owner, UpdateUserInput{
		ID: employee.ID, Status: &deactivated,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != deactivated ||
		!employeeSectionsEqual(updated.SectionAccess, []string{"schedule", "distribution"}) {
		t.Fatalf("deactivated user = %#v", updated)
	}
	assertSessionRevoked(t, ctx, pool, sessionID)
	assertDistributionDisabled(t, ctx, pool, companyID, employee.ID, true)

	active := "active"
	updated, err = service.UpdateUser(ctx, owner, UpdateUserInput{
		ID: employee.ID, Status: &active,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != active ||
		!employeeSectionsEqual(updated.SectionAccess, []string{"schedule", "distribution"}) {
		t.Fatalf("restored user = %#v", updated)
	}
	assertDistributionDisabled(t, ctx, pool, companyID, employee.ID, true)

	if err = service.DeleteUser(ctx, owner, employee.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.GetUser(ctx, owner, employee.ID); !isCompanyError(err, ErrorNotFound) {
		t.Fatalf("deleted user lookup error = %v", err)
	}
	var auditRows int
	if err = pool.QueryRow(ctx, `
		SELECT count(*) FROM user_admin_audit
		WHERE company_id=$1 AND target_user_id=$2`, companyID, employee.ID,
	).Scan(&auditRows); err != nil {
		t.Fatal(err)
	}
	if auditRows < 4 {
		t.Fatalf("audit rows = %d, want at least 4", auditRows)
	}
	if err = service.DeleteUser(ctx, owner, colleague.ID); !isCompanyError(err, ErrorConflict) {
		t.Fatalf("single-member delete error = %v, want conflict", err)
	}

	amoUserID := uuid.New()
	amoEmail := "amo.lifecycle@example.com"
	if _, err = pool.Exec(ctx, `UPDATE companies SET amo_account_id='31355990' WHERE id=$1`, companyID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO users (
			id, company_id, email, first_name, last_name, role, status, source, external_id
		)
		VALUES ($1, $2, $3, 'Амо', 'Сотрудник', 'employee', 'active', 'amo', '42')`,
		amoUserID, companyID, amoEmail,
	); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO employee_section_access (company_id, user_id, section)
		VALUES ($2, $1, 'schedule'), ($2, $1, 'knowledge'), ($2, $1, 'academy')`,
		amoUserID, companyID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO credentials (company_id, user_id, password_hash)
		VALUES ($2, $1, 'hash')`,
		amoUserID, companyID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO access_links (company_id, user_id, token)
		VALUES ($2, $1, 'amo-link')`,
		amoUserID, companyID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO distribution_groups (id, company_id, name, member_ids)
		VALUES ($1, $2, 'amo', $3)`,
		uuid.New(), companyID, []uuid.UUID{colleague.ID, amoUserID},
	); err != nil {
		t.Fatal(err)
	}
	amoSessionID := seedAccessSession(t, ctx, pool, companyID, amoUserID)
	service.externalUsers = staticExternalEmployees{}
	if err = service.syncAmoUsersNow(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = service.GetUser(ctx, owner, amoUserID); !isCompanyError(err, ErrorNotFound) {
		t.Fatalf("tombstoned user lookup error = %v", err)
	}
	assertSessionRevoked(t, ctx, pool, amoSessionID)
	assertAccessRows(t, ctx, pool, companyID, amoUserID, 0, 0)
	assertDistributionDisabled(t, ctx, pool, companyID, amoUserID, true)

	service.externalUsers = staticExternalEmployees{{
		ID: "42", Name: "Имя из amoCRM", Email: &amoEmail,
	}}
	if err = service.syncAmoUsersNow(ctx, owner); err != nil {
		t.Fatal(err)
	}
	restoredAmo, err := service.GetUser(ctx, owner, amoUserID)
	if err != nil {
		t.Fatal(err)
	}
	if restoredAmo.Status != "active" ||
		!employeeSectionsEqual(restoredAmo.SectionAccess, defaultEmployeeSections) {
		t.Fatalf("restored amo user = %#v", restoredAmo)
	}
	assertAccessRows(t, ctx, pool, companyID, amoUserID, 0, 0)
	assertDistributionDisabled(t, ctx, pool, companyID, amoUserID, true)
}

func TestAmoImportAllowsSameEmployeeInDifferentCompanies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := companyAccessTestPool(t, ctx)
	now := time.Date(2026, time.August, 13, 14, 0, 0, 0, time.UTC)
	email := "shared.employee@example.com"
	service := &Service{
		pool: pool, now: func() time.Time { return now },
		externalUsers: staticExternalEmployees{{ID: "42", Name: "Общий Сотрудник", Email: &email}},
	}

	actors := make([]Actor, 0, 2)
	for index, accountID := range []string{"11111111", "22222222"} {
		companyID, ownerID := uuid.New(), uuid.New()
		actor := Actor{UserID: ownerID, CompanyID: companyID, Role: "owner"}
		seedAccessCompany(t, ctx, pool, companyID, ownerID, []accessTestUser{{
			id: ownerID, role: "owner", status: "active",
		}})
		if _, err := pool.Exec(ctx, `UPDATE companies SET amo_account_id=$2 WHERE id=$1`, companyID, accountID); err != nil {
			t.Fatal(err)
		}
		if err := service.syncAmoUsersNow(ctx, actor); err != nil {
			t.Fatalf("импорт компании %d: %v", index+1, err)
		}
		actors = append(actors, actor)
	}

	var users, companies int
	if err := pool.QueryRow(ctx, `
		SELECT count(*), count(DISTINCT company_id)
		FROM users
		WHERE email=$1 AND source='amo' AND external_id='42'
	`, email).Scan(&users, &companies); err != nil {
		t.Fatal(err)
	}
	if users != 2 || companies != 2 {
		t.Fatalf("imported users=%d companies=%d, want 2 and 2", users, companies)
	}
	for _, actor := range actors {
		companyUsers, err := service.ListUsers(ctx, actor)
		if err != nil {
			t.Fatal(err)
		}
		matches := 0
		for _, user := range companyUsers {
			if user.Email == email {
				matches++
				if user.CompanyID != actor.CompanyID {
					t.Fatalf("чужой сотрудник попал в компанию: user=%+v actor=%+v", user, actor)
				}
			}
		}
		if matches != 1 {
			t.Fatalf("company=%s shared employees=%d, want 1", actor.CompanyID, matches)
		}
	}
}

func assertDistributionDisabled(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	companyID, userID uuid.UUID,
	want bool,
) {
	t.Helper()
	var disabled bool
	if err := pool.QueryRow(ctx, `
		SELECT $2::uuid = ANY(disabled_member_ids)
		FROM distribution_groups
		WHERE company_id=$1 AND $2::uuid = ANY(member_ids)`, companyID, userID,
	).Scan(&disabled); err != nil {
		t.Fatal(err)
	}
	if disabled != want {
		t.Fatalf("distribution disabled = %v, want %v", disabled, want)
	}
}
