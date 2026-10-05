package grpc

import (
	"context"

	"github.com/google/uuid"
	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/company/internal/application"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func connectionProto(c application.DistributionConnection) *v.DistributionConnection {
	return &v.DistributionConnection{BindingId: c.BindingID.String(), Revision: uint64(c.Revision), InstallationId: c.InstallationID.String(), IntegrationId: c.IntegrationID.String(), AccountId: c.AccountID, State: c.State, IntentId: c.IntentID.String(), ExpiresAt: timestamppb.New(c.ExpiresAt), MappingRevision: uint64(c.MappingRevision), MappingAckRevision: uint64(c.MappingAckRevision)}
}
func mappingProto(m application.DistributionEmployeeMapping) *v.DistributionEmployeeMapping {
	p := &v.DistributionEmployeeMapping{Id: m.ID.String(), UserIdSnapshot: m.UserIDSnapshot.String(), CrmUserId: m.CRMUserID, State: m.State, Revision: uint64(m.Revision), VerifiedAt: timestamppb.New(m.VerifiedAt)}
	if m.UserID != nil {
		s := m.UserID.String()
		p.UserId = &s
	}
	return p
}
func (s *Server) GetDistributionConnections(ctx context.Context, _ *v.GetDistributionConnectionsRequest) (*v.GetDistributionConnectionsResponse, error) {
	a, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	rows, e := s.application.ListDistributionConnections(ctx, a)
	if e != nil {
		return nil, transportError(e)
	}
	out := &v.GetDistributionConnectionsResponse{}
	for _, row := range rows {
		out.Connections = append(out.Connections, connectionProto(row))
	}
	return out, nil
}
func (s *Server) LinkDistributionConnection(ctx context.Context, r *v.LinkDistributionConnectionRequest) (*v.LinkDistributionConnectionResponse, error) {
	a, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	if r == nil {
		return nil, invalidRequest()
	}
	i, e := uuid.Parse(r.InstallationId)
	if e != nil {
		return nil, invalidRequest()
	}
	g, e := uuid.Parse(r.IntegrationId)
	if e != nil {
		return nil, invalidRequest()
	}
	intent, e := uuid.Parse(r.IntentId)
	if e != nil {
		return nil, invalidRequest()
	}
	c, e := s.application.LinkDistributionConnection(ctx, a, application.DistributionLinkInput{InstallationID: i, IntegrationID: g, IntentID: intent, AccountID: r.AccountId, WidgetToken: r.WidgetToken})
	if e != nil {
		return nil, transportError(e)
	}
	return &v.LinkDistributionConnectionResponse{Connection: connectionProto(c)}, nil
}
func (s *Server) GetDistributionReferences(ctx context.Context, r *v.GetDistributionReferencesRequest) (*v.GetDistributionReferencesResponse, error) {
	a, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	id, e := uuid.Parse(r.GetBindingId())
	if e != nil {
		return nil, invalidRequest()
	}
	refs, e := s.application.DistributionReferences(ctx, a, id)
	if e != nil {
		return nil, transportError(e)
	}
	out := &v.GetDistributionReferencesResponse{State: refs.State, FetchedAt: timestamppb.New(refs.FetchedAt), FreshUntil: timestamppb.New(refs.FreshUntil)}
	for _, u := range refs.Users {
		out.Users = append(out.Users, &v.DistributionCRMUser{Id: u.ID, Name: u.Name, IsActive: u.IsActive, GroupId: u.GroupID})
	}
	for _, p := range refs.Pipelines {
		cp := &v.DistributionCRMPipeline{Id: p.ID, Name: p.Name}
		for _, st := range p.Statuses {
			cp.Statuses = append(cp.Statuses, &v.DistributionCRMStatus{Id: st.ID, Name: st.Name})
		}
		out.Pipelines = append(out.Pipelines, cp)
	}
	return out, nil
}
func (s *Server) GetDistributionEmployeeMappings(ctx context.Context, r *v.GetDistributionEmployeeMappingsRequest) (*v.GetDistributionEmployeeMappingsResponse, error) {
	a, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	id, e := uuid.Parse(r.GetBindingId())
	if e != nil {
		return nil, invalidRequest()
	}
	rows, e := s.application.ListDistributionEmployeeMappings(ctx, a, id)
	if e != nil {
		return nil, transportError(e)
	}
	out := &v.GetDistributionEmployeeMappingsResponse{}
	for _, m := range rows {
		out.Mappings = append(out.Mappings, mappingProto(m))
	}
	return out, nil
}
func (s *Server) SetDistributionEmployeeMapping(ctx context.Context, r *v.SetDistributionEmployeeMappingRequest) (*v.SetDistributionEmployeeMappingResponse, error) {
	a, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	if r == nil {
		return nil, invalidRequest()
	}
	id, e := uuid.Parse(r.BindingId)
	if e != nil {
		return nil, invalidRequest()
	}
	u, e := uuid.Parse(r.UserId)
	if e != nil {
		return nil, invalidRequest()
	}
	m, e := s.application.SetDistributionEmployeeMapping(ctx, a, id, u, r.CrmUserId)
	if e != nil {
		return nil, transportError(e)
	}
	return &v.SetDistributionEmployeeMappingResponse{Mapping: mappingProto(m)}, nil
}
func (s *Server) CheckDistributionLeadPermission(ctx context.Context, r *v.CheckDistributionLeadPermissionRequest) (*v.CheckDistributionLeadPermissionResponse, error) {
	a, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	id, e := uuid.Parse(r.GetBindingId())
	if e != nil {
		return nil, invalidRequest()
	}
	p, e := s.application.DistributionLeadPermission(ctx, a, id, r.GetLeadId())
	if e != nil {
		return nil, transportError(e)
	}
	return &v.CheckDistributionLeadPermissionResponse{UserId: p.UserID, CanViewLead: p.CanViewLead, Reason: p.Reason, CheckedAt: timestamppb.New(p.CheckedAt)}, nil
}
func (s *Server) RevokeDistributionConnection(ctx context.Context, r *v.RevokeDistributionConnectionRequest) (*v.RevokeDistributionConnectionResponse, error) {
	a, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	id, e := uuid.Parse(r.GetBindingId())
	if e != nil {
		return nil, invalidRequest()
	}
	if e = s.application.RevokeDistributionConnection(ctx, a, id); e != nil {
		return nil, transportError(e)
	}
	return &v.RevokeDistributionConnectionResponse{}, nil
}

func (s *Server) SyncDistributionMappings(ctx context.Context, r *v.SyncDistributionMappingsRequest) (*v.SyncDistributionMappingsResponse, error) {
	a, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	id, e := uuid.Parse(r.GetBindingId())
	if e != nil {
		return nil, invalidRequest()
	}
	if e = s.application.SyncDistributionMappings(ctx, a, id); e != nil {
		return nil, transportError(e)
	}
	return &v.SyncDistributionMappingsResponse{}, nil
}
func (s *Server) ReconcileDistributionEmployeeMappings(ctx context.Context, r *v.ReconcileDistributionEmployeeMappingsRequest) (*v.ReconcileDistributionEmployeeMappingsResponse, error) {
	a, e := s.actor(ctx)
	if e != nil {
		return nil, e
	}
	id, e := uuid.Parse(r.GetBindingId())
	if e != nil {
		return nil, invalidRequest()
	}
	if e = s.application.ReconcileDistributionEmployeeMappings(ctx, a, id); e != nil {
		return nil, transportError(e)
	}
	return &v.ReconcileDistributionEmployeeMappingsResponse{}, nil
}
