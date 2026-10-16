package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/google/uuid"
	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func taskControlDecode(raw []byte, out any) error {
	if len(raw) > 1048576 {
		return invalidRequest()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return invalidRequest()
	}
	return nil
}
func (s *Server) TaskControlConnection(ctx context.Context, r *v.TaskControlConnectionRequest) (*v.TaskControlConnectionResponse, error) {
	if r == nil {
		return nil, invalidRequest()
	}
	var a application.Actor
	var err error
	runtime := r.Action == v.TaskControlConnectionRequest_ACTION_EXCHANGE || r.Action == v.TaskControlConnectionRequest_ACTION_BOOTSTRAP
	if runtime {
		md, _ := metadata.FromIncomingContext(ctx)
		if len(md.Get("x-teamos-service")) != 1 || md.Get("x-teamos-service")[0] != "task-control" || len(md.Get("x-teamos-service-token")) != 1 || !secureServiceTokenEqual(md.Get("x-teamos-service-token")[0], s.gatewayServiceToken) {
			return nil, status.Error(codes.Unauthenticated, "Требуется служебная авторизация")
		}
	} else {
		a, err = s.actor(ctx)
		if err != nil {
			return nil, err
		}
	}
	var out any
	switch r.Action {
	case v.TaskControlConnectionRequest_ACTION_CAPABILITIES:
		out, err = s.application.GetTaskControlCapabilities(ctx, a)
	case v.TaskControlConnectionRequest_ACTION_AUTHORIZE:
		id, e := uuid.Parse(r.Id)
		if e != nil {
			return nil, invalidRequest()
		}
		err = s.application.AuthorizeTaskControlOperation(ctx, a, id, r.Operation)
		out = map[string]bool{"authorized": err == nil}
	case v.TaskControlConnectionRequest_ACTION_INSTALLATION:
		out, err = s.application.GetTaskControlInstallation(ctx, a, r.AccountId)
	case v.TaskControlConnectionRequest_ACTION_LIST:
		out, err = s.application.ListTaskControlConnections(ctx, a)
	case v.TaskControlConnectionRequest_ACTION_LINK:
		var input application.TaskControlLinkInput
		if e := taskControlDecode(r.JsonBody, &input); e != nil {
			return nil, e
		}
		out, err = s.application.LinkTaskControl(ctx, a, input)
	case v.TaskControlConnectionRequest_ACTION_REVOKE:
		id, e := uuid.Parse(r.Id)
		if e != nil {
			return nil, invalidRequest()
		}
		var input application.TaskControlRevokeInput
		if e = taskControlDecode(r.JsonBody, &input); e != nil {
			return nil, e
		}
		out, err = s.application.RevokeTaskControl(ctx, a, id, input)
	case v.TaskControlConnectionRequest_ACTION_INTENT:
		id, e := uuid.Parse(r.Id)
		if e != nil {
			return nil, invalidRequest()
		}
		out, err = s.application.GetTaskControlIntent(ctx, a, id)
	case v.TaskControlConnectionRequest_ACTION_EXCHANGE:
		out, err = s.application.ExchangeTaskControlSession(ctx, r.Credential)
	case v.TaskControlConnectionRequest_ACTION_BOOTSTRAP:
		out, err = s.application.TaskControlBootstrap(ctx, r.Credential)
	case v.TaskControlConnectionRequest_ACTION_DRAFT, v.TaskControlConnectionRequest_ACTION_UPDATE_DRAFT, v.TaskControlConnectionRequest_ACTION_VALIDATE, v.TaskControlConnectionRequest_ACTION_PUBLISH, v.TaskControlConnectionRequest_ACTION_PUBLICATIONS, v.TaskControlConnectionRequest_ACTION_PUBLICATION, v.TaskControlConnectionRequest_ACTION_MODE:
		id, e := uuid.Parse(r.Id)
		if e != nil {
			return nil, invalidRequest()
		}
		switch r.Action {
		case v.TaskControlConnectionRequest_ACTION_DRAFT:
			out, err = s.application.GetTaskControlDraft(ctx, a, id)
		case v.TaskControlConnectionRequest_ACTION_UPDATE_DRAFT:
			var input application.TaskControlDraftInput
			if e = taskControlDecode(r.JsonBody, &input); e != nil {
				return nil, e
			}
			out, err = s.application.UpdateTaskControlDraft(ctx, a, id, input)
		case v.TaskControlConnectionRequest_ACTION_VALIDATE:
			out, err = s.application.ValidateTaskControlDraft(ctx, a, id)
		case v.TaskControlConnectionRequest_ACTION_PUBLISH:
			var input application.TaskControlPublishInput
			if e = taskControlDecode(r.JsonBody, &input); e != nil {
				return nil, e
			}
			out, err = s.application.PublishTaskControlDraft(ctx, a, id, input)
		case v.TaskControlConnectionRequest_ACTION_PUBLICATIONS:
			out, err = s.application.TaskControlPublications(ctx, a, id, uuid.Nil)
		case v.TaskControlConnectionRequest_ACTION_PUBLICATION:
			pid, e := uuid.Parse(r.AccountId)
			if e != nil {
				return nil, invalidRequest()
			}
			out, err = s.application.TaskControlPublications(ctx, a, id, pid)
		case v.TaskControlConnectionRequest_ACTION_MODE:
			var input application.TaskControlModeInput
			if e = taskControlDecode(r.JsonBody, &input); e != nil {
				return nil, e
			}
			out, err = s.application.SetTaskControlMode(ctx, a, id, input)
		}

	default:
		return nil, invalidRequest()
	}
	if err != nil {
		return nil, transportError(application.TaskControlError(err))
	}
	raw, e := json.Marshal(out)
	if e != nil {
		return nil, status.Error(codes.Internal, "Не удалось подготовить ответ")
	}
	return &v.TaskControlConnectionResponse{JsonBody: raw}, nil
}
