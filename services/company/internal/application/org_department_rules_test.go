package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pashagolub/pgxmock/v4"
)

func TestDeleteDepartmentRejectsProtectedDepartments(t *testing.T) {
	for _, test := range []struct {
		name, source, message string
	}{
		{name: "system root", source: "system", message: "Головной отдел нельзя удалить"},
		{name: "amo import", source: "amo", message: "Импортированный отдел нельзя удалить"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(mock.Close)

			companyID, userID, departmentID := uuid.New(), uuid.New(), uuid.New()
			expectDepartment(mock, companyID, departmentID, test.source)
			service := &Service{pool: mock}
			err = service.DeleteDepartment(context.Background(), Actor{
				CompanyID: companyID, UserID: userID, Role: "admin",
			}, departmentID)
			if err == nil || err.Error() != test.message {
				t.Fatalf("DeleteDepartment() error = %v, want %q", err, test.message)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUpdateDepartmentRejectsAmoDepartment(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)

	companyID, userID, departmentID := uuid.New(), uuid.New(), uuid.New()
	expectDepartment(mock, companyID, departmentID, "amo")
	service := &Service{pool: mock}
	name := "Новое название"
	_, err = service.UpdateDepartment(context.Background(), Actor{
		CompanyID: companyID, UserID: userID, Role: "admin",
	}, UpdateDepartmentInput{ID: departmentID, Name: &name})
	if err == nil || err.Error() != "Импортированный отдел можно только перемещать" {
		t.Fatalf("UpdateDepartment() error = %v", err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func expectDepartment(mock pgxmock.PgxPoolIface, companyID, departmentID uuid.UUID, source string) {
	now := time.Now()
	mock.ExpectQuery("SELECT id, company_id, name.+FROM departments WHERE company_id").
		WithArgs(companyID, departmentID).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "company_id", "name", "parent_id", "head_user_id", "valuable_final_product",
			"order", "created_at", "updated_at", "source", "external_id",
		}).AddRow(
			departmentID, companyID, "Отдел", nil, nil, nil, int32(0), now, now, source, pgtype.Text{},
		))
}
