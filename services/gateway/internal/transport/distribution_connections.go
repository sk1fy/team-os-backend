package transport

import (
	"net/http"

	"github.com/google/uuid"
	v "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/gateway/internal/api"
)

func connectionAPI(c *v.DistributionConnection) api.DistributionConnection {
	return api.DistributionConnection{BindingId: uuid.MustParse(c.BindingId), Revision: int64(c.Revision), InstallationId: uuid.MustParse(c.InstallationId), IntegrationId: uuid.MustParse(c.IntegrationId), AccountId: c.AccountId, State: api.DistributionConnectionState(c.State), IntentId: uuid.MustParse(c.IntentId), ExpiresAt: c.ExpiresAt.AsTime(), MappingRevision: int64(c.MappingRevision), MappingAckRevision: int64(c.MappingAckRevision)}
}
func mappingAPI(m *v.DistributionEmployeeMapping) api.DistributionEmployeeMapping {
	out := api.DistributionEmployeeMapping{Id: uuid.MustParse(m.Id), UserIdSnapshot: uuid.MustParse(m.UserIdSnapshot), CrmUserId: m.CrmUserId, State: api.DistributionEmployeeMappingState(m.State), Revision: int64(m.Revision), VerifiedAt: m.VerifiedAt.AsTime()}
	if m.UserId != nil {
		u := uuid.MustParse(*m.UserId)
		out.UserId = &u
	}
	return out
}
func (h *Handler) GetDistributionConnections(w http.ResponseWriter, r *http.Request) {
	out, e := h.company.GetDistributionConnections(outgoingContext(r), &v.GetDistributionConnectionsRequest{})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	rows := []api.DistributionConnection{}
	for _, c := range out.Connections {
		rows = append(rows, connectionAPI(c))
	}
	writeJSON(w, 200, rows)
}
func (h *Handler) LinkDistributionConnection(w http.ResponseWriter, r *http.Request) {
	var in api.DistributionLinkInput
	if !decode(w, r, &in) {
		return
	}
	token := ""
	if in.WidgetToken != nil {
		token = *in.WidgetToken
	}
	out, e := h.company.LinkDistributionConnection(outgoingContext(r), &v.LinkDistributionConnectionRequest{InstallationId: in.InstallationId.String(), IntegrationId: in.IntegrationId.String(), AccountId: in.AccountId, IntentId: in.IntentId.String(), WidgetToken: token})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	writeJSON(w, 200, connectionAPI(out.Connection))
}
func (h *Handler) GetDistributionReferences(w http.ResponseWriter, r *http.Request, p api.GetDistributionReferencesParams) {
	out, e := h.company.GetDistributionReferences(outgoingContext(r), &v.GetDistributionReferencesRequest{BindingId: p.BindingId.String()})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	res := api.DistributionReferences{State: api.DistributionReferencesState(out.State), FetchedAt: out.FetchedAt.AsTime(), FreshUntil: out.FreshUntil.AsTime(), Users: []api.DistributionCRMUser{}, Pipelines: []api.DistributionCRMPipeline{}}
	for _, u := range out.Users {
		res.Users = append(res.Users, api.DistributionCRMUser{Id: u.Id, Name: u.Name, IsActive: u.IsActive, GroupId: u.GroupId})
	}
	for _, p := range out.Pipelines {
		cp := api.DistributionCRMPipeline{Id: p.Id, Name: p.Name, Statuses: []api.DistributionCRMStatus{}}
		for _, st := range p.Statuses {
			cp.Statuses = append(cp.Statuses, api.DistributionCRMStatus{Id: st.Id, Name: st.Name})
		}
		res.Pipelines = append(res.Pipelines, cp)
	}
	writeJSON(w, 200, res)
}
func (h *Handler) GetDistributionEmployeeMappings(w http.ResponseWriter, r *http.Request, id api.ID) {
	out, e := h.company.GetDistributionEmployeeMappings(outgoingContext(r), &v.GetDistributionEmployeeMappingsRequest{BindingId: id.String()})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	rows := []api.DistributionEmployeeMapping{}
	for _, m := range out.Mappings {
		rows = append(rows, mappingAPI(m))
	}
	writeJSON(w, 200, rows)
}
func (h *Handler) SetDistributionEmployeeMapping(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.DistributionMappingInput
	if !decode(w, r, &in) {
		return
	}
	out, e := h.company.SetDistributionEmployeeMapping(outgoingContext(r), &v.SetDistributionEmployeeMappingRequest{BindingId: id.String(), UserId: in.UserId.String(), CrmUserId: in.CrmUserId})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	writeJSON(w, 200, mappingAPI(out.Mapping))
}
func (h *Handler) CheckDistributionLeadPermission(w http.ResponseWriter, r *http.Request, id api.ID) {
	var in api.DistributionPermissionInput
	if !decode(w, r, &in) {
		return
	}
	out, e := h.company.CheckDistributionLeadPermission(outgoingContext(r), &v.CheckDistributionLeadPermissionRequest{BindingId: id.String(), LeadId: in.LeadId})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	writeJSON(w, 200, api.DistributionLeadPermission{UserId: out.UserId, CanViewLead: out.CanViewLead, Reason: out.Reason, CheckedAt: out.CheckedAt.AsTime()})
}
func (h *Handler) RevokeDistributionConnection(w http.ResponseWriter, r *http.Request, id api.ID) {
	_, e := h.company.RevokeDistributionConnection(outgoingContext(r), &v.RevokeDistributionConnectionRequest{BindingId: id.String()})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) SyncDistributionMappings(w http.ResponseWriter, r *http.Request, id api.ID) {
	_, e := h.company.SyncDistributionMappings(outgoingContext(r), &v.SyncDistributionMappingsRequest{BindingId: id.String()})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) ReconcileDistributionEmployeeMappings(w http.ResponseWriter, r *http.Request, id api.ID) {
	_, e := h.company.ReconcileDistributionEmployeeMappings(outgoingContext(r), &v.ReconcileDistributionEmployeeMappingsRequest{BindingId: id.String()})
	if e != nil {
		h.writeRPCError(w, r, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
