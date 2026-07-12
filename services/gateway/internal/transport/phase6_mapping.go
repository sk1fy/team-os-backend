package transport

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func scheduleTemplateToProto(value api.ScheduleTemplate) (*companyv1.ScheduleTemplate, error) {
	discriminator, err := value.Discriminator()
	if err != nil {
		return nil, err
	}
	switch discriminator {
	case "week":
		template, conversionErr := value.AsWeekTemplate()
		if conversionErr != nil {
			return nil, conversionErr
		}
		days := make([]uint32, len(template.Days))
		for i, day := range template.Days {
			days[i] = uint32(day)
		}
		return &companyv1.ScheduleTemplate{Type: "week", Days: days, Start: template.Start, End: template.End}, nil
	case "cycle":
		template, conversionErr := value.AsCycleTemplate()
		if conversionErr != nil {
			return nil, conversionErr
		}
		on, off, start := uint32(template.On), uint32(template.Off), template.CycleStart.Format(time.DateOnly)
		return &companyv1.ScheduleTemplate{Type: "cycle", On: &on, Off: &off, Start: template.Start, End: template.End, CycleStart: &start}, nil
	default:
		return nil, fmt.Errorf("неизвестный тип шаблона графика")
	}
}
func scheduleTemplateFromProto(value *companyv1.ScheduleTemplate) (api.ScheduleTemplate, error) {
	if value == nil {
		return api.ScheduleTemplate{}, fmt.Errorf("company вернул пустой шаблон графика")
	}
	var result api.ScheduleTemplate
	if value.Type == "week" {
		days := make([]int, len(value.Days))
		for i, day := range value.Days {
			days[i] = int(day)
		}
		if err := result.FromWeekTemplate(api.WeekTemplate{Type: "week", Days: days, Start: value.Start, End: value.End}); err != nil {
			return result, err
		}
		return result, nil
	}
	if value.Type == "cycle" {
		if value.On == nil || value.Off == nil || value.CycleStart == nil {
			return result, fmt.Errorf("company вернул неполный циклический график")
		}
		date, err := time.Parse(time.DateOnly, *value.CycleStart)
		if err != nil {
			return result, err
		}
		if err = result.FromCycleTemplate(api.CycleTemplate{Type: "cycle", On: int(*value.On), Off: int(*value.Off), Start: value.Start, End: value.End, CycleStart: openapi_types.Date{Time: date}}); err != nil {
			return result, err
		}
		return result, nil
	}
	return result, fmt.Errorf("company вернул неизвестный тип графика")
}
func scheduleFromProto(value *companyv1.UserSchedule) (api.UserSchedule, error) {
	if value == nil {
		return api.UserSchedule{}, fmt.Errorf("company вернул пустой график")
	}
	id, err := uuid.Parse(value.UserId)
	if err != nil {
		return api.UserSchedule{}, err
	}
	template, err := scheduleTemplateFromProto(value.Template)
	if err != nil {
		return api.UserSchedule{}, err
	}
	return api.UserSchedule{UserId: id, Template: template}, nil
}
func schedulesFromProto(values []*companyv1.UserSchedule) ([]api.UserSchedule, error) {
	result := make([]api.UserSchedule, len(values))
	for i, value := range values {
		converted, err := scheduleFromProto(value)
		if err != nil {
			return nil, err
		}
		result[i] = converted
	}
	return result, nil
}
func shiftTypeToProto(value api.ShiftType) (companyv1.ShiftType, error) {
	switch value {
	case api.Work:
		return companyv1.ShiftType_SHIFT_TYPE_WORK, nil
	case api.Off:
		return companyv1.ShiftType_SHIFT_TYPE_OFF, nil
	case api.Vacation:
		return companyv1.ShiftType_SHIFT_TYPE_VACATION, nil
	case api.Sick:
		return companyv1.ShiftType_SHIFT_TYPE_SICK, nil
	case api.Trip:
		return companyv1.ShiftType_SHIFT_TYPE_TRIP, nil
	default:
		return 0, fmt.Errorf("неизвестный тип смены")
	}
}
func shiftTypeFromProto(value companyv1.ShiftType) (api.ShiftType, error) {
	switch value {
	case companyv1.ShiftType_SHIFT_TYPE_WORK:
		return api.Work, nil
	case companyv1.ShiftType_SHIFT_TYPE_OFF:
		return api.Off, nil
	case companyv1.ShiftType_SHIFT_TYPE_VACATION:
		return api.Vacation, nil
	case companyv1.ShiftType_SHIFT_TYPE_SICK:
		return api.Sick, nil
	case companyv1.ShiftType_SHIFT_TYPE_TRIP:
		return api.Trip, nil
	default:
		return "", fmt.Errorf("company вернул неизвестный тип смены")
	}
}
func exceptionFromProto(value *companyv1.ShiftException) (api.ShiftException, error) {
	if value == nil {
		return api.ShiftException{}, fmt.Errorf("company вернул пустое изменение графика")
	}
	id, err := uuid.Parse(value.Id)
	if err != nil {
		return api.ShiftException{}, err
	}
	userID, err := uuid.Parse(value.UserId)
	if err != nil {
		return api.ShiftException{}, err
	}
	date, err := time.Parse(time.DateOnly, value.Date)
	if err != nil {
		return api.ShiftException{}, err
	}
	shiftType, err := shiftTypeFromProto(value.Type)
	if err != nil {
		return api.ShiftException{}, err
	}
	return api.ShiftException{Id: id, UserId: userID, Date: openapi_types.Date{Time: date}, Type: shiftType, Start: value.Start, End: value.End, Note: value.Note}, nil
}
func exceptionsFromProto(values []*companyv1.ShiftException) ([]api.ShiftException, error) {
	result := make([]api.ShiftException, len(values))
	for i, value := range values {
		converted, err := exceptionFromProto(value)
		if err != nil {
			return nil, err
		}
		result[i] = converted
	}
	return result, nil
}
func distributionAlgorithmToProto(value api.DistributionAlgorithm) (companyv1.DistributionAlgorithm, error) {
	switch value {
	case api.RoundRobin:
		return companyv1.DistributionAlgorithm_DISTRIBUTION_ALGORITHM_ROUND_ROBIN, nil
	case api.LeastLoaded:
		return companyv1.DistributionAlgorithm_DISTRIBUTION_ALGORITHM_LEAST_LOADED, nil
	case api.Priority:
		return companyv1.DistributionAlgorithm_DISTRIBUTION_ALGORITHM_PRIORITY, nil
	default:
		return 0, fmt.Errorf("неизвестный алгоритм распределения")
	}
}
func distributionAlgorithmFromProto(value companyv1.DistributionAlgorithm) (api.DistributionAlgorithm, error) {
	switch value {
	case companyv1.DistributionAlgorithm_DISTRIBUTION_ALGORITHM_ROUND_ROBIN:
		return api.RoundRobin, nil
	case companyv1.DistributionAlgorithm_DISTRIBUTION_ALGORITHM_LEAST_LOADED:
		return api.LeastLoaded, nil
	case companyv1.DistributionAlgorithm_DISTRIBUTION_ALGORITHM_PRIORITY:
		return api.Priority, nil
	default:
		return "", fmt.Errorf("company вернул неизвестный алгоритм распределения")
	}
}
func distributionGroupFromProto(value *companyv1.DistributionGroup) (api.DealDistributionGroup, error) {
	if value == nil {
		return api.DealDistributionGroup{}, fmt.Errorf("company вернул пустую группу распределения")
	}
	id, err := uuid.Parse(value.Id)
	if err != nil {
		return api.DealDistributionGroup{}, err
	}
	members, err := UUIDsFromStrings(value.MemberIds)
	if err != nil {
		return api.DealDistributionGroup{}, err
	}
	disabled, err := UUIDsFromStrings(value.DisabledMemberIds)
	if err != nil {
		return api.DealDistributionGroup{}, err
	}
	algorithm, err := distributionAlgorithmFromProto(value.Algorithm)
	if err != nil {
		return api.DealDistributionGroup{}, err
	}
	created := time.Time{}
	if value.CreatedAt != nil {
		created = value.CreatedAt.AsTime()
	}
	return api.DealDistributionGroup{Id: id, Name: value.Name, Description: value.Description, Active: value.Active, Algorithm: algorithm, MemberIds: members, DisabledMemberIds: disabled, Source: value.Source, DealLimit: int(value.DealLimit), UnclaimedMinutes: int(value.UnclaimedMinutes), CreatedAt: created}, nil
}
func distributionGroupsFromProto(values []*companyv1.DistributionGroup) ([]api.DealDistributionGroup, error) {
	result := make([]api.DealDistributionGroup, len(values))
	for i, value := range values {
		converted, err := distributionGroupFromProto(value)
		if err != nil {
			return nil, err
		}
		result[i] = converted
	}
	return result, nil
}
func distributionEventStatusFromProto(value companyv1.DistributionEventStatus) (api.DistributionEventStatus, error) {
	switch value {
	case companyv1.DistributionEventStatus_DISTRIBUTION_EVENT_STATUS_ACCEPTED:
		return api.DistributionEventStatusAccepted, nil
	case companyv1.DistributionEventStatus_DISTRIBUTION_EVENT_STATUS_IN_PROGRESS:
		return api.DistributionEventStatusInProgress, nil
	case companyv1.DistributionEventStatus_DISTRIBUTION_EVENT_STATUS_REASSIGNED:
		return api.DistributionEventStatusReassigned, nil
	case companyv1.DistributionEventStatus_DISTRIBUTION_EVENT_STATUS_DECLINED:
		return api.DistributionEventStatusDeclined, nil
	default:
		return "", fmt.Errorf("company вернул неизвестный статус распределения")
	}
}
func distributionEventFromProto(value *companyv1.DistributionEvent) (api.DistributionEvent, error) {
	if value == nil {
		return api.DistributionEvent{}, fmt.Errorf("company вернул пустое событие распределения")
	}
	id, err := uuid.Parse(value.Id)
	if err != nil {
		return api.DistributionEvent{}, err
	}
	groupID, err := uuid.Parse(value.GroupId)
	if err != nil {
		return api.DistributionEvent{}, err
	}
	userID, err := uuid.Parse(value.UserId)
	if err != nil {
		return api.DistributionEvent{}, err
	}
	status, err := distributionEventStatusFromProto(value.Status)
	if err != nil {
		return api.DistributionEvent{}, err
	}
	created := time.Time{}
	if value.CreatedAt != nil {
		created = value.CreatedAt.AsTime()
	}
	return api.DistributionEvent{Id: id, GroupId: groupID, UserId: userID, DealNumber: int(value.DealNumber), Status: status, CreatedAt: created}, nil
}
func distributionEventsFromProto(values []*companyv1.DistributionEvent) ([]api.DistributionEvent, error) {
	result := make([]api.DistributionEvent, len(values))
	for i, value := range values {
		converted, err := distributionEventFromProto(value)
		if err != nil {
			return nil, err
		}
		result[i] = converted
	}
	return result, nil
}
