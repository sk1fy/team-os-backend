package grpc

import (
	"context"

	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
)

func (s *Server) GetSchedules(ctx context.Context, request *companyv1.GetSchedulesRequest) (*companyv1.GetSchedulesResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	values, err := s.application.ListSchedules(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetSchedulesResponse{Schedules: schedulesToProto(values)}, nil
}

func (s *Server) SaveSchedule(ctx context.Context, request *companyv1.SaveScheduleRequest) (*companyv1.SaveScheduleResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil || request.Template == nil {
		return nil, invalidRequest()
	}
	userID, err := parseUUID(request.UserId, "сотрудника")
	if err != nil {
		return nil, err
	}
	template, err := scheduleTemplateFromProto(request.Template)
	if err != nil {
		return nil, err
	}
	value, err := s.application.SaveSchedule(ctx, actor, userID, template)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.SaveScheduleResponse{Schedule: scheduleToProto(value)}, nil
}

func (s *Server) UpdateUserCard(ctx context.Context, request *companyv1.UpdateUserCardRequest) (*companyv1.UpdateUserCardResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil || request.User == nil || request.Template == nil {
		return nil, invalidRequest()
	}
	user, err := updateUserInputFromProto(request.User)
	if err != nil {
		return nil, err
	}
	template, err := scheduleTemplateFromProto(request.Template)
	if err != nil {
		return nil, err
	}
	value, err := s.application.UpdateUserCard(ctx, actor, application.UpdateUserCardInput{
		User:     user,
		Schedule: template,
	})
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.UpdateUserCardResponse{
		User:     userToProto(value.User),
		Schedule: scheduleToProto(value.Schedule),
	}, nil
}

func (s *Server) GetShiftExceptions(ctx context.Context, request *companyv1.GetShiftExceptionsRequest) (*companyv1.GetShiftExceptionsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	values, err := s.application.ListShiftExceptions(ctx, actor, request.Month)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetShiftExceptionsResponse{Exceptions: exceptionsToProto(values)}, nil
}

func (s *Server) SaveShiftExceptions(ctx context.Context, request *companyv1.SaveShiftExceptionsRequest) (*companyv1.SaveShiftExceptionsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	inputs := make([]application.SaveShiftExceptionInput, len(request.Exceptions))
	for index, value := range request.Exceptions {
		if value == nil {
			return nil, invalidRequest()
		}
		userID, parseErr := parseUUID(value.UserId, "сотрудника")
		if parseErr != nil {
			return nil, parseErr
		}
		shiftType, parseErr := shiftTypeFromProto(value.Type)
		if parseErr != nil {
			return nil, parseErr
		}
		inputs[index] = application.SaveShiftExceptionInput{UserID: userID, Date: value.Date, Type: shiftType, Start: cloneString(value.Start), End: cloneString(value.End), Note: cloneString(value.Note)}
	}
	values, err := s.application.SaveShiftExceptions(ctx, actor, inputs)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.SaveShiftExceptionsResponse{Exceptions: exceptionsToProto(values)}, nil
}

func (s *Server) GetDistributionGroups(ctx context.Context, request *companyv1.GetDistributionGroupsRequest) (*companyv1.GetDistributionGroupsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	values, err := s.application.ListDistributionGroups(ctx, actor)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetDistributionGroupsResponse{Groups: distributionGroupsToProto(values)}, nil
}

func (s *Server) CreateDistributionGroup(ctx context.Context, request *companyv1.CreateDistributionGroupRequest) (*companyv1.CreateDistributionGroupResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	members, err := parseUUIDs(request.MemberIds, "сотрудника")
	if err != nil {
		return nil, err
	}
	value, err := s.application.CreateDistributionGroup(ctx, actor, application.CreateDistributionGroupInput{Name: request.Name, Description: cloneString(request.Description), MemberIDs: members})
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.CreateDistributionGroupResponse{Group: distributionGroupToProto(value)}, nil
}

func (s *Server) UpdateDistributionGroup(ctx context.Context, request *companyv1.UpdateDistributionGroupRequest) (*companyv1.UpdateDistributionGroupResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.Id, "группы распределения")
	if err != nil {
		return nil, err
	}
	members, err := parseUUIDs(request.MemberIds, "сотрудника")
	if err != nil {
		return nil, err
	}
	disabled, err := parseUUIDs(request.DisabledMemberIds, "сотрудника")
	if err != nil {
		return nil, err
	}
	var algorithm *string
	if request.Algorithm != nil {
		converted, conversionErr := distributionAlgorithmFromProto(*request.Algorithm)
		if conversionErr != nil {
			return nil, conversionErr
		}
		algorithm = &converted
	}
	var dealLimit, unclaimed *int32
	if request.DealLimit != nil {
		value := int32(*request.DealLimit)
		dealLimit = &value
	}
	if request.UnclaimedMinutes != nil {
		value := int32(*request.UnclaimedMinutes)
		unclaimed = &value
	}
	description := cloneString(request.Description)
	if request.ClearDescription {
		description = nil
	}
	value, err := s.application.UpdateDistributionGroup(ctx, actor, application.UpdateDistributionGroupInput{ID: id, Name: cloneString(request.Name), SetDescription: request.Description != nil || request.ClearDescription, Description: description, Active: request.Active, Algorithm: algorithm, SetMemberIDs: request.SetMemberIds, MemberIDs: members, SetDisabledMemberIDs: request.SetDisabledMemberIds, DisabledMemberIDs: disabled, Source: cloneString(request.Source), DealLimit: dealLimit, UnclaimedMinutes: unclaimed})
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.UpdateDistributionGroupResponse{Group: distributionGroupToProto(value)}, nil
}

func (s *Server) DeleteDistributionGroup(ctx context.Context, request *companyv1.DeleteDistributionGroupRequest) (*companyv1.DeleteDistributionGroupResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.Id, "группы распределения")
	if err != nil {
		return nil, err
	}
	if err = s.application.DeleteDistributionGroup(ctx, actor, id); err != nil {
		return nil, transportError(err)
	}
	return &companyv1.DeleteDistributionGroupResponse{}, nil
}
func (s *Server) GetDistributionEvents(ctx context.Context, request *companyv1.GetDistributionEventsRequest) (*companyv1.GetDistributionEventsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.GroupId, "группы распределения")
	if err != nil {
		return nil, err
	}
	values, err := s.application.ListDistributionEvents(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.GetDistributionEventsResponse{Events: distributionEventsToProto(values)}, nil
}
func (s *Server) SimulateDistributionDeal(ctx context.Context, request *companyv1.SimulateDistributionDealRequest) (*companyv1.SimulateDistributionDealResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.GroupId, "группы распределения")
	if err != nil {
		return nil, err
	}
	value, err := s.application.SimulateDistributionDeal(ctx, actor, id)
	if err != nil {
		return nil, transportError(err)
	}
	return &companyv1.SimulateDistributionDealResponse{Event: distributionEventToProto(value)}, nil
}
func (s *Server) ResetDistributionEvents(ctx context.Context, request *companyv1.ResetDistributionEventsRequest) (*companyv1.ResetDistributionEventsResponse, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, invalidRequest()
	}
	id, err := parseUUID(request.GroupId, "группы распределения")
	if err != nil {
		return nil, err
	}
	if err = s.application.ResetDistributionEvents(ctx, actor, id); err != nil {
		return nil, transportError(err)
	}
	return &companyv1.ResetDistributionEventsResponse{}, nil
}
