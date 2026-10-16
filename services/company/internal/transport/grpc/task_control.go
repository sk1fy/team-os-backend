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
	runtime := r.Action == v.TaskControlConnectionRequest_ACTION_ACK || r.Action == v.TaskControlConnectionRequest_ACTION_TELEMETRY || r.Action == v.TaskControlConnectionRequest_ACTION_RUNTIME_CONNECTION || r.Action == v.TaskControlConnectionRequest_ACTION_SOURCE_PENDING || r.Action == v.TaskControlConnectionRequest_ACTION_SOURCE_SUBMIT || r.Action == v.TaskControlConnectionRequest_ACTION_EXCHANGE || (r.Action == v.TaskControlConnectionRequest_ACTION_BOOTSTRAP || r.Action == v.TaskControlConnectionRequest_ACTION_BOOTSTRAP_V2 || r.Action == v.TaskControlConnectionRequest_ACTION_ACK_V2)
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
	case v.TaskControlConnectionRequest_ACTION_APPLICATION:
		id, e := uuid.Parse(r.Id)
		if e != nil {
			return nil, invalidRequest()
		}
		pid, e := uuid.Parse(r.AccountId)
		if e != nil {
			return nil, invalidRequest()
		}
		out, err = s.application.TaskControlApplication(ctx, a, id, pid)
	case v.TaskControlConnectionRequest_ACTION_ACK:
		var in application.TaskControlAckInputV2
		if len(r.JsonBody) > 8192 {
			return nil, invalidRequest()
		}
		if e := taskControlDecode(r.JsonBody, &in); e != nil {
			return nil, e
		}
		err = s.application.AckTaskControl(ctx, r.Credential, in)
		out = map[string]bool{"accepted": err == nil}
	case v.TaskControlConnectionRequest_ACTION_TELEMETRY:
		var in application.TaskControlTelemetryInput
		if len(r.JsonBody) > 8192 {
			return nil, invalidRequest()
		}
		if e := taskControlDecode(r.JsonBody, &in); e != nil {
			return nil, e
		}
		err = s.application.RecordTaskControlTelemetry(ctx, r.Credential, in)
		out = map[string]bool{"accepted": err == nil}
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

	case v.TaskControlConnectionRequest_ACTION_REFERENCES:
		id, e := uuid.Parse(r.Id)
		if e != nil {
			return nil, invalidRequest()
		}
		if r.Operation == "legacy" {
			out, err = s.application.TaskControlLegacyReferences(ctx, a, id)
		} else {
			var opts application.TaskControlReferenceOptions
			if len(r.JsonBody) > 0 {
				if e = taskControlDecode(r.JsonBody, &opts); e != nil {
					return nil, e
				}
			}
			out, err = s.application.GetTaskControlReferences(ctx, a, id, opts)
		}
	case v.TaskControlConnectionRequest_ACTION_REFRESH_REFERENCES:
		id, e := uuid.Parse(r.Id)
		if e != nil {
			return nil, invalidRequest()
		}
		out, err = s.application.RequestTaskControlReferencesRefresh(ctx, a, id)
	case v.TaskControlConnectionRequest_ACTION_REFERENCES_REFRESH:
		id, e := uuid.Parse(r.Id)
		refreshID, re := uuid.Parse(r.AccountId)
		if e != nil || re != nil {
			return nil, invalidRequest()
		}
		out, err = s.application.GetTaskControlReferencesRefresh(ctx, a, id, refreshID)
	case v.TaskControlConnectionRequest_ACTION_EXCHANGE:
		var in struct {
			CoreAssertion  string   `json:"coreAssertion"`
			RuntimeVersion string   `json:"runtimeVersion"`
			Capabilities   []string `json:"capabilities"`
		}
		if len(r.JsonBody) > 0 {
			if e := taskControlDecode(r.JsonBody, &in); e != nil {
				return nil, e
			}
		}
		out, err = s.application.ExchangeTaskControlSessionNegotiated(ctx, r.Credential, in.RuntimeVersion, in.Capabilities)
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
			var opts application.TaskControlPublicationOptionsV2
			if len(r.JsonBody) > 0 {
				if e = taskControlDecode(r.JsonBody, &opts); e != nil {
					return nil, e
				}
			}
			out, err = s.application.TaskControlPublications(ctx, a, id, uuid.Nil, opts)
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

	case v.TaskControlConnectionRequest_ACTION_HISTORY, v.TaskControlConnectionRequest_ACTION_ROLLBACK:
		id, e := uuid.Parse(r.Id)
		if e != nil {
			return nil, invalidRequest()
		}
		if r.Action == v.TaskControlConnectionRequest_ACTION_HISTORY {
			var opts application.TaskControlHistoryOptions
			if len(r.JsonBody) > 0 {
				if e = taskControlDecode(r.JsonBody, &opts); e != nil {
					return nil, e
				}
			}
			if r.Operation == "history-v1" {
				out, err = s.application.TaskControlHistoryLegacy(ctx, a, id, opts)
			} else {
				out, err = s.application.TaskControlHistory(ctx, a, id, opts)
			}
		} else {
			var in application.TaskControlRollbackInputV2
			if e = taskControlDecode(r.JsonBody, &in); e != nil {
				return nil, e
			}
			out, err = s.application.RollbackTaskControl(ctx, a, id, in)
		}
	case v.TaskControlConnectionRequest_ACTION_RUNTIME_CONNECTION:
		out, err = s.application.TaskControlRuntimeConnection(ctx, r.Credential)
	case v.TaskControlConnectionRequest_ACTION_SOURCE_PENDING:
		out, err = s.application.PendingTaskControlSources(ctx, uuid.Nil, r.Credential)
	case v.TaskControlConnectionRequest_ACTION_SOURCE_SUBMIT:
		rid, e := uuid.Parse(r.Id)
		if e != nil {
			return nil, invalidRequest()
		}
		var input application.TaskControlSourceResult
		if e = taskControlDecode(r.JsonBody, &input); e != nil {
			return nil, e
		}
		if input.CoreAssertion != r.Credential {
			return nil, invalidRequest()
		}
		out, err = s.application.SubmitTaskControlSource(ctx, uuid.Nil, rid, input)
	case v.TaskControlConnectionRequest_ACTION_SOURCE_CREATE, v.TaskControlConnectionRequest_ACTION_SOURCE_GET, v.TaskControlConnectionRequest_ACTION_IMPORT_PREVIEW, v.TaskControlConnectionRequest_ACTION_IMPORT_GET, v.TaskControlConnectionRequest_ACTION_IMPORT_APPLY:
		id, e := uuid.Parse(r.Id)
		if e != nil {
			return nil, invalidRequest()
		}
		out, err = s.taskControlImport(ctx, a, id, r)
	case v.TaskControlConnectionRequest_ACTION_BOOTSTRAP_V2:
		out, err = s.application.TaskControlBootstrapV2(ctx, r.Credential)
	case v.TaskControlConnectionRequest_ACTION_ACK_V2:
		if len(r.JsonBody) > 8192 {
			return nil, invalidRequest()
		}
		var input application.TaskControlAckInputV2
		if e := taskControlDecode(r.JsonBody, &input); e != nil {
			return nil, e
		}
		err = s.application.AckTaskControlV2(ctx, r.Credential, input)
		out = map[string]bool{"acknowledged": err == nil}
	case v.TaskControlConnectionRequest_ACTION_DRAFT_V2, v.TaskControlConnectionRequest_ACTION_UPDATE_DRAFT_V2, v.TaskControlConnectionRequest_ACTION_VALIDATE_V2, v.TaskControlConnectionRequest_ACTION_PUBLISH_V2, v.TaskControlConnectionRequest_ACTION_PUBLICATIONS_V2, v.TaskControlConnectionRequest_ACTION_PUBLICATION_V2, v.TaskControlConnectionRequest_ACTION_ACTIVATE_V2, v.TaskControlConnectionRequest_ACTION_APPLICATION_V2, v.TaskControlConnectionRequest_ACTION_ROLLBACK_V2, v.TaskControlConnectionRequest_ACTION_MODE_PREVIEW_V2, v.TaskControlConnectionRequest_ACTION_MODE_V2:
		id, e := uuid.Parse(r.Id)
		if e != nil {
			return nil, invalidRequest()
		}
		out, err = s.taskControlV2(ctx, a, id, r)
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

