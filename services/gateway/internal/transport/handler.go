package transport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	academyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/academy/v1"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	filesv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/files/v1"
	kbv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/kb/v1"
	notificationsv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/notifications/v1"
	tasksv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/tasks/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/pkg/httpx"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/authmw"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const refreshCookieName = "teamos_refresh"

type CookieConfig struct {
	Secure       bool
	PublicAppURL string
}

type Handler struct {
	api.Unimplemented
	company       companyv1.CompanyServiceClient
	kb            kbv1.KbServiceClient
	tasks         tasksv1.TasksServiceClient
	academy       academyv1.AcademyServiceClient
	notifications notificationsv1.NotificationsServiceClient
	files         filesv1.FilesServiceClient
	cookie        CookieConfig
	logger        *slog.Logger
}

func (h *Handler) SetFilesClient(client filesv1.FilesServiceClient) { h.files = client }

func NewHandler(
	companyClient companyv1.CompanyServiceClient,
	kbClient kbv1.KbServiceClient,
	tasksClient tasksv1.TasksServiceClient,
	academyClient academyv1.AcademyServiceClient,
	cookie CookieConfig,
	logger *slog.Logger,
	notificationClients ...notificationsv1.NotificationsServiceClient,
) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{
		company: companyClient, kb: kbClient, tasks: tasksClient, academy: academyClient,
		cookie: cookie, logger: logger,
	}
	if len(notificationClients) > 0 {
		h.notifications = notificationClients[0]
	}
	return h
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var input api.LoginInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.company.Login(outgoingContext(r), &companyv1.LoginRequest{
		Email: string(input.Email), Password: input.Password,
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	h.writeSession(w, r, http.StatusOK, response.GetSession())
}

func (h *Handler) LoginWithAccessLink(w http.ResponseWriter, r *http.Request, token api.AccessLinkToken) {
	response, err := h.company.LoginWithAccessLink(outgoingContext(r), &companyv1.LoginWithAccessLinkRequest{Token: token})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	h.writeSession(w, r, http.StatusOK, response.GetSession())
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var input api.RegisterInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.company.Register(outgoingContext(r), &companyv1.RegisterRequest{
		CompanyName: input.CompanyName, Email: string(input.Email), Password: input.Password,
		FirstName: input.FirstName, LastName: input.LastName,
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	h.writeSession(w, r, http.StatusCreated, response.GetSession())
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(refreshCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		apierror.Write(w, apierror.Unauthorized("Сессия недействительна или истекла"))
		return
	}
	response, err := h.company.Refresh(outgoingContext(r), &companyv1.RefreshRequest{RefreshToken: cookie.Value})
	if err != nil {
		if status.Code(err) == codes.Unauthenticated {
			h.clearRefreshCookie(w)
		}
		h.writeRPCError(w, r, err)
		return
	}
	h.writeSession(w, r, http.StatusOK, response.GetSession())
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	refreshToken := ""
	if cookie, err := r.Cookie(refreshCookieName); err == nil {
		refreshToken = cookie.Value
	}
	_, err := h.company.Logout(outgoingContext(r), &companyv1.LogoutRequest{RefreshToken: refreshToken})
	h.clearRefreshCookie(w)
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetInviteByToken(w http.ResponseWriter, r *http.Request, token api.InviteToken) {
	response, err := h.company.GetInviteByToken(outgoingContext(r), &companyv1.GetInviteByTokenRequest{Token: token})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := inviteFromProto(response.GetInvite())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) AcceptInvite(w http.ResponseWriter, r *http.Request, token api.InviteToken) {
	var input api.AcceptInviteInput
	if !decode(w, r, &input) {
		return
	}
	var email *string
	if input.Email != nil {
		value := string(*input.Email)
		email = &value
	}
	response, err := h.company.AcceptInvite(outgoingContext(r), &companyv1.AcceptInviteRequest{
		Token: token, FirstName: input.FirstName, LastName: input.LastName,
		Password: input.Password, Email: email,
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	h.writeSession(w, r, http.StatusOK, response.GetSession())
}

func (h *Handler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	response, err := h.company.GetCurrentUser(outgoingContext(r), &companyv1.GetCurrentUserRequest{})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := userFromProto(response.GetUser())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) UpdateCurrentUser(w http.ResponseWriter, r *http.Request) {
	var input api.UpdateCurrentUserInput
	if !decode(w, r, &input) {
		return
	}
	phone, err := clearablePhoneString(input.Phone)
	if err != nil {
		apierror.Write(w, apierror.BadRequest("Некорректный номер телефона"))
		return
	}
	response, err := h.company.UpdateCurrentUser(outgoingContext(r), &companyv1.UpdateCurrentUserRequest{
		FirstName: input.FirstName, LastName: input.LastName, Phone: phone,
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := userFromProto(response.GetUser())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetCompany(w http.ResponseWriter, r *http.Request) {
	response, err := h.company.GetCompany(outgoingContext(r), &companyv1.GetCompanyRequest{})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := companyFromProto(response.GetCompany())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) UpdateCompany(w http.ResponseWriter, r *http.Request) {
	var input api.UpdateCompanyInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.company.UpdateCompany(outgoingContext(r), &companyv1.UpdateCompanyRequest{
		Name: input.Name, LogoUrl: input.LogoUrl, AmoAccountId: input.AmoAccountId,
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := companyFromProto(response.GetCompany())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetDepartments(w http.ResponseWriter, r *http.Request) {
	response, err := h.company.GetDepartments(outgoingContext(r), &companyv1.GetDepartmentsRequest{})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := departmentsFromProto(response.GetDepartments())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) CreateDepartment(w http.ResponseWriter, r *http.Request) {
	var input api.CreateDepartmentInput
	if !decode(w, r, &input) {
		return
	}
	if !input.ParentId.IsSpecified() {
		apierror.Write(w, apierror.BadRequest("Поле parentId обязательно"))
		return
	}
	var parentID *string
	if !input.ParentId.IsNull() {
		value, err := input.ParentId.Get()
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректный parentId"))
			return
		}
		parentID = &value
	}
	response, err := h.company.CreateDepartment(outgoingContext(r), &companyv1.CreateDepartmentRequest{
		Name: input.Name, ParentId: parentID, HeadUserId: UUIDPointerString(input.HeadUserId),
		ValuableFinalProduct: input.ValuableFinalProduct,
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	h.writeDepartment(w, r, http.StatusCreated, response.GetDepartment())
}

func (h *Handler) UpdateDepartment(w http.ResponseWriter, r *http.Request, id api.Id) {
	var input api.UpdateDepartmentInput
	if !decode(w, r, &input) {
		return
	}
	request := &companyv1.UpdateDepartmentRequest{Id: id.String(), Name: input.Name}
	if input.HeadUserId.IsSpecified() {
		if input.HeadUserId.IsNull() {
			request.ClearHeadUserId = true
		} else {
			value, err := input.HeadUserId.Get()
			if err != nil {
				apierror.Write(w, apierror.BadRequest("Некорректный headUserId"))
				return
			}
			request.HeadUserId = &value
		}
	}
	if input.ValuableFinalProduct.IsSpecified() {
		if input.ValuableFinalProduct.IsNull() {
			request.ClearValuableFinalProduct = true
		} else {
			value, err := input.ValuableFinalProduct.Get()
			if err != nil {
				apierror.Write(w, apierror.BadRequest("Некорректный valuableFinalProduct"))
				return
			}
			request.ValuableFinalProduct = &value
		}
	}
	response, err := h.company.UpdateDepartment(outgoingContext(r), request)
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	h.writeDepartment(w, r, http.StatusOK, response.GetDepartment())
}

func (h *Handler) DeleteDepartment(w http.ResponseWriter, r *http.Request, id api.Id) {
	_, err := h.company.DeleteDepartment(outgoingContext(r), &companyv1.DeleteDepartmentRequest{Id: id.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MoveDepartment(w http.ResponseWriter, r *http.Request, id api.Id) {
	var input api.MoveDepartmentInput
	if !decode(w, r, &input) {
		return
	}
	if !input.ParentId.IsSpecified() {
		apierror.Write(w, apierror.BadRequest("Поле parentId обязательно"))
		return
	}
	request := &companyv1.MoveDepartmentRequest{Id: id.String()}
	if !input.ParentId.IsNull() {
		value, err := input.ParentId.Get()
		if err != nil {
			apierror.Write(w, apierror.BadRequest("Некорректный parentId"))
			return
		}
		request.ParentId = &value
	}
	response, err := h.company.MoveDepartment(outgoingContext(r), request)
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	h.writeDepartment(w, r, http.StatusOK, response.GetDepartment())
}

func (h *Handler) GetPositions(w http.ResponseWriter, r *http.Request) {
	response, err := h.company.GetPositions(outgoingContext(r), &companyv1.GetPositionsRequest{})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := positionsFromProto(response.GetPositions())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetPosition(w http.ResponseWriter, r *http.Request, id api.Id) {
	response, err := h.company.GetPosition(outgoingContext(r), &companyv1.GetPositionRequest{Id: id.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	h.writePosition(w, r, http.StatusOK, response.GetPosition())
}

func (h *Handler) CreatePosition(w http.ResponseWriter, r *http.Request) {
	var input api.CreatePositionInput
	if !decode(w, r, &input) {
		return
	}
	var level *uint32
	if input.Level != nil {
		if *input.Level < 0 {
			apierror.Write(w, apierror.BadRequest("Уровень должности должен быть от 0 до 4"))
			return
		}
		value := uint32(*input.Level)
		level = &value
	}
	response, err := h.company.CreatePosition(outgoingContext(r), &companyv1.CreatePositionRequest{
		Name: input.Name, DepartmentId: input.DepartmentId.String(), Level: level,
		Description: input.Description,
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	h.writePosition(w, r, http.StatusCreated, response.GetPosition())
}

func (h *Handler) UpdatePosition(w http.ResponseWriter, r *http.Request, id api.Id) {
	var input api.UpdatePositionInput
	if !decode(w, r, &input) {
		return
	}
	var level *uint32
	if input.Level != nil {
		if *input.Level < 0 {
			apierror.Write(w, apierror.BadRequest("Уровень должности должен быть от 0 до 4"))
			return
		}
		value := uint32(*input.Level)
		level = &value
	}
	response, err := h.company.UpdatePosition(outgoingContext(r), &companyv1.UpdatePositionRequest{
		Id: id.String(), Name: input.Name, DepartmentId: UUIDPointerString(input.DepartmentId),
		Level: level, Description: input.Description,
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	h.writePosition(w, r, http.StatusOK, response.GetPosition())
}

func (h *Handler) DeletePosition(w http.ResponseWriter, r *http.Request, id api.Id) {
	_, err := h.company.DeletePosition(outgoingContext(r), &companyv1.DeletePositionRequest{Id: id.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MovePosition(w http.ResponseWriter, r *http.Request, id api.Id) {
	var input api.MovePositionInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.company.MovePosition(outgoingContext(r), &companyv1.MovePositionRequest{
		Id: id.String(), DepartmentId: input.DepartmentId.String(),
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	h.writePosition(w, r, http.StatusOK, response.GetPosition())
}

func (h *Handler) GetUsers(w http.ResponseWriter, r *http.Request) {
	response, err := h.company.GetUsers(outgoingContext(r), &companyv1.GetUsersRequest{})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := usersFromProto(response.GetUsers())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request, id api.Id) {
	response, err := h.company.GetUser(outgoingContext(r), &companyv1.GetUserRequest{Id: id.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := userFromProto(response.GetUser())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetUserAccess(w http.ResponseWriter, r *http.Request, id api.Id) {
	response, err := h.company.GetUserAccess(outgoingContext(r), &companyv1.GetUserAccessRequest{Id: id.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := employeeAccessFromProto(response.GetAccess())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	if converted.LinkToken != nil {
		linkURL := h.accessLinkURL(r, *converted.LinkToken)
		converted.LinkUrl = &linkURL
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) SetUserPasswordAccess(w http.ResponseWriter, r *http.Request, id api.Id) {
	var input api.SetUserPasswordAccessInput
	if r.ContentLength != 0 && !decode(w, r, &input) {
		return
	}
	response, err := h.company.SetUserPasswordAccess(outgoingContext(r), &companyv1.SetUserPasswordAccessRequest{
		Id: id.String(), Password: input.Password,
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, api.EmployeePasswordAccess{Password: response.GetPassword()})
}

func (h *Handler) SetUserLinkAccess(w http.ResponseWriter, r *http.Request, id api.Id) {
	response, err := h.company.SetUserLinkAccess(outgoingContext(r), &companyv1.SetUserLinkAccessRequest{Id: id.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	if response.GetCreatedAt() == nil || !response.GetCreatedAt().IsValid() {
		h.writeConversionError(w, r, errors.New("company returned invalid access link creation time"))
		return
	}
	writeJSON(w, http.StatusOK, api.EmployeeLinkAccess{
		Token: response.GetToken(), LinkUrl: h.accessLinkURL(r, response.GetToken()),
		CreatedAt: response.GetCreatedAt().AsTime(),
	})
}

func (h *Handler) accessLinkURL(r *http.Request, token string) string {
	if baseURL := strings.TrimRight(strings.TrimSpace(h.cookie.PublicAppURL), "/"); baseURL != "" {
		return baseURL + "/access/" + token
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	return scheme + "://" + r.Host + "/access/" + token
}

func (h *Handler) RevokeUserAccess(w http.ResponseWriter, r *http.Request, id api.Id) {
	_, err := h.company.RevokeUserAccess(outgoingContext(r), &companyv1.RevokeUserAccessRequest{Id: id.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var input api.CreateUserInput
	if !decode(w, r, &input) {
		return
	}
	positionIDs := []uuid.UUID{}
	if input.PositionIds != nil {
		positionIDs = *input.PositionIds
	}
	lastName := ""
	if input.LastName != nil {
		lastName = *input.LastName
	}
	response, err := h.company.CreateUser(outgoingContext(r), &companyv1.CreateUserRequest{
		FirstName: input.FirstName, LastName: lastName, Email: string(input.Email),
		Phone: input.Phone, Role: roleToProto(input.Role), PositionIds: stringsFromUUIDs(positionIDs),
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := userFromProto(response.GetUser())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request, id api.Id) {
	var input api.UpdateUserInput
	if !decode(w, r, &input) {
		return
	}
	request, err := updateUserRequest(id, input)
	if err != nil {
		apierror.Write(w, apierror.BadRequest(err.Error()))
		return
	}
	response, err := h.company.UpdateUser(outgoingContext(r), request)
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := userFromProto(response.GetUser())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func updateUserRequest(id api.Id, input api.UpdateUserInput) (*companyv1.UpdateUserRequest, error) {
	phone, err := clearablePhoneString(input.Phone)
	if err != nil {
		return nil, fmt.Errorf("Некорректный номер телефона")
	}
	birthDate, err := clearableDateString(input.BirthDate)
	if err != nil {
		return nil, fmt.Errorf("Некорректная дата рождения")
	}
	hiredAt, err := clearableDateString(input.HiredAt)
	if err != nil {
		return nil, fmt.Errorf("Некорректная дата выхода на работу")
	}
	var vacation *uint32
	if input.VacationAllowance != nil {
		if *input.VacationAllowance < 0 {
			return nil, fmt.Errorf("Норма отпуска не может быть отрицательной")
		}
		value := uint32(*input.VacationAllowance)
		vacation = &value
	}
	request := &companyv1.UpdateUserRequest{
		Id: id.String(), FirstName: input.FirstName, LastName: input.LastName, Phone: phone,
		BirthDate: birthDate, HiredAt: hiredAt, VacationAllowance: vacation,
	}
	if input.Role != nil {
		value := roleToProto(*input.Role)
		request.Role = &value
	}
	if input.Status != nil {
		value := statusToProto(*input.Status)
		request.Status = &value
	}
	if input.PositionIds != nil {
		request.UpdatePositionIds = true
		request.PositionIds = stringsFromUUIDs(*input.PositionIds)
	}
	return request, nil
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request, id api.Id) {
	_, err := h.company.DeleteUser(outgoingContext(r), &companyv1.DeleteUserRequest{Id: id.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetInvites(w http.ResponseWriter, r *http.Request) {
	response, err := h.company.GetInvites(outgoingContext(r), &companyv1.GetInvitesRequest{})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := invitesFromProto(response.GetInvites())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) InviteUser(w http.ResponseWriter, r *http.Request) {
	var input api.InviteUserInput
	if !decode(w, r, &input) {
		return
	}
	var email *string
	if input.Email != nil {
		value := string(*input.Email)
		email = &value
	}
	response, err := h.company.InviteUser(outgoingContext(r), &companyv1.InviteUserRequest{
		Email: email, Role: roleToProto(input.Role), PositionId: UUIDPointerString(input.PositionId),
		DepartmentId: UUIDPointerString(input.DepartmentId),
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := inviteFromProto(response.GetInvite())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, converted)
}

func (h *Handler) ResendInvite(w http.ResponseWriter, r *http.Request, id api.Id) {
	response, err := h.company.ResendInvite(outgoingContext(r), &companyv1.ResendInviteRequest{Id: id.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := inviteFromProto(response.GetInvite())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) RevokeInvite(w http.ResponseWriter, r *http.Request, id api.Id) {
	_, err := h.company.RevokeInvite(outgoingContext(r), &companyv1.RevokeInviteRequest{Id: id.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeSession(w http.ResponseWriter, r *http.Request, code int, session *companyv1.AuthSession) {
	if session == nil || session.GetRefreshToken() == "" || session.GetAccessToken() == "" {
		h.writeConversionError(w, r, errors.New("company returned an empty auth session"))
		return
	}
	user, err := userFromProto(session.GetUser())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if session.GetRefreshExpiresAt() != nil && session.GetRefreshExpiresAt().IsValid() {
		expiresAt = session.GetRefreshExpiresAt().AsTime()
	}
	http.SetCookie(w, &http.Cookie{
		Name: refreshCookieName, Value: session.GetRefreshToken(), Path: "/api/v1/auth",
		Expires: expiresAt, MaxAge: max(1, int(time.Until(expiresAt).Seconds())),
		HttpOnly: true, Secure: h.cookie.Secure, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, code, api.AuthResponse{AccessToken: session.GetAccessToken(), User: user})
}

func (h *Handler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: refreshCookieName, Value: "", Path: "/api/v1/auth",
		Expires: time.Unix(1, 0), MaxAge: -1, HttpOnly: true,
		Secure: h.cookie.Secure, SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) writeDepartment(w http.ResponseWriter, r *http.Request, code int, value *companyv1.Department) {
	converted, err := departmentFromProto(value)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, code, converted)
}

func (h *Handler) writePosition(w http.ResponseWriter, r *http.Request, code int, value *companyv1.Position) {
	converted, err := positionFromProto(value)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, code, converted)
}

func (h *Handler) writeConversionError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.ErrorContext(r.Context(), "invalid company response", "error", err)
	apierror.Write(w, apierror.Internal(err))
}

func (h *Handler) writeRPCError(w http.ResponseWriter, r *http.Request, err error) {
	grpcStatus, ok := status.FromError(err)
	if !ok {
		h.logger.ErrorContext(r.Context(), "company RPC failed", "error", err)
		apierror.Write(w, apierror.Internal(err))
		return
	}
	message := grpcStatus.Message()
	switch grpcStatus.Code() {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.OutOfRange:
		apierror.Write(w, apierror.BadRequest(message))
	case codes.Unauthenticated:
		apierror.Write(w, apierror.Unauthorized(message))
	case codes.PermissionDenied:
		apierror.Write(w, apierror.Forbidden(message))
	case codes.NotFound:
		apierror.Write(w, apierror.New(http.StatusNotFound, message))
	case codes.AlreadyExists, codes.Aborted:
		apierror.Write(w, apierror.Conflict(message))
	case codes.Unavailable, codes.DeadlineExceeded:
		h.logger.WarnContext(r.Context(), "company RPC unavailable", "code", grpcStatus.Code(), "error", err)
		apierror.Write(w, apierror.New(http.StatusServiceUnavailable, "Сервис временно недоступен"))
	default:
		h.logger.ErrorContext(r.Context(), "company RPC failed", "code", grpcStatus.Code(), "error", err)
		apierror.Write(w, apierror.Internal(err))
	}
}

func decode(w http.ResponseWriter, r *http.Request, destination any) bool {
	if err := httpx.DecodeJSON(w, r, destination, httpx.DefaultMaxBodyBytes); err != nil {
		apierror.Write(w, err)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	if err := httpx.WriteJSON(w, code, value); err != nil {
		// At this point a response might already be committed; middleware logs
		// the short write while the client receives a transport failure.
		return
	}
}

func outgoingContext(r *http.Request) context.Context {
	pairs := []string{"x-user-agent", r.UserAgent()}
	if token, ok := authmw.Token(r.Context()); ok {
		pairs = append(pairs, "authorization", "Bearer "+token)
	}
	if requestID := httpx.RequestIDFromContext(r.Context()); requestID != "" {
		pairs = append(pairs, "x-request-id", requestID)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		pairs = append(pairs, "x-client-ip", host)
	}
	return metadata.NewOutgoingContext(r.Context(), metadata.Pairs(pairs...))
}

func UUIDPointerString(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	result := value.String()
	return &result
}
