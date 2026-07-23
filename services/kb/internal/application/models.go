package application

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	domainaccess "github.com/sk1fy/team-os-backend/services/kb/internal/domain/access"
)

type Actor struct {
	UserID        uuid.UUID
	CompanyID     uuid.UUID
	Role          string
	PositionIDs   []uuid.UUID
	DepartmentIDs []uuid.UUID
}

func (a Actor) subject() domainaccess.Subject {
	return domainaccess.Subject{
		UserID: a.UserID, Role: a.Role,
		PositionIDs: a.PositionIDs, DepartmentIDs: a.DepartmentIDs,
	}
}

type AccessSettings struct {
	Scope         domainaccess.Scope
	DepartmentIDs []uuid.UUID
	PositionIDs   []uuid.UUID
	UserIDs       []uuid.UUID
}

type PartnerAccessSettings struct {
	Mode       string
	PartnerIDs []uuid.UUID
}

func (s PartnerAccessSettings) allows(partnerID uuid.UUID) bool {
	switch s.Mode {
	case "all":
		return true
	case "selected":
		for _, id := range s.PartnerIDs {
			if id == partnerID {
				return true
			}
		}
	}
	return false
}

type Section struct {
	ID            uuid.UUID
	CompanyID     uuid.UUID
	Name          string
	ParentID      *uuid.UUID
	Order         int32
	Access        AccessSettings
	PartnerAccess PartnerAccessSettings
	Visibility    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (s Section) domain(byID map[uuid.UUID]Section) domainaccess.Section {
	return domainaccess.Section{
		ID: s.ID, ParentID: s.ParentID,
		Access: domainaccess.Settings{
			Scope:         s.Access.Scope,
			DepartmentIDs: append([]uuid.UUID(nil), s.Access.DepartmentIDs...),
			PositionIDs:   append([]uuid.UUID(nil), s.Access.PositionIDs...),
			UserIDs:       append([]uuid.UUID(nil), s.Access.UserIDs...),
		},
	}
}

type Article struct {
	ID                      uuid.UUID
	CompanyID               uuid.UUID
	SectionID               uuid.UUID
	Title                   string
	Content                 json.RawMessage
	Status                  string
	AuthorID                uuid.UUID
	Version                 int32
	RequiresAcknowledgement bool
	PlainText               string
	CreatedAt               time.Time
	UpdatedAt               time.Time
	PartnerAccess           PartnerAccessSettings
	PartnerReusePolicy      string
}

type ArticlePartnerPolicy struct {
	ArticleID   uuid.UUID
	Access      PartnerAccessSettings
	ReusePolicy string
	UpdatedAt   time.Time
	UpdatedByID *uuid.UUID
}

type ArticleCourseCopyPermission struct {
	CanRead                  bool
	CanCopy                  bool
	ReusePolicy              string
	DenialReason             string
	ResolvedArticleVersionID *uuid.UUID
}

type ArticleSnapshotAttachment struct {
	FileID uuid.UUID
}

type ArticleSnapshotForCourseCopy struct {
	ArticleID        uuid.UUID
	ArticleVersionID uuid.UUID
	Version          int32
	Title            string
	Content          json.RawMessage
	Attachments      []ArticleSnapshotAttachment
	ContentHash      string
	CapturedAt       time.Time
}

type ArticleVersion struct {
	ID        uuid.UUID
	CompanyID uuid.UUID
	ArticleID uuid.UUID
	Version   int32
	Title     string
	Content   json.RawMessage
	AuthorID  uuid.UUID
	CreatedAt time.Time
}

type Acknowledgement struct {
	ArticleID      uuid.UUID
	UserID         uuid.UUID
	AcknowledgedAt time.Time
}
