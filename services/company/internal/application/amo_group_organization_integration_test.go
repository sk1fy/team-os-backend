//go:build integration

package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAmoImportCreatesDepartmentsAndAssignsEmployees(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := companyRegistrationTestPool(t, ctx)

	companyID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO companies (id, name, amo_account_id, status, onboarding_completed_at)
		VALUES ($1, 'Ракурс', '31355880', 'active', now())
	`, companyID); err != nil {
		t.Fatal(err)
	}
	firstEmail := "first@example.com"
	secondEmail := "second@example.com"
	service := &Service{
		pool: pool,
		now:  time.Now,
		externalUsers: staticExternalEmployees{
			{ID: "1", Name: "Первый Сотрудник", Email: &firstEmail, GroupID: "group_1", GroupName: "Отдел продаж"},
			{ID: "2", Name: "Второй Сотрудник", Email: &secondEmail, GroupID: "group_1", GroupName: "Отдел продаж"},
		},
	}
	actor := Actor{CompanyID: companyID, UserID: uuid.New()}
	if err := service.syncAmoUsersNow(ctx, actor); err != nil {
		t.Fatal(err)
	}

	var departments, roots, attachedToRoot, positions, assignments int
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM departments WHERE company_id=$1 AND source='amo'),
			(SELECT count(*) FROM departments WHERE company_id=$1 AND source='system' AND parent_id IS NULL),
			(SELECT count(*)
			 FROM departments AS department
			 JOIN departments AS root ON root.id=department.parent_id AND root.company_id=department.company_id
			 WHERE department.company_id=$1 AND department.source='amo' AND root.source='system'),
			(SELECT count(*) FROM positions WHERE company_id=$1),
			(SELECT count(*) FROM user_departments WHERE company_id=$1)
	`, companyID).Scan(&departments, &roots, &attachedToRoot, &positions, &assignments); err != nil {
		t.Fatal(err)
	}
	if departments != 1 || roots != 1 || attachedToRoot != 1 || positions != 0 || assignments != 2 {
		t.Fatalf(
			"departments=%d roots=%d attachedToRoot=%d positions=%d assignments=%d",
			departments, roots, attachedToRoot, positions, assignments,
		)
	}
	assertAmoEmployeeDepartment(t, ctx, service, companyID, "1", "Отдел продаж")
	assertAmoEmployeeDepartment(t, ctx, service, companyID, "2", "Отдел продаж")

	service.externalUsers = staticExternalEmployees{
		{ID: "1", Name: "Первый Сотрудник", Email: &firstEmail, GroupID: "group_2", GroupName: "Администратор"},
		{ID: "2", Name: "Второй Сотрудник", Email: &secondEmail, GroupID: "group_1", GroupName: "Продажи"},
	}
	if err := service.syncAmoUsersNow(ctx, actor); err != nil {
		t.Fatal(err)
	}
	assertAmoEmployeeDepartment(t, ctx, service, companyID, "1", "Администратор")
	assertAmoEmployeeDepartment(t, ctx, service, companyID, "2", "Продажи")
}

func assertAmoEmployeeDepartment(
	t *testing.T,
	ctx context.Context,
	service *Service,
	companyID uuid.UUID,
	externalUserID string,
	wantDepartment string,
) {
	t.Helper()
	var userID uuid.UUID
	var departmentName, departmentExternalID string
	err := service.pool.QueryRow(ctx, `
		SELECT employee.id, department.name, department.external_id
		FROM users AS employee
		JOIN user_departments AS assignment
		  ON assignment.company_id=employee.company_id AND assignment.user_id=employee.id
		JOIN departments AS department
		  ON department.company_id=assignment.company_id AND department.id=assignment.department_id
		WHERE employee.company_id=$1 AND employee.external_id=$2
	`, companyID, externalUserID).Scan(&userID, &departmentName, &departmentExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if departmentName != wantDepartment {
		t.Fatalf("employee=%s department=%q", externalUserID, departmentName)
	}
	if departmentExternalID == "" {
		t.Fatalf("employee=%s departmentExternalID=%q", externalUserID, departmentExternalID)
	}
	user, err := service.GetUser(ctx, Actor{CompanyID: companyID}, userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(user.PositionIDs) != 0 || len(user.DepartmentIDs) != 1 || user.DepartmentIDs[0] == uuid.Nil {
		t.Fatalf("employee=%s positionIDs=%v departmentIDs=%v", externalUserID, user.PositionIDs, user.DepartmentIDs)
	}
}
