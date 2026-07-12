package org

// ID is an opaque identifier of a company domain entity.
type ID string

// UserRole is a user's role within a company.
type UserRole string

const (
	RoleOwner    UserRole = "owner"
	RoleAdmin    UserRole = "admin"
	RoleEmployee UserRole = "employee"
	RolePartner  UserRole = "partner"
)

// UserStatus is a user's lifecycle status within a company.
type UserStatus string

const (
	StatusActive      UserStatus = "active"
	StatusInvited     UserStatus = "invited"
	StatusDeactivated UserStatus = "deactivated"
)

// User contains the user attributes used by organization rules.
// Persistence and transport models intentionally remain outside this package.
type User struct {
	ID     ID
	Email  string
	Role   UserRole
	Status UserStatus
}

// Department contains the attributes needed to build and validate the
// organization tree. A nil ParentID denotes a root department.
type Department struct {
	ID                   ID
	Name                 string
	ParentID             *ID
	HeadUserID           *ID
	ValuableFinalProduct *string
	Order                int
}
