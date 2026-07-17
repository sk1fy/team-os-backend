package application

import (
	"time"

	"github.com/google/uuid"
)

type Actor struct {
	UserID    uuid.UUID
	CompanyID uuid.UUID
	Role      string
}

type Company struct {
	ID           uuid.UUID
	Name         string
	LogoURL      *string
	OwnerID      uuid.UUID
	CreatedAt    time.Time
	AmoAccountID *string
}

type User struct {
	ID                uuid.UUID
	CompanyID         uuid.UUID
	Email             string
	FirstName         string
	LastName          string
	AvatarURL         *string
	Phone             *string
	Role              string
	Status            string
	PositionIDs       []uuid.UUID
	BirthDate         *string
	HiredAt           *string
	VacationAllowance *int16
	CreatedAt         time.Time
	Source            string
	AccessMode        string
}

type EmployeeAccess struct {
	Mode          string
	LinkToken     *string
	LinkCreatedAt *time.Time
}

type EmployeeLinkAccess struct {
	Token     string
	CreatedAt time.Time
}

type SetPasswordAccessInput struct {
	Password *string
}

type ExternalEmployee struct {
	ID        string
	Name      string
	Email     *string
	AvatarURL *string
	GroupID   string
	GroupName string
}

type Department struct {
	ID                   uuid.UUID
	Name                 string
	ParentID             *uuid.UUID
	HeadUserID           *uuid.UUID
	ValuableFinalProduct *string
	Order                int32
}

type Position struct {
	ID                uuid.UUID
	Name              string
	DepartmentID      uuid.UUID
	Level             int16
	Description       *string
	ArticleIDs        []uuid.UUID
	RequiredCourseIDs []uuid.UUID
}

type Invite struct {
	ID           uuid.UUID
	Email        *string
	Token        string
	Role         string
	PositionID   *uuid.UUID
	DepartmentID *uuid.UUID
	InvitedByID  uuid.UUID
	Status       string
	CreatedAt    time.Time
}

type AuthResult struct {
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
	User             User
}

type SessionMeta struct {
	UserAgent string
	IPAddress string
}

type RegisterInput struct {
	CompanyName string
	Email       string
	Password    string
	FirstName   string
	LastName    string
}

type LoginInput struct {
	Email    string
	Password string
}

type AcceptInviteInput struct {
	Token     string
	Email     string
	FirstName string
	LastName  string
	Password  string
}

type UpdateCurrentUserInput struct {
	FirstName    *string
	LastName     *string
	SetPhone     bool
	Phone        *string
	SetAvatarURL bool
	AvatarURL    *string
}

type UpdateCompanyInput struct {
	Name            *string
	SetLogoURL      bool
	LogoURL         *string
	SetAmoAccountID bool
	AmoAccountID    *string
}

type CreateDepartmentInput struct {
	Name                 string
	ParentID             *uuid.UUID
	HeadUserID           *uuid.UUID
	ValuableFinalProduct *string
}

type UpdateDepartmentInput struct {
	ID                      uuid.UUID
	Name                    *string
	SetHeadUserID           bool
	HeadUserID              *uuid.UUID
	SetValuableFinalProduct bool
	ValuableFinalProduct    *string
}

type CreatePositionInput struct {
	Name         string
	DepartmentID uuid.UUID
	Level        *int16
	Description  *string
}

type UpdatePositionInput struct {
	ID             uuid.UUID
	Name           *string
	DepartmentID   *uuid.UUID
	Level          *int16
	SetDescription bool
	Description    *string
}

type CreateUserInput struct {
	FirstName   string
	LastName    string
	Email       string
	Phone       *string
	Role        string
	PositionIDs []uuid.UUID
}

type UpdateUserInput struct {
	ID                   uuid.UUID
	FirstName            *string
	LastName             *string
	SetPhone             bool
	Phone                *string
	SetBirthDate         bool
	BirthDate            *string
	SetHiredAt           bool
	HiredAt              *string
	SetVacationAllowance bool
	VacationAllowance    *int16
	Role                 *string
	Status               *string
	SetPositionIDs       bool
	PositionIDs          []uuid.UUID
}

type InviteUserInput struct {
	Email        *string
	Role         string
	PositionID   *uuid.UUID
	DepartmentID *uuid.UUID
}

type ScheduleTemplate struct {
	Type       string
	Days       []int
	On         int
	Off        int
	Start      string
	End        string
	CycleStart string
}

type UserSchedule struct {
	UserID   uuid.UUID
	Template ScheduleTemplate
}

type UserCard struct {
	User     User
	Schedule UserSchedule
}

type UpdateUserCardInput struct {
	User     UpdateUserInput
	Schedule ScheduleTemplate
}

type ShiftException struct {
	ID     uuid.UUID
	UserID uuid.UUID
	Date   string
	Type   string
	Start  *string
	End    *string
	Note   *string
}

type SaveShiftExceptionInput struct {
	UserID uuid.UUID
	Date   string
	Type   string
	Start  *string
	End    *string
	Note   *string
}

type DistributionGroup struct {
	ID                uuid.UUID
	Name              string
	Description       *string
	Active            bool
	Algorithm         string
	MemberIDs         []uuid.UUID
	DisabledMemberIDs []uuid.UUID
	Source            string
	DealLimit         int32
	UnclaimedMinutes  int32
	CreatedAt         time.Time
}

type DistributionEvent struct {
	ID         uuid.UUID
	GroupID    uuid.UUID
	DealNumber int64
	UserID     uuid.UUID
	Status     string
	CreatedAt  time.Time
}

type CreateDistributionGroupInput struct {
	Name        string
	Description *string
	MemberIDs   []uuid.UUID
}

type UpdateDistributionGroupInput struct {
	ID                   uuid.UUID
	Name                 *string
	SetDescription       bool
	Description          *string
	Active               *bool
	Algorithm            *string
	SetMemberIDs         bool
	MemberIDs            []uuid.UUID
	SetDisabledMemberIDs bool
	DisabledMemberIDs    []uuid.UUID
	Source               *string
	DealLimit            *int32
	UnclaimedMinutes     *int32
}
