package application

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ExternalPersonalAccess struct {
	ID                 uuid.UUID
	CompanyID          uuid.UUID
	CourseID           uuid.UUID
	CourseVersionID    uuid.UUID
	PartnerOwnerID     uuid.UUID
	ExternalLearnerID  *uuid.UUID
	ExpectedEmail      string
	RecipientFirstName *string
	RecipientLastName  *string
	DeadlineDays       int32
	Status             string
	TokenPrefix        string
	EnrollmentID       *uuid.UUID
	IssuedByID         uuid.UUID
	IssuedAt           time.Time
	ActivatedAt        *time.Time
	RevokedAt          *time.Time
}

type ExternalPersonalAccessCreated struct {
	Access ExternalPersonalAccess
	Token  string
}

type ExternalLearner struct {
	ID              uuid.UUID
	CompanyID       uuid.UUID
	Email           string
	NormalizedEmail string
	FirstName       *string
	LastName        *string
	Phone           *string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type PartnerExternalReportQuery struct {
	Search   *string
	CourseID *uuid.UUID
	Page     int32
	PageSize int32
}

type PartnerExternalReportRow struct {
	EnrollmentID    uuid.UUID
	CourseID        uuid.UUID
	CourseTitle     string
	LearnerEmail    string
	LearnerName     *string
	ProgressStatus  string
	AccessStatus    string
	ProgressPercent int32
	ActivatedAt     *time.Time
	CompletedAt     *time.Time
}

type PartnerExternalReportPage struct {
	Items    []PartnerExternalReportRow
	Page     int32
	PageSize int32
	Total    int64
}

type ExternalPrincipal struct {
	CompanyID uuid.UUID
	LearnerID uuid.UUID
	SessionID uuid.UUID
	ExpiresAt time.Time
}

type PublicAcademyOutlineLesson struct {
	ID               uuid.UUID
	Title            string
	Order            int32
	EstimatedMinutes *int32
}

type PublicAcademyOutlineSection struct {
	ID      uuid.UUID
	Title   string
	Order   int32
	Lessons []PublicAcademyOutlineLesson
}

type PublicAcademyAccess struct {
	Kind                      string
	CourseID                  uuid.UUID
	CourseVersionID           uuid.UUID
	Title                     string
	Description               *string
	CoverURL                  *string
	OwnerType                 string
	OwnerUserID               *uuid.UUID
	DeadlineDays              int32
	Available                 bool
	UnavailableReason         *string
	EmailVerificationRequired bool
	Outline                   []PublicAcademyOutlineSection
}

type ExternalVerificationChallenge struct {
	ID        uuid.UUID
	ExpiresAt time.Time
}

type ExternalVerificationConfirmed struct {
	LearnerID        uuid.UUID
	VerifiedAt       time.Time
	SessionToken     string
	SessionExpiresAt time.Time
}

type ExternalQuizAttemptResult struct {
	ID                uuid.UUID
	Score             int32
	Passed            bool
	PendingReview     bool
	AttemptsRemaining *int32
	CreatedAt         time.Time
}

// ExternalQuizAttemptSubmitted is the atomic public quiz response: attempt plus
// the updated enrollment snapshot so the client does not need a follow-up GET.
type ExternalQuizAttemptSubmitted struct {
	Attempt    ExternalQuizAttemptResult
	Enrollment Enrollment
}

type ExternalEnrollmentResults struct {
	Enrollment         Enrollment
	CompletedLessonIDs []uuid.UUID
	QuizAttempts       []ExternalQuizAttemptResult
}

type RequestExternalVerificationInput struct {
	AccessToken string
	Email       string
	FirstName   *string
	LastName    *string
	IPHash      []byte
	Analytics   CampaignAnalyticsContext
}

type SubmitExternalQuizInput struct {
	EnrollmentID   uuid.UUID
	QuizID         uuid.UUID
	IdempotencyKey string
	Answers        []EnrollmentQuizAnswer
}

type ExternalEnrollmentReport struct {
	Learner       ExternalLearner
	Enrollment    Enrollment
	Lessons       []EnrollmentLessonProgress
	QuizAttempts  []EnrollmentQuizAttempt
	ActiveSeconds int64
	History       json.RawMessage
}
