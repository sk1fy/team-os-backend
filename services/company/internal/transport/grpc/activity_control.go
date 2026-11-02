package grpc

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	dto "github.com/sk1fy/team-os-backend/services/company/internal/activitycontract"
)

func (s *Server) ActivityControl(ctx context.Context, r *v.ActivityControlRequest) (*v.ActivityControlResponse, error) {
	if r == nil {
		return nil, invalidRequest()
	}
	a, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	var expected uuid.UUID
	if r.Action != v.ActivityControlRequest_ACTION_CONNECTION {
		expected, err = uuid.Parse(r.BindingId)
		if err != nil || expected == uuid.Nil {
			return nil, invalidRequest()
		}
	}
	var out any
	switch r.Action {
	case v.ActivityControlRequest_ACTION_CONNECTION:
		out, err = s.application.ActivityConnection(ctx, a)
	case v.ActivityControlRequest_ACTION_PANELS:
		out, err = s.application.ActivityPanels(ctx, a, expected)
	case v.ActivityControlRequest_ACTION_EMPLOYEES:
		out, err = s.application.ActivityEmployees(ctx, a, expected)
	case v.ActivityControlRequest_ACTION_TASK_PANEL:
		out, err = s.application.ActivityTaskPanel(ctx, a, expected)
	case v.ActivityControlRequest_ACTION_CREATE:
		key, e := uuid.Parse(r.CommandId)
		if e != nil || key == uuid.Nil {
			return nil, invalidRequest()
		}
		var in dto.ActivityPanelInput
		if taskControlDecode(r.JsonBody, &in) != nil {
			return nil, invalidRequest()
		}
		out, err = s.application.CreateActivityPanel(ctx, a, expected, key, in)
	case v.ActivityControlRequest_ACTION_UPDATE:
		id, e := uuid.Parse(r.PanelId)
		if e != nil || id == uuid.Nil {
			return nil, invalidRequest()
		}
		var in dto.ActivityPanelPatch
		if taskControlDecode(r.JsonBody, &in) != nil {
			return nil, invalidRequest()
		}
		out, err = s.application.UpdateActivityPanel(ctx, a, expected, id, in)
	case v.ActivityControlRequest_ACTION_SHARE:
		id, e := uuid.Parse(r.PanelId)
		if e != nil || id == uuid.Nil {
			return nil, invalidRequest()
		}
		key, e := uuid.Parse(r.CommandId)
		if e != nil || key == uuid.Nil {
			return nil, invalidRequest()
		}
		out, err = s.application.ShareActivityPanel(ctx, a, expected, id, key)
	case v.ActivityControlRequest_ACTION_SAVE_TASK_PANEL:
		var in dto.ActivityTaskPanel
		if taskControlDecode(r.JsonBody, &in) != nil {
			return nil, invalidRequest()
		}
		out, err = s.application.SaveActivityTaskPanel(ctx, a, expected, in)
	default:
		return nil, invalidRequest()
	}
	if err != nil {
		return nil, transportError(err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, transportError(err)
	}
	return &v.ActivityControlResponse{JsonBody: raw}, nil
}
