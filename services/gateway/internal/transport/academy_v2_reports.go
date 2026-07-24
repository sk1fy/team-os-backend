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
	page, pageSize := pagination(params.Page, params.PageSize)
	report, err := h.internalReportPage(r, filters, page, pageSize)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	totalPages := (report.total + report.pageSize - 1) / report.pageSize
	writeJSON(w, http.StatusOK, api.InternalReportResult{
		Items: report.items, Page: report.page, PageSize: report.pageSize,
		Total: report.total, TotalPages: totalPages,
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
	report, err := h.internalReportPage(r, filters, 1, maxInternalReportExportRows+1)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	if report.total > maxInternalReportExportRows {
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
	for _, row := range report.items {
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
	page, pageSize := pagination(params.Page, params.PageSize)
	request := &academyv1.GetPartnerExternalReportPageRequest{
		Search: params.Q, Page: uint32(page), PageSize: uint32(pageSize),
	}
	if params.CourseId != nil {
		courseID := params.CourseId.String()
		request.CourseId = &courseID
	}
	response, err := h.academy.GetPartnerExternalReportPage(ctx, request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	rows := make([]api.PartnerExternalReportRow, len(response.GetItems()))
	for index, item := range response.GetItems() {
		enrollmentID, parseErr := uuid.Parse(item.GetEnrollmentId())
		if parseErr != nil {
			h.writeConversionError(w, r, parseErr)
			return
		}
		courseID, parseErr := uuid.Parse(item.GetCourseId())
		if parseErr != nil {
			h.writeConversionError(w, r, parseErr)
			return
		}
		rows[index] = api.PartnerExternalReportRow{
			EnrollmentId: enrollmentID, CourseId: courseID, CourseTitle: item.GetCourseTitle(),
			LearnerEmail: openapi_types.Email(item.GetLearnerEmail()), LearnerName: item.LearnerName,
			ProgressStatus: string(enrollmentProgressStatusFromProto(item.GetProgressStatus())),
			AccessStatus:   string(enrollmentAccessStatusFromProto(item.GetAccessStatus())),
			Percent:        int(item.GetProgressPercent()), ActivatedAt: protoTimestampPointer(item.GetActivatedAt()),
			CompletedAt: protoTimestampPointer(item.GetCompletedAt()),
		}
	}
	total := int(response.GetTotal())
	resolvedPageSize := int(response.GetPageSize())
	totalPages := 0
	if resolvedPageSize > 0 {
		totalPages = (total + resolvedPageSize - 1) / resolvedPageSize
	}
	writeJSON(w, http.StatusOK, api.PaginatedPartnerExternalReportRows{
		Items: rows, Page: int(response.GetPage()), PageSize: resolvedPageSize, Total: total, TotalPages: totalPages,
	})
}

type internalReportPage struct {
	items    []api.InternalReportRow
	page     int
	pageSize int
	total    int
}

func (h *Handler) internalReportPage(
	r *http.Request,
	filters internalReportFilters,
	page, pageSize int,
) (internalReportPage, error) {
	ctx := outgoingContext(r)
	normalizedQuery := strings.TrimSpace(filters.query)
	scopeRequest := &companyv1.ResolveReportUserScopeRequest{}
	if normalizedQuery != "" {
		scopeRequest.Search = &normalizedQuery
	}
	if filters.positionID != nil {
		scopeRequest.PositionId = protoStringPointer(filters.positionID.String())
	}
	if filters.departmentID != nil {
		scopeRequest.DepartmentId = protoStringPointer(filters.departmentID.String())
	}
	scope, err := h.company.ResolveReportUserScope(ctx, scopeRequest)
	if err != nil {
		return internalReportPage{}, err
	}
	request := &academyv1.GetInternalEnrollmentReportPageRequest{
		UserIds: scope.GetUserIds(), SearchUserIds: scope.GetSearchUserIds(),
		Status: internalReportStatusToProto(filters.status),
		Sort:   internalReportSortToProto(filters.sort),
		Page:   uint32(page), PageSize: uint32(pageSize),
	}
	if normalizedQuery != "" {
		request.Search = &normalizedQuery
	}
	if filters.courseID != nil {
		request.CourseId = protoStringPointer(filters.courseID.String())
	}
	response, err := h.academy.GetInternalEnrollmentReportPage(ctx, request)
	if err != nil {
		return internalReportPage{}, err
	}
	profileIDs := make([]string, 0, len(response.GetItems()))
	seenProfileIDs := make(map[string]struct{}, len(response.GetItems()))
	for _, enrollment := range response.GetItems() {
		userID := enrollment.GetUserId()
		if userID == "" {
			continue
		}
		if _, exists := seenProfileIDs[userID]; exists {
			continue
		}
		seenProfileIDs[userID] = struct{}{}
		profileIDs = append(profileIDs, userID)
	}
	profileRequest := &companyv1.GetReportUserProfilesRequest{UserIds: profileIDs}
	if filters.positionID != nil {
		profileRequest.PreferredPositionId = protoStringPointer(filters.positionID.String())
	}
	if filters.departmentID != nil {
		profileRequest.PreferredDepartmentId = protoStringPointer(filters.departmentID.String())
	}
	profiles, err := h.company.GetReportUserProfiles(ctx, profileRequest)
	if err != nil {
		return internalReportPage{}, err
	}
	profileByID := make(map[string]*companyv1.ReportUserProfile, len(profiles.GetProfiles()))
	for _, profile := range profiles.GetProfiles() {
		profileByID[profile.GetUserId()] = profile
	}
	summaries, err := h.enrollmentSummaries(response.GetItems())
	if err != nil {
		return internalReportPage{}, err
	}
	summaryByID := make(map[string]api.EnrollmentSummary, len(summaries))
	for _, summary := range summaries {
		summaryByID[summary.Id.String()] = summary
	}
	rows := make([]api.InternalReportRow, 0, len(response.GetItems()))
	for _, enrollment := range response.GetItems() {
		if enrollment.GetUserId() == "" {
			continue
		}
		userID, parseErr := uuid.Parse(enrollment.GetUserId())
		if parseErr != nil {
			return internalReportPage{}, parseErr
		}
		profile := profileByID[userID.String()]
		if profile == nil {
			continue
		}
		summary := summaryByID[enrollment.GetId()]
		name := strings.TrimSpace(profile.GetFirstName() + " " + profile.GetLastName())
		if name == "" {
			name = profile.GetEmail()
		}
		status := api.InternalReportStatus(summary.ProgressStatus)
		if enrollment.GetAccessStatus() == academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_FROZEN {
			status = api.InternalReportStatusFrozen
		} else if enrollment.GetOverdue() && summary.ProgressStatus != api.EnrollmentProgressStatusCompleted {
			status = api.InternalReportStatusOverdue
		}
		enrollmentID := summary.Id
		rows = append(rows, api.InternalReportRow{
			EnrollmentId: &enrollmentID, UserId: userID, UserName: name,
			DepartmentName: profile.DepartmentName, PositionName: profile.PositionName,
			CourseId: summary.CourseId, CourseTitle: summary.CourseTitle, Status: status,
			Percent: summary.Percent, CompletedLessons: summary.CompletedLessons, TotalLessons: summary.TotalLessons,
			DueDate: summary.DueDate, StartedAt: summary.StartedAt, CompletedAt: summary.CompletedAt,
			LastActivityAt: summary.LastActivityAt,
		})
	}
	return internalReportPage{
		items: rows, page: int(response.GetPage()),
		pageSize: int(response.GetPageSize()), total: int(response.GetTotal()),
	}, nil
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

func internalReportStatusToProto(value string) academyv1.InternalEnrollmentReportStatus {
	switch value {
	case "not_started":
		return academyv1.InternalEnrollmentReportStatus_INTERNAL_ENROLLMENT_REPORT_STATUS_NOT_STARTED
	case "in_progress":
		return academyv1.InternalEnrollmentReportStatus_INTERNAL_ENROLLMENT_REPORT_STATUS_IN_PROGRESS
	case "completed":
		return academyv1.InternalEnrollmentReportStatus_INTERNAL_ENROLLMENT_REPORT_STATUS_COMPLETED
	case "overdue":
		return academyv1.InternalEnrollmentReportStatus_INTERNAL_ENROLLMENT_REPORT_STATUS_OVERDUE
	case "frozen":
		return academyv1.InternalEnrollmentReportStatus_INTERNAL_ENROLLMENT_REPORT_STATUS_FROZEN
	default:
		return academyv1.InternalEnrollmentReportStatus_INTERNAL_ENROLLMENT_REPORT_STATUS_UNSPECIFIED
	}
}

func internalReportSortToProto(value string) academyv1.InternalEnrollmentReportSort {
	switch value {
	case "updated_asc":
		return academyv1.InternalEnrollmentReportSort_INTERNAL_ENROLLMENT_REPORT_SORT_UPDATED_ASC
	case "title_asc":
		return academyv1.InternalEnrollmentReportSort_INTERNAL_ENROLLMENT_REPORT_SORT_TITLE_ASC
	case "title_desc":
		return academyv1.InternalEnrollmentReportSort_INTERNAL_ENROLLMENT_REPORT_SORT_TITLE_DESC
	case "deadline_asc":
		return academyv1.InternalEnrollmentReportSort_INTERNAL_ENROLLMENT_REPORT_SORT_DEADLINE_ASC
	case "status":
		return academyv1.InternalEnrollmentReportSort_INTERNAL_ENROLLMENT_REPORT_SORT_STATUS
	case "updated_desc":
		return academyv1.InternalEnrollmentReportSort_INTERNAL_ENROLLMENT_REPORT_SORT_UPDATED_DESC
	default:
		return academyv1.InternalEnrollmentReportSort_INTERNAL_ENROLLMENT_REPORT_SORT_UNSPECIFIED
	}
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
