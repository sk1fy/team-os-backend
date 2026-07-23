package transport

import (
	"encoding/csv"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/authmw"
)

const maxInternalReportExportRows = 10_000

type internalReportFilters struct {
	query        string
	courseID     *uuid.UUID
	departmentID *uuid.UUID
	positionID   *uuid.UUID
	status       string
	sort         string
}

func (h *Handler) GetAcademyInternalReport(w http.ResponseWriter, r *http.Request, params api.GetAcademyInternalReportParams) {
	if !requireAcademyManager(w, r) {
		return
	}
	filters := internalReportFiltersFromParams(params)
	rows, err := h.internalReportRows(r, filters)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	page, pageSize := pagination(params.Page, params.PageSize)
	start, end := pageWindow(len(rows), page, pageSize)
	totalPages := (len(rows) + pageSize - 1) / pageSize
	writeJSON(w, http.StatusOK, api.InternalReportResult{
		Items: rows[start:end], Page: page, PageSize: pageSize, Total: len(rows), TotalPages: totalPages,
		FiltersApplied: internalFiltersApplied(filters),
	})
}

func (h *Handler) ExportAcademyInternalReport(w http.ResponseWriter, r *http.Request, params api.ExportAcademyInternalReportParams) {
	if !requireAcademyManager(w, r) {
		return
	}
	filters := internalReportFilters{
		query: pointerString((*string)(params.Q)), courseID: uuidPointer(params.CourseId),
		departmentID: uuidPointer(params.DepartmentId), positionID: uuidPointer(params.PositionId),
		status: pointerString((*string)(params.Status)), sort: pointerString((*string)(params.Sort)),
	}
	rows, err := h.internalReportRows(r, filters)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	if len(rows) > maxInternalReportExportRows {
		apierror.Write(w, apierror.BadRequest("Выгрузка ограничена 10 000 строками; уточните фильтры"))
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="academy-internal-report.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{
		"Сотрудник", "Отдел", "Должность", "Курс", "Статус", "Прогресс, %",
		"Пройдено уроков", "Всего уроков", "Срок", "Начато", "Завершено", "Последняя активность",
	})
	for _, row := range rows {
		_ = writer.Write([]string{
			csvSafe(row.UserName), csvSafe(pointerString(row.DepartmentName)), csvSafe(pointerString(row.PositionName)),
			csvSafe(row.CourseTitle), string(row.Status), strconv.Itoa(row.Percent),
			strconv.Itoa(row.CompletedLessons), strconv.Itoa(row.TotalLessons), formatCSVTime(row.DueDate),
			formatCSVTime(row.StartedAt), formatCSVTime(row.CompletedAt), formatCSVTime(row.LastActivityAt),
		})
	}
	writer.Flush()
}

