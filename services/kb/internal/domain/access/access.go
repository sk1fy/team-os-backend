// Package access implements AccessSettings inheritance and authorization checks.
package access

import (
	"github.com/google/uuid"
)

type Scope string

const (
	ScopeCompany Scope = "company"
	ScopeCustom  Scope = "custom"
)

type Settings struct {
	Scope         Scope
	DepartmentIDs []uuid.UUID
	PositionIDs   []uuid.UUID
	UserIDs       []uuid.UUID
}

type Section struct {
	ID       uuid.UUID
	ParentID *uuid.UUID
	Access   Settings
}

type Subject struct {
	UserID        uuid.UUID
	Role          string
	PositionIDs   []uuid.UUID
	DepartmentIDs []uuid.UUID
}

// EffectiveAccess resolves inherited access for a section tree.
func EffectiveAccess(section Section, byID map[uuid.UUID]Section) Settings {
	current := section
	visited := make(map[uuid.UUID]struct{}, len(byID))
	for {
		if _, exists := visited[current.ID]; exists {
			// Corrupted hierarchies fail closed instead of hanging a request.
			return Settings{Scope: ScopeCustom}
		}
		visited[current.ID] = struct{}{}
		if current.Access.Scope == ScopeCustom {
			return current.Access
		}
		if current.ParentID == nil {
			return Settings{
				Scope:         ScopeCompany,
				DepartmentIDs: nil,
				PositionIDs:   nil,
				UserIDs:       nil,
			}
		}
		parent, ok := byID[*current.ParentID]
		if !ok {
			return Settings{
				Scope:         ScopeCompany,
				DepartmentIDs: nil,
				PositionIDs:   nil,
				UserIDs:       nil,
			}
		}
		current = parent
	}
}

// Allowed reports whether subject can read content protected by access settings.
func Allowed(subject Subject, settings Settings) bool {
	if subject.Role == "partner" && settings.Scope == ScopeCompany {
		return false
	}
	if settings.Scope == ScopeCompany {
		return subject.Role != "partner"
	}
	if containsUUID(settings.UserIDs, subject.UserID) {
		return true
	}
	if intersects(settings.PositionIDs, subject.PositionIDs) {
		return true
	}
	return intersects(settings.DepartmentIDs, subject.DepartmentIDs)
}

// CanManage reports whether subject can create or edit KB content.
func CanManage(subject Subject) bool {
	return subject.Role == "owner" || subject.Role == "admin"
}

func containsUUID(values []uuid.UUID, target uuid.UUID) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func intersects(left, right []uuid.UUID) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := make(map[uuid.UUID]struct{}, len(left))
	for _, value := range left {
		seen[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[value]; ok {
			return true
		}
	}
	return false
}
