package application

import "github.com/google/uuid"

func canManageBoardStructure(actor Actor) bool {
	return actor.Role == "owner" || actor.Role == "admin"
}

func canCreateTask(actor Actor) bool {
	switch actor.Role {
	case "owner", "admin", "employee", "partner":
		return true
	default:
		return false
	}
}

func canAccessTask(actor Actor, task Task) bool {
	switch actor.Role {
	case "owner", "admin", "employee":
		return true
	case "partner":
		if task.AuthorID == actor.UserID || containsUUID(task.AssigneeIDs, actor.UserID) || containsUUID(task.WatcherIDs, actor.UserID) {
			return true
		}
		return task.AssigneePositionID != nil && containsUUID(actor.PositionIDs, *task.AssigneePositionID)
	default:
		return false
	}
}

func containsUUID(values []uuid.UUID, target uuid.UUID) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