func (h *Handler) GetAcademyExternalReport(w http.ResponseWriter, r *http.Request, params api.GetAcademyExternalReportParams) {
	claims, ok := authmw.Claims(r.Context())
	if !ok || claims.Role != "partner" {
		apierror.Write(w, apierror.Forbidden("Отчёт доступен только партнёру"))
		return
	}
	ctx := outgoingContext(r)
	ownerType := academyv1.CourseOwnerType_COURSE_OWNER_TYPE_PARTNER
	coursesResponse, err := h.academy.GetCourses(ctx, &academyv1.GetCoursesRequest{
		OwnerType: &ownerType, PartnerId: protoStringPointer(claims.Subject),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	query := ""
	if params.Q != nil {
		query = strings.ToLower(strings.TrimSpace(*params.Q))
	}
	learners := make(map[string]*academyv1.ExternalLearner)
	rows := []api.PartnerExternalReportRow{}
	for _, course := range coursesResponse.GetCourses() {
		if params.CourseId != nil && course.GetId() != params.CourseId.String() {
			continue
		}
		report, reportErr := h.academy.GetCourseExternalReport(ctx, &academyv1.GetCourseExternalReportRequest{CourseId: course.GetId()})
		if reportErr != nil {
			h.writeAcademyRPCError(w, r, reportErr)
			return
		}
		courseID, parseErr := uuid.Parse(course.GetId())
		if parseErr != nil {
			h.writeConversionError(w, r, parseErr)
			return
		}
		for _, enrollment := range report.GetReport().GetEnrollments() {
			learnerID := enrollment.GetExternalLearnerId()
			learner := learners[learnerID]
			if learner == nil {
				learnerResponse, learnerErr := h.academy.GetExternalLearner(ctx, &academyv1.GetExternalLearnerRequest{LearnerId: learnerID})
				if learnerErr != nil {
					h.writeAcademyRPCError(w, r, learnerErr)
					return
				}
				learner = learnerResponse.GetLearner()
				learners[learnerID] = learner
			}
			name := strings.TrimSpace(learner.GetFirstName() + " " + learner.GetLastName())
			searchable := strings.ToLower(course.GetTitle() + " " + learner.GetEmail() + " " + name)
			if query != "" && !strings.Contains(searchable, query) {
				continue
			}
			enrollmentID, parseErr := uuid.Parse(enrollment.GetId())
			if parseErr != nil {
				h.writeConversionError(w, r, parseErr)
				return
			}
			row := api.PartnerExternalReportRow{
				EnrollmentId: enrollmentID, CourseId: courseID, CourseTitle: course.GetTitle(),
				LearnerEmail: openapi_types.Email(learner.GetEmail()), ProgressStatus: string(enrollmentProgressStatusFromProto(enrollment.GetProgressStatus())),
				AccessStatus: string(enrollmentAccessStatusFromProto(enrollment.GetAccessStatus())), Percent: int(enrollment.GetProgressPercent()),
				ActivatedAt: protoTimestampPointer(enrollment.GetActivatedAt()), CompletedAt: protoTimestampPointer(enrollment.GetCompletedAt()),
			}
			if name != "" {
				row.LearnerName = &name
			}
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i].ActivatedAt, rows[j].ActivatedAt
		return left != nil && (right == nil || left.After(*right))
	})
	page, pageSize := pagination(params.Page, params.PageSize)
	start, end := pageWindow(len(rows), page, pageSize)
	totalPages := (len(rows) + pageSize - 1) / pageSize
	writeJSON(w, http.StatusOK, api.PaginatedPartnerExternalReportRows{
		Items: rows[start:end], Page: page, PageSize: pageSize, Total: len(rows), TotalPages: totalPages,
	})
}

func (h *Handler) internalReportRows(r *http.Request, filters internalReportFilters) ([]api.InternalReportRow, error) {
	ctx := outgoingContext(r)
	request := &academyv1.GetEnrollmentsRequest{}
	if filters.courseID != nil {
		request.CourseId = protoStringPointer(filters.courseID.String())
	}
	response, err := h.academy.GetEnrollments(ctx, request)
	if err != nil {
		return nil, err
	}
	summaries, err := h.enrollmentSummaries(ctx, response.GetEnrollments())
	if err != nil {
		return nil, err
	}
	directory, err := h.loadCompanyDirectory(ctx)
	if err != nil {
		return nil, err
	}
	summaryByID := make(map[string]api.EnrollmentSummary, len(summaries))
	for _, summary := range summaries {
		summaryByID[summary.Id.String()] = summary
	}
	rows := make([]api.InternalReportRow, 0, len(response.GetEnrollments()))
	for _, enrollment := range response.GetEnrollments() {
		if enrollment.GetUserId() == "" {
			continue
		}
		userID, parseErr := uuid.Parse(enrollment.GetUserId())
		if parseErr != nil {
			return nil, parseErr
		}
		user := directory.users[userID]
		if user == nil {
			continue
		}
		_, _, positionName, departmentName, matchesOrg := directory.userOrg(user, filters.positionID, filters.departmentID)
		if !matchesOrg {
			continue
		}
		summary := summaryByID[enrollment.GetId()]
		name := strings.TrimSpace(user.GetFirstName() + " " + user.GetLastName())
		if name == "" {
			name = user.GetEmail()
		}
		if filters.query != "" && !strings.Contains(strings.ToLower(name+" "+user.GetEmail()+" "+summary.CourseTitle), strings.ToLower(filters.query)) {
			continue
		}
		status := api.InternalReportStatus(summary.ProgressStatus)
		if enrollment.GetAccessStatus() == academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_FROZEN {
			status = api.InternalReportStatusFrozen
		} else if enrollment.GetOverdue() && summary.ProgressStatus != api.EnrollmentProgressStatusCompleted {
			status = api.InternalReportStatusOverdue
		}
		if filters.status != "" && string(status) != filters.status {
			continue
		}
		enrollmentID := summary.Id
		rows = append(rows, api.InternalReportRow{
			EnrollmentId: &enrollmentID, UserId: userID, UserName: name,
			DepartmentName: departmentName, PositionName: positionName,
			CourseId: summary.CourseId, CourseTitle: summary.CourseTitle, Status: status,
			Percent: summary.Percent, CompletedLessons: summary.CompletedLessons, TotalLessons: summary.TotalLessons,
			DueDate: summary.DueDate, StartedAt: summary.StartedAt, CompletedAt: summary.CompletedAt,
			LastActivityAt: summary.LastActivityAt,
		})
	}
	sortInternalReport(rows, filters.sort)
	return rows, nil
}

func (d companyDirectory) userOrg(
	user *companyv1.User,
	preferredPositionID, preferredDepartmentID *uuid.UUID,
) (*uuid.UUID, *uuid.UUID, *string, *string, bool) {
	if user == nil || len(user.GetPositionIds()) == 0 {
		return nil, nil, nil, nil, preferredPositionID == nil && preferredDepartmentID == nil
	}
	type candidate struct {
		positionID   uuid.UUID
		position     *companyv1.Position
		departmentID *uuid.UUID
	}
	candidates := make([]candidate, 0, len(user.GetPositionIds()))
	for _, rawID := range user.GetPositionIds() {
		positionID, err := uuid.Parse(rawID)
		if err != nil {
			continue
		}
		item := candidate{positionID: positionID, position: d.positions[positionID]}
		if item.position != nil {
			if departmentID, parseErr := uuid.Parse(item.position.GetDepartmentId()); parseErr == nil {
				item.departmentID = &departmentID
			}
		}
		candidates = append(candidates, item)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].positionID.String() < candidates[j].positionID.String() })
	for _, item := range candidates {
		if preferredPositionID != nil && item.positionID != *preferredPositionID {
			continue
		}
		if preferredDepartmentID != nil && (item.departmentID == nil || *item.departmentID != *preferredDepartmentID) {
			continue
		}
		var positionName, departmentName *string
		if item.position != nil {
			name := item.position.GetName()
			positionName = &name
		}
		if item.departmentID != nil {
			if department := d.departments[*item.departmentID]; department != nil {
				name := department.GetName()
				departmentName = &name
			}
		}
		return &item.positionID, item.departmentID, positionName, departmentName, true
	}
	return nil, nil, nil, nil, false
}

