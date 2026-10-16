package taskcontrol

// Permissions never derives authority from browser roles or Distribution access.
type Permissions struct {
	Read   bool `json:"read"`
	Manage bool `json:"manage"`
}

func Access(role string, override *Permissions) Permissions {
	admin := role == "owner" || role == "admin"
	p := Permissions{Read: admin, Manage: admin}
	if override != nil {
		p.Read = override.Read
		p.Manage = admin && override.Read && override.Manage
	}
	return p
}

// RequiredAccess is shared by capability discovery and guards for later APIs.
// Unknown operations fail closed.
func RequiredAccess(operation string) (manage bool, known bool) {
	switch operation {
	case "connections", "installation", "draft", "publications", "history", "references", "application":
		return false, true
	case "link", "revoke", "mode", "updateDraft", "validate", "publish", "rollback", "imports", "sourceRequests", "refreshReferences", "intent":
		return true, true
	default:
		return false, false
	}
}
