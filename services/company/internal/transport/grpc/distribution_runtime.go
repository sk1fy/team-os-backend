package grpc

import (
	"context"
	"encoding/json"

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
	case "settings", "rules", "availability", "queue", "history":
	default:
		return nil, invalidRequest()
	}
	id, e := runtimeID(r.Id, r.Kind == "availability" || r.Kind == "history")
	if e != nil {
		return nil, e
	}
	if r.Limit < 0 || r.Limit > 100 || r.Offset < 0 || r.Offset > 100000 {
		return nil, invalidRequest()
	}
	raw, e := s.application.DistributionRuntimeRead(ctx, a, r.Kind, id, r.Limit, r.Offset)
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
	case "settings", "rules", "rule":
	default:
		return nil, invalidRequest()
	}
	id, e := runtimeID(r.Id, r.Kind == "rule")
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
