package transport

import (
	"net/http"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/pkg/apierror"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func (h *Handler) GetSchedules(w http.ResponseWriter, r *http.Request) {
	response, err := h.company.GetSchedules(outgoingContext(r), &companyv1.GetSchedulesRequest{})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	values, err := schedulesFromProto(response.Schedules)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, values)
}

func (h *Handler) SaveSchedule(w http.ResponseWriter, r *http.Request, userID api.ID) {
	var input api.SaveScheduleInput
	if !decode(w, r, &input) {
		return
	}
	template, err := scheduleTemplateToProto(input.Template)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	response, err := h.company.SaveSchedule(outgoingContext(r), &companyv1.SaveScheduleRequest{UserId: userID.String(), Template: template})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	value, err := scheduleFromProto(response.Schedule)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h *Handler) UpdateUserCard(w http.ResponseWriter, r *http.Request, id api.ID) {
	var input api.UpdateUserCardInput
	if !decode(w, r, &input) {
		return
	}
	user, err := updateUserRequest(id, input.User)
	if err != nil {
		apierror.Write(w, apierror.BadRequest(err.Error()))
		return
	}
	template, err := scheduleTemplateToProto(input.Schedule.Template)
	if err != nil {
		apierror.Write(w, apierror.BadRequest("Некорректный шаблон графика"))
		return
	}
	response, err := h.company.UpdateUserCard(outgoingContext(r), &companyv1.UpdateUserCardRequest{
		User:     user,
		Template: template,
	})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	convertedUser, err := userFromProto(response.GetUser())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	convertedSchedule, err := scheduleFromProto(response.GetSchedule())
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, api.UserCard{User: convertedUser, Schedule: convertedSchedule})
}

func (h *Handler) GetExceptions(w http.ResponseWriter, r *http.Request, params api.GetExceptionsParams) {
	response, err := h.company.GetShiftExceptions(outgoingContext(r), &companyv1.GetShiftExceptionsRequest{Month: string(params.Month)})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	values, err := exceptionsFromProto(response.Exceptions)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, values)
}

func (h *Handler) SaveExceptions(w http.ResponseWriter, r *http.Request) {
	var input []api.SaveShiftExceptionInput
	if !decode(w, r, &input) {
		return
	}
	values := make([]*companyv1.SaveShiftExceptionInput, len(input))
	for i, value := range input {
		convertedType, err := shiftTypeToProto(value.Type)
		if err != nil {
			h.writeConversionError(w, r, err)
			return
		}
		date := value.Date.Format("2006-01-02")
		values[i] = &companyv1.SaveShiftExceptionInput{UserId: value.UserId.String(), Date: date, Type: convertedType, Start: value.Start, End: value.End, Note: value.Note}
	}
	response, err := h.company.SaveShiftExceptions(outgoingContext(r), &companyv1.SaveShiftExceptionsRequest{Exceptions: values})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	converted, err := exceptionsFromProto(response.Exceptions)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, converted)
}

func (h *Handler) GetDistributionGroups(w http.ResponseWriter, r *http.Request) {
	response, err := h.company.GetDistributionGroups(outgoingContext(r), &companyv1.GetDistributionGroupsRequest{})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	values, err := distributionGroupsFromProto(response.Groups)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, values)
}

func (h *Handler) CreateDistributionGroup(w http.ResponseWriter, r *http.Request) {
	var input api.CreateDistributionGroupInput
	if !decode(w, r, &input) {
		return
	}
	response, err := h.company.CreateDistributionGroup(outgoingContext(r), &companyv1.CreateDistributionGroupRequest{Name: input.Name, Description: input.Description, MemberIds: stringsFromUUIDs(input.MemberIds)})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	value, err := distributionGroupFromProto(response.Group)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (h *Handler) UpdateDistributionGroup(w http.ResponseWriter, r *http.Request, id api.ID) {
	var input api.UpdateDistributionGroupInput
	if !decode(w, r, &input) {
		return
	}
	request := &companyv1.UpdateDistributionGroupRequest{Id: id.String(), Name: input.Name, Description: input.Description, Active: input.Active, Source: input.Source}
	if input.Algorithm != nil {
		value, err := distributionAlgorithmToProto(*input.Algorithm)
		if err != nil {
			h.writeConversionError(w, r, err)
			return
		}
		request.Algorithm = &value
	}
	if input.MemberIds != nil {
		request.SetMemberIds, request.MemberIds = true, stringsFromUUIDs(*input.MemberIds)
	}
	if input.DisabledMemberIds != nil {
		request.SetDisabledMemberIds, request.DisabledMemberIds = true, stringsFromUUIDs(*input.DisabledMemberIds)
	}
	if input.DealLimit != nil {
		value := uint32(*input.DealLimit)
		request.DealLimit = &value
	}
	if input.UnclaimedMinutes != nil {
		value := uint32(*input.UnclaimedMinutes)
		request.UnclaimedMinutes = &value
	}
	response, err := h.company.UpdateDistributionGroup(outgoingContext(r), request)
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	value, err := distributionGroupFromProto(response.Group)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h *Handler) DeleteDistributionGroup(w http.ResponseWriter, r *http.Request, id api.ID) {
	_, err := h.company.DeleteDistributionGroup(outgoingContext(r), &companyv1.DeleteDistributionGroupRequest{Id: id.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) GetDistributionEvents(w http.ResponseWriter, r *http.Request, groupID api.ID) {
	response, err := h.company.GetDistributionEvents(outgoingContext(r), &companyv1.GetDistributionEventsRequest{GroupId: groupID.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	values, err := distributionEventsFromProto(response.Events)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, values)
}
func (h *Handler) SimulateDistributionDeal(w http.ResponseWriter, r *http.Request, groupID api.ID) {
	response, err := h.company.SimulateDistributionDeal(outgoingContext(r), &companyv1.SimulateDistributionDealRequest{GroupId: groupID.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	value, err := distributionEventFromProto(response.Event)
	if err != nil {
		h.writeConversionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (h *Handler) ResetDistributionEvents(w http.ResponseWriter, r *http.Request, groupID api.ID) {
	_, err := h.company.ResetDistributionEvents(outgoingContext(r), &companyv1.ResetDistributionEventsRequest{GroupId: groupID.String()})
	if err != nil {
		h.writeRPCError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
