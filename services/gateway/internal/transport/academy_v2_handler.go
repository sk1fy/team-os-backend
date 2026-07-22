package transport

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/authmw"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (h *Handler) GetCourseDraft(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	ctx := outgoingContext(r)
	courseResponse, err := h.academy.GetCourse(ctx, &academyv1.GetCourseRequest{Id: courseID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	draftID := courseResponse.GetCourse().GetCurrentDraftVersionId()
	if draftID == "" {
		apierror.Write(w, apierror.NotFound("Черновик версии курса не найден"))
		return
	}
	response, err := h.academy.GetCourseVersion(ctx, &academyv1.GetCourseVersionRequest{
		CourseId: courseID.String(), VersionId: draftID,
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	converted, err := courseVersionAuthorDetailFromProto(response)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetCourseVersionLearner(w http.ResponseWriter, r *http.Request, versionID api.VersionId) {
	claims, ok := authmw.Claims(r.Context())
	if !ok || (claims.Role != "owner" && claims.Role != "admin" && claims.Role != "partner") {
		apierror.Write(w, apierror.Forbidden("Недостаточно прав для предпросмотра версии курса"))
		return
	}
	ctx := outgoingContext(r)
	courses, err := h.academy.GetCourses(ctx, &academyv1.GetCoursesRequest{})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	for _, course := range courses.GetCourses() {
		versions, listErr := h.academy.GetCourseVersions(ctx, &academyv1.GetCourseVersionsRequest{CourseId: course.GetId()})
		if listErr != nil {
			continue
		}
		for _, version := range versions.GetVersions() {
			if version.GetId() != versionID.String() {
				continue
			}
			response, getErr := h.academy.GetCourseVersion(ctx, &academyv1.GetCourseVersionRequest{
				CourseId: course.GetId(), VersionId: version.GetId(),
			})
			if getErr != nil {
				h.writeAcademyRPCError(w, r, getErr)
				return
			}
			author, convertErr := courseVersionAuthorDetailFromProto(response)
			if convertErr != nil {
				h.writeConversionError(w, r, convertErr)
				return
			}
			writeJSON(w, http.StatusOK, courseVersionLearnerDetailFromAuthor(author))
			return
		}
	}
	apierror.Write(w, apierror.NotFound("Версия курса не найдена"))
}

func (h *Handler) DeleteCourseVersionLessonQuiz(w http.ResponseWriter, r *http.Request, lessonID api.LessonId) {
	_, err := h.academy.DeleteCourseVersionQuiz(outgoingContext(r), &academyv1.DeleteCourseVersionQuizRequest{
		LessonVersionId: lessonID.String(),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetCourseTemplatePreview(w http.ResponseWriter, r *http.Request, templateID api.TemplateId) {
	ctx := outgoingContext(r)
	base, err := h.academy.GetCourseTemplate(ctx, &academyv1.GetCourseTemplateRequest{TemplateId: templateID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	versionID := base.GetTemplate().GetLatestPublishedVersionId()
	if versionID == "" {
		apierror.Write(w, apierror.NotFound("Опубликованная версия шаблона не найдена"))
		return
	}
	response, err := h.academy.GetCourseTemplate(ctx, &academyv1.GetCourseTemplateRequest{
		TemplateId: templateID.String(), VersionId: &versionID,
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	convertedTemplate, err := courseTemplateDetailsFromProto(response)
	if err != nil || convertedTemplate.SelectedVersion == nil {
		if err == nil {
			err = errors.New("academy returned template without selected version")
		}
		h.writeConversionError(w, r, err)
		return
	}
	// V2 frontend intentionally reuses CourseVersionAuthorDetail here. For
	// template previews courseId is the template root ID; no Course is implied.
	converted, err := templateVersionAuthorDetail(*convertedTemplate.SelectedVersion)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetCourseAssignments(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	if !requireAcademyManager(w, r) {
		return
	}
	values, err := h.courseAssignmentSummaries(outgoingContext(r), courseID)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, values)
}

func (h *Handler) CreateCourseAssignment(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	if !requireAcademyManager(w, r) {
		return
	}
	var input api.CreateCourseAssignmentInput
	if !decode(w, r, &input) {
		return
	}
	request := &academyv1.AssignCourseRequest{CourseId: courseID.String(), AssigneeId: protoStringPointer(input.TargetId.String())}
	switch input.TargetType {
	case api.AssignmentTargetTypeUser:
		request.AssigneeType = academyv1.AssigneeType_ASSIGNEE_TYPE_USER
	case api.AssignmentTargetTypePosition:
		request.AssigneeType = academyv1.AssigneeType_ASSIGNEE_TYPE_POSITION
	case api.AssignmentTargetTypeDepartment:
		request.AssigneeType = academyv1.AssigneeType_ASSIGNEE_TYPE_DEPARTMENT
	default:
		apierror.Write(w, apierror.BadRequest("Некорректный тип назначения"))
		return
	}
	if input.CourseVersionId != nil {
		value := input.CourseVersionId.String()
		request.CourseVersionId = &value
	}
	if input.DueDate != nil {
		request.DueDate = timestamppb.New(*input.DueDate)
	}
	response, err := h.academy.AssignCourse(outgoingContext(r), request)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	values, err := h.courseAssignmentSummaries(outgoingContext(r), courseID)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	for _, value := range values {
		if value.Id.String() == response.GetAssignment().GetId() {
			writeJSON(w, http.StatusCreated, value)
			return
		}
	}
	h.writeConversionError(w, r, errors.New("created assignment missing from course scope"))
}

func (h *Handler) RevokeCourseAssignment(w http.ResponseWriter, r *http.Request, assignmentID api.ID) {
	if !requireAcademyManager(w, r) {
		return
	}
	_, err := h.academy.RevokeAssignment(outgoingContext(r), &academyv1.RevokeAssignmentRequest{AssignmentId: assignmentID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetMyEnrollments(w http.ResponseWriter, r *http.Request, params api.GetMyEnrollmentsParams) {
	values, err := h.myEnrollmentSummaries(r, params.Status)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	page, pageSize := pagination(params.Page, params.PageSize)
	writeJSON(w, http.StatusOK, paginateEnrollments(values, page, pageSize))
}

func (h *Handler) GetMyLearning(w http.ResponseWriter, r *http.Request) {
	values, err := h.myEnrollmentSummaries(r, nil)
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	result := api.MyLearningSummary{Enrollments: values}
	for index := range values {
		value := &values[index]
		result.Stats.TotalAssigned++
		switch value.ProgressStatus {
		case api.EnrollmentProgressStatusCompleted:
			result.Stats.Completed++
		case api.EnrollmentProgressStatusInProgress:
			result.Stats.InProgress++
			if result.ContinueEnrollment == nil {
				result.ContinueEnrollment = value
			}
		}
		if value.DueDate != nil && value.ProgressStatus != api.EnrollmentProgressStatusCompleted && value.DueDate.Before(nowUTC()) {
			result.Stats.Overdue++
		}
	}
	if result.ContinueEnrollment == nil && len(values) > 0 {
		result.ContinueEnrollment = &values[0]
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetMyCourseEnrollment(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	claims, ok := authmw.Claims(r.Context())
	if !ok {
		apierror.Write(w, apierror.Unauthorized())
		return
	}
	response, err := h.academy.GetEnrollments(outgoingContext(r), &academyv1.GetEnrollmentsRequest{
		CourseId: protoStringPointer(courseID.String()), UserId: protoStringPointer(claims.Subject),
	})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	for _, enrollment := range response.GetEnrollments() {
		if enrollment.GetAccessStatus() == academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_REVOKED ||
			enrollment.GetAccessStatus() == academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_CLOSED ||
			enrollment.GetAccessStatus() == academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_EXPIRED {
			continue
		}
		id, parseErr := uuid.Parse(enrollment.GetId())
		if parseErr != nil {
			h.writeConversionError(w, r, parseErr)
			return
		}
		writeJSON(w, http.StatusOK, api.MyCourseEnrollment{EnrollmentId: id})
		return
	}
	apierror.Write(w, apierror.NotFound("Актуальное прохождение курса не найдено"))
}

func (h *Handler) SelfEnrollAcademyCourse(w http.ResponseWriter, r *http.Request, courseID api.CourseId) {
	response, err := h.academy.SelfEnrollCourse(outgoingContext(r), &academyv1.SelfEnrollCourseRequest{CourseId: courseID.String()})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	values, err := h.enrollmentSummaries(outgoingContext(r), []*academyv1.CourseEnrollment{response.GetEnrollment()})
	if err != nil || len(values) != 1 {
		if err == nil {
			err = errors.New("academy returned empty self enrollment")
		}
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, values[0])
}

func (h *Handler) GetAcademyCatalog(w http.ResponseWriter, r *http.Request, params api.GetAcademyCatalogParams) {
	ctx := outgoingContext(r)
	claims, ok := authmw.Claims(r.Context())
	if !ok {
		apierror.Write(w, apierror.Unauthorized())
		return
	}
	if claims.Role == "partner" {
		page, pageSize := pagination(params.Page, params.PageSize)
		writeJSON(w, http.StatusOK, api.PaginatedCatalogCourseCards{Items: []api.CatalogCourseCard{}, Page: page, PageSize: pageSize, Total: 0, TotalPages: 0})
		return
	}
	courseResponse, err := h.academy.GetCourses(ctx, &academyv1.GetCoursesRequest{})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	enrollmentResponse, err := h.academy.GetEnrollments(ctx, &academyv1.GetEnrollmentsRequest{UserId: protoStringPointer(claims.Subject)})
	if err != nil {
		h.writeAcademyRPCError(w, r, err)
		return
	}
	enrolled := make(map[string]*academyv1.CourseEnrollment)
	for _, enrollment := range enrollmentResponse.GetEnrollments() {
		if enrollment.GetAccessStatus() == academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_REVOKED ||
			enrollment.GetAccessStatus() == academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_CLOSED ||
			enrollment.GetAccessStatus() == academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_EXPIRED {
			continue
		}
		if _, exists := enrolled[enrollment.GetCourseId()]; !exists {
			enrolled[enrollment.GetCourseId()] = enrollment
		}
	}
	query := ""
	if params.Q != nil {
		query = strings.ToLower(strings.TrimSpace(*params.Q))
	}
	courses := make([]*academyv1.Course, 0, len(courseResponse.GetCourses()))
	for _, course := range courseResponse.GetCourses() {
		if course.GetOwnerType() != academyv1.CourseOwnerType_COURSE_OWNER_TYPE_COMPANY ||
			course.GetLifecycleStatus() != academyv1.CourseLifecycleStatus_COURSE_LIFECYCLE_STATUS_ACTIVE ||
			course.GetDistributionStatus() != academyv1.CourseDistributionStatus_COURSE_DISTRIBUTION_STATUS_ACTIVE ||
			course.GetStatus() != academyv1.CourseStatus_COURSE_STATUS_PUBLISHED || course.GetLatestPublishedVersionId() == "" ||
			(course.GetVisibility() != academyv1.CourseVisibility_COURSE_VISIBILITY_PUBLIC &&
				course.GetVisibility() != academyv1.CourseVisibility_COURSE_VISIBILITY_COMPANY) ||
			(query != "" && !strings.Contains(strings.ToLower(course.GetTitle()+" "+course.GetDescription()), query)) {
			continue
		}
		courses = append(courses, course)
	}
	sort.SliceStable(courses, func(i, j int) bool {
		return strings.ToLower(courses[i].GetTitle()) < strings.ToLower(courses[j].GetTitle())
	})
	page, pageSize := pagination(params.Page, params.PageSize)
	start, end := pageWindow(len(courses), page, pageSize)
	items := make([]api.CatalogCourseCard, 0, end-start)
	for _, course := range courses[start:end] {
		versionResponse, getErr := h.academy.GetCatalogCourseVersion(ctx, &academyv1.GetCatalogCourseVersionRequest{CourseId: course.GetId()})
		if getErr != nil {
			h.writeAcademyRPCError(w, r, getErr)
			return
		}
		id, parseErr := uuid.Parse(course.GetId())
		if parseErr != nil {
			h.writeConversionError(w, r, parseErr)
			return
		}
		version := versionResponse.GetVersion()
		minutes, lessonCount := 0, 0
		for _, section := range version.GetSections() {
			for _, lesson := range section.GetLessons() {
				lessonCount++
				minutes += int(lesson.GetEstimatedMinutes())
			}
		}
		versionNumber := int(version.GetNumber())
		item := api.CatalogCourseCard{
			Id: id, Title: course.GetTitle(), Description: course.Description, CoverUrl: course.CoverUrl,
			LessonCount: lessonCount, EstimatedMinutes: &minutes, LatestVersionNumber: &versionNumber,
		}
		isEnrolled := false
		if enrollment := enrolled[course.GetId()]; enrollment != nil {
			isEnrolled = true
			enrollmentID, parseEnrollmentErr := uuid.Parse(enrollment.GetId())
			if parseEnrollmentErr != nil {
				h.writeConversionError(w, r, parseEnrollmentErr)
				return
			}
			progress := int(enrollment.GetProgressPercent())
			item.EnrollmentId, item.ProgressPercent = &enrollmentID, &progress
		}
		item.Enrolled = &isEnrolled
		items = append(items, item)
	}
	totalPages := (len(courses) + pageSize - 1) / pageSize
	writeJSON(w, http.StatusOK, api.PaginatedCatalogCourseCards{
		Items: items, Page: page, PageSize: pageSize, Total: len(courses), TotalPages: totalPages,
	})
}

func (h *Handler) myEnrollmentSummaries(r *http.Request, statusFilter *api.EnrollmentProgressStatus) ([]api.EnrollmentSummary, error) {
	claims, ok := authmw.Claims(r.Context())
	if !ok {
		return nil, errors.New("missing authenticated claims")
	}
	request := &academyv1.GetEnrollmentsRequest{UserId: protoStringPointer(claims.Subject)}
	if statusFilter != nil {
		status := academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_UNSPECIFIED
		switch *statusFilter {
		case api.EnrollmentProgressStatusNotStarted:
			status = academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_NOT_STARTED
		case api.EnrollmentProgressStatusInProgress:
			status = academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_IN_PROGRESS
		case api.EnrollmentProgressStatusCompleted:
			status = academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_COMPLETED
		default:
			return nil, errors.New("invalid enrollment progress status")
		}
		request.ProgressStatus = &status
	}
	response, err := h.academy.GetEnrollments(outgoingContext(r), request)
	if err != nil {
		return nil, err
	}
	return h.enrollmentSummaries(outgoingContext(r), response.GetEnrollments())
}

func (h *Handler) enrollmentSummaries(ctx context.Context, values []*academyv1.CourseEnrollment) ([]api.EnrollmentSummary, error) {
	courses := make(map[string]*academyv1.Course)
	result := make([]api.EnrollmentSummary, 0, len(values))
	for _, value := range values {
		base, err := academyEnrollmentFromProto(value)
		if err != nil {
			return nil, err
		}
		course := courses[value.GetCourseId()]
		if course == nil {
			response, getErr := h.academy.GetCourse(ctx, &academyv1.GetCourseRequest{Id: value.GetCourseId()})
			if getErr != nil {
				return nil, getErr
			}
			course = response.GetCourse()
			courses[value.GetCourseId()] = course
		}
		outline, err := h.academy.GetEnrollmentOutline(ctx, &academyv1.GetEnrollmentOutlineRequest{EnrollmentId: value.GetId()})
		if err != nil {
			return nil, err
		}
		total, completed := 0, 0
		for _, section := range outline.GetOutline().GetSections() {
			for _, lesson := range section.GetLessons() {
				total++
				if lesson.GetStatus() == academyv1.EnrollmentLessonStatus_ENROLLMENT_LESSON_STATUS_COMPLETED {
					completed++
				}
			}
		}
		summary := api.EnrollmentSummary{
			Id: base.Id, CourseId: base.CourseId, CourseVersionId: base.CourseVersionId,
			CourseTitle: course.GetTitle(), CourseCoverUrl: course.CoverUrl,
			LearnerType: api.EnrollmentSummaryLearnerType(base.LearnerType), ProgressStatus: base.ProgressStatus,
			AccessStatus: base.AccessStatus, Percent: base.ProgressPercent,
			CompletedLessons: completed, TotalLessons: total, CurrentLessonId: base.CurrentLessonVersionId,
			ActivatedAt: base.ActivatedAt, AccessUntil: base.AccessUntil, DueDate: base.DueDate,
			StartedAt: base.StartedAt, CompletedAt: base.CompletedAt, LastActivityAt: base.LastActivityAt,
		}
		if base.SourceId != nil {
			switch value.GetSourceType() {
			case academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_ASSIGNMENT:
				summary.AssignmentId = base.SourceId
			case academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_PARTNER_PROMO_CAMPAIGN,
				academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_COMPANY_CANDIDATE_CAMPAIGN:
				summary.CampaignId = base.SourceId
			}
		}
		result = append(result, summary)
	}
	return result, nil
}

func (h *Handler) courseAssignmentSummaries(ctx context.Context, courseID uuid.UUID) ([]api.CourseAssignmentSummary, error) {
	assignments, err := h.academy.GetAssignments(ctx, &academyv1.GetAssignmentsRequest{})
	if err != nil {
		return nil, err
	}
	enrollments, err := h.academy.GetEnrollments(ctx, &academyv1.GetEnrollmentsRequest{CourseId: protoStringPointer(courseID.String())})
	if err != nil {
		return nil, err
	}
	directory, err := h.loadCompanyDirectory(ctx)
	if err != nil {
		return nil, err
	}
	result := []api.CourseAssignmentSummary{}
	for _, value := range assignments.GetAssignments() {
		if value.GetCourseId() != courseID.String() || value.GetAssigneeId() == "" || value.GetCourseVersionId() == "" {
			continue
		}
		base, convertErr := assignmentFromProto(value)
		if convertErr != nil {
			return nil, convertErr
		}
		targetID, parseErr := uuid.Parse(value.GetAssigneeId())
		if parseErr != nil {
			return nil, parseErr
		}
		versionID, parseErr := uuid.Parse(value.GetCourseVersionId())
		if parseErr != nil {
			return nil, parseErr
		}
		item := api.CourseAssignmentSummary{
			Id: base.Id, CourseId: courseID, CourseVersionId: versionID,
			TargetType: api.AssignmentTargetType(base.AssigneeType), TargetId: targetID,
			DueDate: base.DueDate, AssignedById: base.AssignedById, CreatedAt: base.CreatedAt,
		}
		item.TargetName = directory.targetName(item.TargetType, targetID)
		item.AssignedByName = directory.userName(base.AssignedById)
		for _, enrollment := range enrollments.GetEnrollments() {
			if enrollment.GetSourceType() != academyv1.EnrollmentSourceType_ENROLLMENT_SOURCE_TYPE_ASSIGNMENT ||
				enrollment.GetSourceId() != value.GetId() {
				continue
			}
			if enrollment.GetProgressStatus() == academyv1.EnrollmentProgressStatus_ENROLLMENT_PROGRESS_STATUS_COMPLETED {
				item.CompletedEnrollments++
			} else if enrollment.GetAccessStatus() != academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_REVOKED &&
				enrollment.GetAccessStatus() != academyv1.EnrollmentAccessStatus_ENROLLMENT_ACCESS_STATUS_CLOSED {
				item.ActiveEnrollments++
			}
		}
		result = append(result, item)
	}
	return result, nil
}

type companyDirectory struct {
	users       map[uuid.UUID]*companyv1.User
	positions   map[uuid.UUID]*companyv1.Position
	departments map[uuid.UUID]*companyv1.Department
}

func (h *Handler) loadCompanyDirectory(ctx context.Context) (companyDirectory, error) {
	users, err := h.company.GetUsers(ctx, &companyv1.GetUsersRequest{})
	if err != nil {
		return companyDirectory{}, err
	}
	positions, err := h.company.GetPositions(ctx, &companyv1.GetPositionsRequest{})
	if err != nil {
		return companyDirectory{}, err
	}
	departments, err := h.company.GetDepartments(ctx, &companyv1.GetDepartmentsRequest{})
	if err != nil {
		return companyDirectory{}, err
	}
	result := companyDirectory{
		users: make(map[uuid.UUID]*companyv1.User), positions: make(map[uuid.UUID]*companyv1.Position),
		departments: make(map[uuid.UUID]*companyv1.Department),
	}
	for _, value := range users.GetUsers() {
		if id, parseErr := uuid.Parse(value.GetId()); parseErr == nil {
			result.users[id] = value
		}
	}
	for _, value := range positions.GetPositions() {
		if id, parseErr := uuid.Parse(value.GetId()); parseErr == nil {
			result.positions[id] = value
		}
	}
	for _, value := range departments.GetDepartments() {
		if id, parseErr := uuid.Parse(value.GetId()); parseErr == nil {
			result.departments[id] = value
		}
	}
	return result, nil
}

func (d companyDirectory) userName(id uuid.UUID) *string {
	value := d.users[id]
	if value == nil {
		return nil
	}
	name := strings.TrimSpace(value.GetFirstName() + " " + value.GetLastName())
	if name == "" {
		name = value.GetEmail()
	}
	return &name
}

func (d companyDirectory) targetName(kind api.AssignmentTargetType, id uuid.UUID) *string {
	switch kind {
	case api.AssignmentTargetTypeUser:
		return d.userName(id)
	case api.AssignmentTargetTypePosition:
		if value := d.positions[id]; value != nil {
			name := value.GetName()
			return &name
		}
	case api.AssignmentTargetTypeDepartment:
		if value := d.departments[id]; value != nil {
			name := value.GetName()
			return &name
		}
	}
	return nil
}

func pagination(page, pageSize *int) (int, int) {
	p, size := 1, 20
	if page != nil && *page > 0 {
		p = *page
	}
	if pageSize != nil && *pageSize > 0 {
		size = *pageSize
	}
	if size > 100 {
		size = 100
	}
	return p, size
}

func pageWindow(total, page, pageSize int) (int, int) {
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return start, end
}

func paginateEnrollments(values []api.EnrollmentSummary, page, pageSize int) api.PaginatedEnrollmentSummaries {
	start, end := pageWindow(len(values), page, pageSize)
	totalPages := (len(values) + pageSize - 1) / pageSize
	return api.PaginatedEnrollmentSummaries{
		Items: values[start:end], Page: page, PageSize: pageSize, Total: len(values), TotalPages: totalPages,
	}
}

func protoStringPointer(value string) *string { return &value }

func nowUTC() (now time.Time) { return time.Now().UTC() }
