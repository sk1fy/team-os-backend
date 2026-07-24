package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	pgxmock "github.com/pashagolub/pgxmock/v4"
)

func TestResolveReportUserScopeAndPageProfiles(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("create pgx mock: %v", err)
	}
	t.Cleanup(mock.Close)
	service := &Service{pool: mock}
	companyID, actorID := uuid.New(), uuid.New()
	firstUserID, secondUserID := uuid.New(), uuid.New()
	positionID, departmentID := uuid.New(), uuid.New()
	actor := Actor{CompanyID: companyID, UserID: actorID, Role: "owner"}
	search := "иван"

	mock.ExpectQuery("(?s)SELECT u.id,.*FROM users AS u.*ORDER BY u.id").
		WithArgs(
			pgtype.Text{String: search, Valid: true}, companyID,
			uuid.NullUUID{UUID: positionID, Valid: true},
			uuid.NullUUID{UUID: departmentID, Valid: true},
		).
		WillReturnRows(pgxmock.NewRows([]string{"id", "matches_search"}).
			AddRow(firstUserID, true).
			AddRow(secondUserID, false))
	scope, err := service.ResolveReportUserScope(context.Background(), actor, ResolveReportUserScopeInput{
		Search: &search, PositionID: &positionID, DepartmentID: &departmentID,
	})
	if err != nil {
		t.Fatalf("resolve report scope: %v", err)
	}
	if len(scope.UserIDs) != 2 || len(scope.SearchUserIDs) != 1 ||
		scope.SearchUserIDs[0] != firstUserID {
		t.Fatalf("report scope = %+v", scope)
	}

	mock.ExpectQuery("(?s)SELECT u.id AS user_id,.*FROM users AS u.*ORDER BY array_position").
		WithArgs(
			uuid.NullUUID{UUID: positionID, Valid: true},
			uuid.NullUUID{UUID: departmentID, Valid: true},
			companyID, []uuid.UUID{firstUserID},
		).
		WillReturnRows(pgxmock.NewRows([]string{
			"user_id", "email", "first_name", "last_name",
			"position_name", "department_name",
		}).AddRow(
			firstUserID, "ivan@example.test", "Иван", "Иванов",
			"Разработчик", "ИТ",
		))
	profiles, err := service.GetReportUserProfiles(context.Background(), actor, GetReportUserProfilesInput{
		UserIDs: []uuid.UUID{firstUserID}, PreferredPositionID: &positionID,
		PreferredDepartmentID: &departmentID,
	})
	if err != nil {
		t.Fatalf("get report profiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].UserID != firstUserID ||
		profiles[0].PositionName == nil || *profiles[0].PositionName != "Разработчик" ||
		profiles[0].DepartmentName == nil || *profiles[0].DepartmentName != "ИТ" {
		t.Fatalf("report profiles = %+v", profiles)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database expectations: %v", err)
	}
}

func TestReportUserReadModelsRequireAdministrator(t *testing.T) {
	service := &Service{}
	actor := Actor{CompanyID: uuid.New(), UserID: uuid.New(), Role: "employee"}
	if _, err := service.ResolveReportUserScope(
		context.Background(), actor, ResolveReportUserScopeInput{},
	); !isCompanyError(err, ErrorForbidden) {
		t.Fatalf("employee report scope error = %v", err)
	}
	if _, err := service.GetReportUserProfiles(
		context.Background(), actor, GetReportUserProfilesInput{},
	); !isCompanyError(err, ErrorForbidden) {
		t.Fatalf("employee report profiles error = %v", err)
	}
}

func isCompanyError(err error, kind ErrorKind) bool {
	var value *Error
	return errors.As(err, &value) && value.Kind == kind
}
