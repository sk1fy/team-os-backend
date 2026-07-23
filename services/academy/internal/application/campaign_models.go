package application

import (
	"time"

	"github.com/google/uuid"
)

// ExternalCampaign is a version-pinned public distribution link. The full
// token is intentionally present only in ExternalCampaignCreated.
type ExternalCampaign struct {
	ID              uuid.UUID
	CompanyID       uuid.UUID
	CourseID        uuid.UUID
	CourseVersionID uuid.UUID
	OwnerType       string
	OwnerUserID     *uuid.UUID
	Purpose         string
	Name            string
	DeadlineDays    int32
	Status          string
	TokenPrefix     string
	CreatedByID     uuid.UUID
	CreatedAt       time.Time
	PausedAt        *time.Time
	RevokedAt       *time.Time
}

type ExternalCampaignCreated struct {
	Campaign ExternalCampaign
	Token    string
}

type CampaignFunnel struct {
	Views          int64
	UniqueVisitors int64
	FormSubmits    int64
	VerifiedEmails int64
	Activations    int64
	Completions    int64
}

type CampaignLessonDropOff struct {
	LessonVersionID uuid.UUID
	Reached         int64
	Completed       int64
}

type CampaignAttribution struct {
	UTMSource   *string
	UTMMedium   *string
	UTMCampaign *string
	UTMContent  *string
	UTMTerm     *string
	Referrer    *string
	Visits      int64
	Activations int64
	Completions int64
}

type CampaignVersionAnalytics struct {
	CourseVersionID        uuid.UUID
	VersionNumber          int32
	Activations            int64
	Completions            int64
	AverageProgressPercent float64
}

type CampaignAnalytics struct {
	FirstLessonStarts      int64
	LessonCompletions      int64
	QuizSubmissions        int64
	ExpiredEnrollments     int64
	ReturnVisits           int64
	AverageProgressPercent float64
	MedianProgressPercent  float64
	AverageCompletionSecs  *int64
	MedianCompletionSecs   *int64
	LessonDropOff          []CampaignLessonDropOff
	Attribution            []CampaignAttribution
	Versions               []CampaignVersionAnalytics
}

type ExternalCampaignReport struct {
	Campaign    ExternalCampaign
	Funnel      CampaignFunnel
	Enrollments []Enrollment
	Analytics   CampaignAnalytics
}

type CourseExternalReport struct {
	CourseID    uuid.UUID
	Enrollments []Enrollment
}

type ExternalLearnerTimelineEvent struct {
	ID              uuid.UUID
	Type            string
	OccurredAt      time.Time
	CourseID        *uuid.UUID
	CourseVersionID *uuid.UUID
	EnrollmentID    *uuid.UUID
	SourceType      *string
	SourceID        *uuid.UUID
	CourseTitle     *string
	VersionNumber   *int32
	ProgressPercent *int32
	AccessStatus    *string
	DeletedCourse   bool
}

type ExternalLearnerTimeline struct {
	Learner ExternalLearner
	Events  []ExternalLearnerTimelineEvent
}

type CampaignAnalyticsContext struct {
	VisitorHash []byte
	UTMSource   *string
	UTMMedium   *string
	UTMCampaign *string
	UTMContent  *string
	UTMTerm     *string
	Referrer    *string
}
