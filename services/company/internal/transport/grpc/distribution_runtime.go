package grpc

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sk1fy/team-os-backend/services/company/internal/application"

	"github.com/google/uuid"
	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
)

func runtimeID(value string, required bool) (uuid.UUID, error) {
	if value == "" && !required {
		return uuid.Nil, nil
	}
	id, e := uuid.Parse(value)
	if e != nil || id == uuid.Nil {
		return uuid.Nil, invalidRequest()
	}
	return id, nil
}
func (s *Server) ReadDistributionRuntime(ctx context.Context, r *v.ReadDistributionRuntimeRequest) (*v.ReadDistributionRuntimeResponse, error) {
	a, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	if r == nil {
		return nil, invalidRequest()
	}
	switch r.Kind {
	case "settings", "rules", "availability", "queue", "history", "detail", "summary":
	default:
		return nil, invalidRequest()
	}
	id, e := runtimeID(r.Id, r.Kind == "availability" || r.Kind == "history" || r.Kind == "detail")
	if e != nil {
		return nil, e
	}
	if r.Limit < 0 || r.Limit > 100 || r.Offset < 0 || r.Offset > 100000 {
		return nil, invalidRequest()
	}
	var raw json.RawMessage
	switch r.Kind {
	case "queue", "summary":
		var group uuid.UUID
		group, e = runtimeID(r.GroupId, false)
		if e != nil {
			return nil, e
		}
		f := application.DistributionQueueFilter{Tab: r.Tab, GroupID: group}
		for _, field := range []struct {
			value string
			dst   **time.Time
		}{{r.From, &f.From}, {r.To, &f.To}} {
			if field.value != "" {
				v, e := time.Parse(time.RFC3339Nano, field.value)
				if e != nil {
					return nil, invalidRequest()
				}
				*field.dst = &v
			}
		}
		if r.Kind == "summary" {
			raw, e = s.application.DistributionSummary(ctx, a, group)
		} else {
			limit := r.Limit
			if limit == 0 {
				limit = 50
			}
			raw, e = s.application.DistributionQueuePage(ctx, a, f, limit, r.Offset)
		}
	case "detail":
		raw, e = s.application.DistributionQueueDetail(ctx, a, id)
	default:
		raw, e = s.application.DistributionRuntimeRead(ctx, a, r.Kind, id, r.Limit, r.Offset)
	}
	if e != nil {
		return nil, transportError(e)
	}
	return &v.ReadDistributionRuntimeResponse{Payload: raw}, nil
}
func (s *Server) WriteDistributionRuntime(ctx context.Context, r *v.WriteDistributionRuntimeRequest) (*v.WriteDistributionRuntimeResponse, error) {
	a, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	if r == nil {
		return nil, invalidRequest()
	}
	switch r.Kind {
	case "settings", "rules", "rule", "action", "group":
	default:
		return nil, invalidRequest()
	}
	id, e := runtimeID(r.Id, r.Kind == "rule" || r.Kind == "action" || r.Kind == "group")
	if e != nil {
		return nil, e
	}
	if len(r.Payload) == 0 || len(r.Payload) > 65536 || !json.Valid(r.Payload) {
		return nil, invalidRequest()
	}
	raw, e := s.application.DistributionRuntimeWrite(ctx, a, r.Kind, id, json.RawMessage(r.Payload))
	if e != nil {
		return nil, transportError(e)
	}
	return &v.WriteDistributionRuntimeResponse{Payload: raw}, nil
}
