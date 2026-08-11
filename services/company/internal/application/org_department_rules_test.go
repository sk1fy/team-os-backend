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
	if err == nil || err.Error() != "Название импортированного отдела нельзя изменить" {
		t.Fatalf("UpdateDepartment() error = %v", err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateAmoDepartmentAllowsHeadAndValuableFinalProduct(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)

	companyID, userID, departmentID := uuid.New(), uuid.New(), uuid.New()
	expectDepartment(mock, companyID, departmentID, "amo")
	now := time.Now()
	mock.ExpectQuery("UPDATE departments").
		WithArgs(pgxmock.AnyArg(), true, pgxmock.AnyArg(), true, pgxmock.AnyArg(), companyID, departmentID).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "company_id", "name", "parent_id", "head_user_id", "valuable_final_product",
			"order", "created_at", "updated_at", "source", "external_id",
		}).AddRow(
			departmentID, companyID, "Отдел", nil, nil, "Квалифицированные заявки",
			int32(0), now, now, "amo", pgtype.Text{},
		))
	service := &Service{pool: mock}
	vfp := "Квалифицированные заявки"
	department, err := service.UpdateDepartment(context.Background(), Actor{
		CompanyID: companyID, UserID: userID, Role: "admin",
	}, UpdateDepartmentInput{
		ID: departmentID, SetHeadUserID: true,
		SetValuableFinalProduct: true, ValuableFinalProduct: &vfp,
	})
	if err != nil {
		t.Fatalf("UpdateDepartment() error = %v", err)
	}
	if department.ValuableFinalProduct == nil || *department.ValuableFinalProduct != vfp {
		t.Fatalf("UpdateDepartment() valuableFinalProduct = %v, want %q", department.ValuableFinalProduct, vfp)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateSystemDepartmentAllowsHead(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)

	companyID, userID, departmentID := uuid.New(), uuid.New(), uuid.New()
	expectDepartment(mock, companyID, departmentID, "system")
	now := time.Now()
	mock.ExpectQuery("UPDATE departments").
		WithArgs(pgxmock.AnyArg(), true, pgxmock.AnyArg(), false, pgxmock.AnyArg(), companyID, departmentID).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "company_id", "name", "parent_id", "head_user_id", "valuable_final_product",
			"order", "created_at", "updated_at", "source", "external_id",
		}).AddRow(
			departmentID, companyID, "Компания", nil, nil, nil,
			int32(0), now, now, "system", pgtype.Text{},
		))
	service := &Service{pool: mock}
	_, err = service.UpdateDepartment(context.Background(), Actor{
		CompanyID: companyID, UserID: userID, Role: "admin",
	}, UpdateDepartmentInput{ID: departmentID, SetHeadUserID: true})
	if err != nil {
		t.Fatalf("UpdateDepartment() error = %v", err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreatePositionAllowsAmoDepartment(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)

	companyID, userID, departmentID := uuid.New(), uuid.New(), uuid.New()
	expectDepartment(mock, companyID, departmentID, "amo")
	now := time.Now()
	mock.ExpectQuery("INSERT INTO positions").
		WithArgs(pgxmock.AnyArg(), companyID, "Менеджер", departmentID, int16(2), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "company_id", "name", "department_id", "level", "description",
			"article_ids", "required_course_ids", "created_at", "updated_at",
		}).AddRow(
			uuid.New(), companyID, "Менеджер", departmentID, int16(2), nil,
			[]uuid.UUID{}, []uuid.UUID{}, now, now,
		))
	service := &Service{pool: mock}
	level := int16(2)
	position, err := service.CreatePosition(context.Background(), Actor{
		CompanyID: companyID, UserID: userID, Role: "admin",
	}, CreatePositionInput{Name: "Менеджер", DepartmentID: departmentID, Level: &level})
	if err != nil {
		t.Fatalf("CreatePosition() error = %v", err)
	}
	if position.DepartmentID != departmentID {
		t.Fatalf("CreatePosition() departmentID = %v, want %v", position.DepartmentID, departmentID)
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