func (s *Server) taskControlV2(ctx context.Context, a application.Actor, id uuid.UUID, r *v.TaskControlConnectionRequest) (any, error) {
	switch r.Action {
	case v.TaskControlConnectionRequest_ACTION_DRAFT_V2:
		return s.application.GetTaskControlDraftV2(ctx, a, id)
	case v.TaskControlConnectionRequest_ACTION_UPDATE_DRAFT_V2:
		var in application.TaskControlDraftInputV2
		if e := taskControlDecode(r.JsonBody, &in); e != nil {
			return nil, e
		}
		return s.application.UpdateTaskControlDraftV2(ctx, a, id, in)
	case v.TaskControlConnectionRequest_ACTION_VALIDATE_V2:
		var in application.TaskControlValidateInputV2
		if e := taskControlDecode(r.JsonBody, &in); e != nil {
			return nil, e
		}
		return s.application.ValidateTaskControlDraftV2(ctx, a, id, in)
	case v.TaskControlConnectionRequest_ACTION_PUBLISH_V2:
		var in application.TaskControlPublishInputV2
		if e := taskControlDecode(r.JsonBody, &in); e != nil {
			return nil, e
		}
		return s.application.PublishTaskControlDraftV2(ctx, a, id, in)
	case v.TaskControlConnectionRequest_ACTION_ACTIVATE_V2:
		var in application.TaskControlActivateInputV2
		if e := taskControlDecode(r.JsonBody, &in); e != nil {
			return nil, e
		}
		return s.application.ActivateTaskControlV2(ctx, a, id, in)
	case v.TaskControlConnectionRequest_ACTION_ROLLBACK_V2:
		var in application.TaskControlRollbackInputV2
		if e := taskControlDecode(r.JsonBody, &in); e != nil {
			return nil, e
		}
		return s.application.RollbackTaskControlV2(ctx, a, id, in)
	case v.TaskControlConnectionRequest_ACTION_MODE_PREVIEW_V2:
		var in application.TaskControlModePreviewInputV2
		if e := taskControlDecode(r.JsonBody, &in); e != nil {
			return nil, e
		}
		return s.application.PreviewTaskControlModeV2(ctx, a, id, in)
	case v.TaskControlConnectionRequest_ACTION_MODE_V2:
		var in application.TaskControlModeInputV2
		if e := taskControlDecode(r.JsonBody, &in); e != nil {
			return nil, e
		}
		return s.application.SetTaskControlModeV2(ctx, a, id, in)
	case v.TaskControlConnectionRequest_ACTION_PUBLICATIONS_V2:
		var opts application.TaskControlPublicationOptionsV2
		if len(r.JsonBody) > 0 {
			if e := taskControlDecode(r.JsonBody, &opts); e != nil {
				return nil, e
			}
		}
		return s.application.TaskControlPublicationsV2(ctx, a, id, uuid.Nil, opts)
	case v.TaskControlConnectionRequest_ACTION_PUBLICATION_V2, v.TaskControlConnectionRequest_ACTION_APPLICATION_V2:
		pid, e := uuid.Parse(r.AccountId)
		if e != nil {
			return nil, invalidRequest()
		}
		if r.Action == v.TaskControlConnectionRequest_ACTION_APPLICATION_V2 {
			return s.application.TaskControlApplicationV2(ctx, a, id, pid)
		}
		return s.application.TaskControlPublicationsV2(ctx, a, id, pid)
	}
	return nil, invalidRequest()
}

