package application

import (
	"time"

	"github.com/google/uuid"
)

type Actor struct {
	UserID        uuid.UUID
	CompanyID     uuid.UUID
	Role          string
	SectionAccess []string
	RequestID     string
}

type Company struct {
	ID                    uuid.UUID
	Name                  string
	LogoURL               *string
	OwnerID               uuid.UUID
	Status                string
	OnboardingCompletedAt *time.Time
	CreatedAt             time.Time
	AmoAccountID          *string
}

type User struct {
	ID                uuid.UUID
	CompanyID         uuid.UUID
	Login             string
	Email             string
	FirstName         string
	LastName          string
	AvatarURL         *string
	Phone             *string
	Role              string
	Status            string
	PositionIDs       []uuid.UUID
	DepartmentIDs     []uuid.UUID
	BirthDate         *string
	HiredAt           *string
	VacationAllowance *int16
	ShowInSchedule    bool
	CreatedAt         time.Time
	Source            string
	AccessMode        string
	SectionAccess     []string
}

type ReportUserScope struct {
	UserIDs       []uuid.UUID
	SearchUserIDs []uuid.UUID
}

type ResolveReportUserScopeInput struct {
	Search       *string
	PositionID   *uuid.UUID
	DepartmentID *uuid.UUID
}

type ReportUserProfile struct {
	UserID         uuid.UUID
	Email          string
	FirstName      string
	LastName       string
	PositionName   *string
	DepartmentName *string
}

type GetReportUserProfilesInput struct {
	UserIDs               []uuid.UUID
	PreferredPositionID   *uuid.UUID
	PreferredDepartmentID *uuid.UUID
}

type EmployeeAccess struct {
	Mode            string
	Login           string
	PasswordEnabled bool
	LinkEnabled     bool
	LinkToken       *string
	LinkCreatedAt   *time.Time
}

type EmployeePasswordAccess struct {
	Login    string
	Password string
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
	Source               string
	IsRoot               bool
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
	CompanyName       string
	Email             string
	Password          string
	FirstName         string
	LastName          string
	RegistrationToken string
}

type CompanyRegistrationTokenResult struct {
	Token     string
	ExpiresAt time.Time
}

type CompanyRegistrationTokenValidation struct {
	Valid             bool
	State             string
	ExternalAccountID *string
	ExpiresAt         *time.Time
}

type AmoWidgetSessionResult struct {
	Action                string
	ExternalAccountID     string
	RegistrationToken     string
	SessionToken          string
	Email                 string
	CompanyName           string
	RequiresPasswordSetup bool
	ExpiresAt             *time.Time
}

type AmoWidgetSessionInput struct {
	Token             string
	ExternalAccountID string
	ExternalUserID    string
	Email             string
	UserName          string
	CompanyName       string
}

type AmoWidgetContinuation struct {
	Email                 string
	CompanyName           string
	RequiresPasswordSetup bool
	ExpiresAt             time.Time
}

type LoginInput struct {
	Login    string
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
	FirstName *string
	LastName  *string
	SetPhone  bool
	Phone     *string
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
	LastName    *string
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
	SetShowInSchedule    bool
	ShowInSchedule       bool
	Role                 *string
	Status               *string
	SetPositionIDs       bool
	PositionIDs          []uuid.UUID
	SetSectionAccess     bool
	SectionAccess        []string
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
