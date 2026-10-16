package application

import (
	"context"

	"github.com/google/uuid"
)

// TaskControlLegacyReferences preserves the published v1 projection without
// confusing an unknown customer capability with an unsupported capability.
func (s *Service) TaskControlLegacyReferences(ctx context.Context, actor Actor, binding uuid.UUID) (any, error) {
	view, err := s.GetTaskControlReferences(ctx, actor, binding, TaskControlReferenceOptions{})
	if err != nil {
		return nil, err
	}
	snapshot := view.CRM
	if snapshot == nil || !snapshot.FreshUntil.After(s.now()) || snapshot.Customers.Availability == "unknown" {
		return nil, coded(ErrorUpstream, "references_unavailable", "Требуется свежий полный справочник; состояние источника доступно в API v2")
	}
	users := []map[string]any{}
	pipelines := []map[string]any{}
	customers := []map[string]any{}
	for _, user := range snapshot.Users {
		users = append(users, map[string]any{"id": user.ID, "name": user.Name, "active": user.IsActive})
	}
	for _, pipeline := range snapshot.Pipelines {
		statuses := []map[string]any{}
		for _, status := range pipeline.Statuses {
			statuses = append(statuses, map[string]any{"id": status.ID, "name": status.Name})
		}
		pipelines = append(pipelines, map[string]any{"id": pipeline.ID, "name": pipeline.Name, "statuses": statuses})
	}
	for _, status := range snapshot.Customers.Statuses {
		customers = append(customers, map[string]any{"id": status.ID, "name": status.Name})
	}
	return map[string]any{"scope": view.Scope, "revision": snapshot.Revision, "fetchedAt": snapshot.Provenance.FetchedAt, "freshUntil": snapshot.FreshUntil, "complete": true,
		"users": users, "pipelines": pipelines, "customersSupported": snapshot.Customers.Availability == "supported", "customerStatuses": customers,
		"sourceSnapshotId": uuid.NewSHA1(uuid.NameSpaceURL, []byte(snapshot.Revision))}, nil
}