func requireAcademyManager(w http.ResponseWriter, r *http.Request) bool {
	claims, ok := authmw.Claims(r.Context())
	if !ok || (claims.Role != "owner" && claims.Role != "admin") {
		apierror.Write(w, apierror.Forbidden("Действие доступно только владельцу или администратору"))
		return false
	}
	return true
}

func internalReportFiltersFromParams(params api.GetAcademyInternalReportParams) internalReportFilters {
	return internalReportFilters{
		query: pointerString((*string)(params.Q)), courseID: uuidPointer(params.CourseId),
		departmentID: uuidPointer(params.DepartmentId), positionID: uuidPointer(params.PositionId),
		status: pointerString((*string)(params.Status)), sort: pointerString((*string)(params.Sort)),
	}
}

func internalFiltersApplied(filters internalReportFilters) map[string]interface{} {
	result := map[string]interface{}{}
	if filters.query != "" {
		result["q"] = filters.query
	}
	if filters.courseID != nil {
		result["courseId"] = filters.courseID.String()
	}
	if filters.departmentID != nil {
		result["departmentId"] = filters.departmentID.String()
	}
	if filters.positionID != nil {
		result["positionId"] = filters.positionID.String()
	}
	if filters.status != "" {
		result["status"] = filters.status
	}
	if filters.sort != "" {
		result["sort"] = filters.sort
	}
	return result
}

func sortInternalReport(rows []api.InternalReportRow, mode string) {
	sort.SliceStable(rows, func(i, j int) bool {
		switch mode {
		case "title_asc":
			return strings.ToLower(rows[i].CourseTitle) < strings.ToLower(rows[j].CourseTitle)
		case "title_desc":
			return strings.ToLower(rows[i].CourseTitle) > strings.ToLower(rows[j].CourseTitle)
		case "deadline_asc":
			return timeBefore(rows[i].DueDate, rows[j].DueDate)
		case "status":
			return rows[i].Status < rows[j].Status
		case "updated_asc":
			return timeBefore(rows[i].LastActivityAt, rows[j].LastActivityAt)
		default:
			return timeAfter(rows[i].LastActivityAt, rows[j].LastActivityAt)
		}
	})
}

func timeBefore(left, right *time.Time) bool {
	return left != nil && (right == nil || left.Before(*right))
}
func timeAfter(left, right *time.Time) bool {
	return left != nil && (right == nil || left.After(*right))
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func uuidPointer(value *uuid.UUID) *uuid.UUID {
	if value == nil {
		return nil
	}
	converted := *value
	return &converted
}

func csvSafe(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}

func formatCSVTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