func (s *Server) taskControlImport(ctx context.Context, a application.Actor, id uuid.UUID, r *v.TaskControlConnectionRequest) (any, error) {
	switch r.Action {
	case v.TaskControlConnectionRequest_ACTION_SOURCE_CREATE:
		var in application.TaskControlSourceInput
		if e := taskControlDecode(r.JsonBody, &in); e != nil {
			return nil, e
		}
		return s.application.CreateTaskControlSource(ctx, a, id, in)
	case v.TaskControlConnectionRequest_ACTION_SOURCE_GET:
		rid, e := uuid.Parse(r.AccountId)
		if e != nil {
			return nil, invalidRequest()
		}
		return s.application.GetTaskControlSource(ctx, a, id, rid)
	case v.TaskControlConnectionRequest_ACTION_IMPORT_PREVIEW:
		var in application.TaskControlImportInput
		if e := taskControlDecode(r.JsonBody, &in); e != nil {
			return nil, e
		}
		return s.application.PreviewTaskControlImport(ctx, a, id, in, r.Operation == "v2")
	case v.TaskControlConnectionRequest_ACTION_IMPORT_GET:
		pid, e := uuid.Parse(r.AccountId)
		if e != nil {
			return nil, invalidRequest()
		}
		return s.application.GetTaskControlImport(ctx, a, id, pid)
	case v.TaskControlConnectionRequest_ACTION_IMPORT_APPLY:
		pid, e := uuid.Parse(r.AccountId)
		if e != nil {
			return nil, invalidRequest()
		}
		var in application.TaskControlImportApplyInput
		if e = taskControlDecode(r.JsonBody, &in); e != nil {
			return nil, e
		}
		return s.application.ApplyTaskControlImport(ctx, a, id, pid, in, r.Operation == "v2")
	}
	return nil, invalidRequest()
}
