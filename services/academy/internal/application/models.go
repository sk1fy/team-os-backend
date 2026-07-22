package application

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Actor struct {
	UserID        uuid.UUID
	CompanyID     uuid.UUID
	Role          string
	PositionIDs   []uuid.UUID
	DepartmentIDs []uuid.UUID
	// Raw bearer token, forwarded to kb/company on synchronous RPC (§9).
	Token string
}

func (a Actor) canManage() bool {
	return a.Role == "owner" || a.Role == "admin"
}

type Course struct {
	ID                       uuid.UUID
	CompanyID                uuid.UUID
	Title                    string
	Description              *string
	CoverURL                 *string
	Status                   string
	Visibility               string
	AuthorID                 uuid.UUID
	OwnerType                string
	OwnerUserID              *uuid.UUID
	CreatedByID              uuid.UUID
	LifecycleStatus          string
	DistributionStatus       string
	Sequential               bool
	DeadlineDays             *int32
	ArchivedAt               *time.Time
	ArchivedByID             *uuid.UUID
	DeletedAt                *time.Time
	DeletedByID              *uuid.UUID
	CurrentDraftVersionID    *uuid.UUID
	LatestPublishedVersionID *uuid.UUID
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type CourseVersion struct {
	ID                          uuid.UUID
	CompanyID                   uuid.UUID
	CourseID                    uuid.UUID
	Number                      int32
	Status                      string
	Title                       string
	Description                 *string
	CoverFileID                 *uuid.UUID
	CoverURL                    *string
	Sequential                  bool
	DefaultInternalDeadlineDays *int32
	CreatedByID                 uuid.UUID
	CreatedAt                   time.Time
	PublishedByID               *uuid.UUID
	PublishedAt                 *time.Time
	ContentHash                 *string
}

type CourseVersionSection struct {
	ID              uuid.UUID
	CompanyID       uuid.UUID
	CourseVersionID uuid.UUID
	StableKey       string
	Title           string
	Order           int32
}

type CourseVersionLesson struct {
	ID                      uuid.UUID
	CompanyID               uuid.UUID
	CourseVersionID         uuid.UUID
	SectionVersionID        uuid.UUID
	StableKey               string
	Title                   string
	Order                   int32
	Content                 json.RawMessage
	SourceType              string
	SourceArticleID         *uuid.UUID
	SourceArticleVersion    *int32
	SourceTemplateID        *uuid.UUID
	SourceTemplateVersionID *uuid.UUID
	EstimatedMinutes        *int32
	QuizVersionID           *uuid.UUID
	FileIDs                 []uuid.UUID
}

type CourseVersionQuiz struct {
	ID              uuid.UUID
	CompanyID       uuid.UUID
	CourseVersionID uuid.UUID
	LessonVersionID uuid.UUID
	Questions       json.RawMessage
	PassingScore    int32
	MaxAttempts     *int32
}

type CourseVersionContent struct {
	Version  CourseVersion
	Sections []CourseVersionSection
	Lessons  []CourseVersionLesson
	Quizzes  []CourseVersionQuiz
}

type CourseTemplate struct {
	ID                       uuid.UUID
	CompanyID                uuid.UUID
	Type                     string
	SystemTemplateKey        *string
	LifecycleStatus          string
	CurrentDraftVersionID    *uuid.UUID
	LatestPublishedVersionID *uuid.UUID
	CreatedByID              uuid.UUID
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type CourseTemplateVersion struct {
	ID            uuid.UUID
	CompanyID     uuid.UUID
	TemplateID    uuid.UUID
	Number        int32
	Status        string
	Title         string
	Description   *string
	CoverFileID   *uuid.UUID
	Sequential    bool
	CreatedByID   uuid.UUID
	CreatedAt     time.Time
	PublishedByID *uuid.UUID
	PublishedAt   *time.Time
	ContentHash   *string
}

type CourseTemplateVersionSection struct {
	ID                uuid.UUID
	CompanyID         uuid.UUID
	TemplateVersionID uuid.UUID
	StableKey         string
	Title             string
	Order             int32
}

type CourseTemplateVersionLesson struct {
	ID                   uuid.UUID
	CompanyID            uuid.UUID
	TemplateVersionID    uuid.UUID
	SectionVersionID     uuid.UUID
	StableKey            string
	Title                string
	Order                int32
	Content              json.RawMessage
	SourceType           string
	SourceArticleID      *uuid.UUID
	SourceArticleVersion *int32
	EstimatedMinutes     *int32
	QuizVersionID        *uuid.UUID
	FileIDs              []uuid.UUID
}

type CourseTemplateVersionQuiz struct {
	ID                uuid.UUID
	CompanyID         uuid.UUID
	TemplateVersionID uuid.UUID
	LessonVersionID   uuid.UUID
	Questions         json.RawMessage
	PassingScore      int32
	MaxAttempts       *int32
}

type CourseTemplateVersionContent struct {
	Sections []CourseTemplateVersionSection
	Lessons  []CourseTemplateVersionLesson
	Quizzes  []CourseTemplateVersionQuiz
}

type CourseTemplateVersionDetails struct {
	Version CourseTemplateVersion
	Content CourseTemplateVersionContent
}

type CourseTemplateDetails struct {
	Template        CourseTemplate
	Versions        []CourseTemplateVersion
	SelectedVersion *CourseTemplateVersionDetails
}

type CourseTemplateInstantiationResult struct {
	Course Course
	Draft  CourseVersion
	Origin CourseOrigin
}

type CourseTemplateDraftContentInput struct {
	Sections []CourseTemplateDraftSectionInput
}

type CourseTemplateDraftSectionInput struct {
	StableKey string
	Title     string
	Order     int32
	Lessons   []CourseTemplateDraftLessonInput
}

type CourseTemplateDraftLessonInput struct {
	StableKey            string
	Title                string
	Order                int32
	Content              json.RawMessage
	SourceType           string
	SourceArticleID      *uuid.UUID
	SourceArticleVersion *int32
	EstimatedMinutes     *int32
	Quiz                 *CourseTemplateDraftQuizInput
}

type CourseTemplateDraftQuizInput struct {
	Questions    json.RawMessage
	PassingScore int32
	MaxAttempts  *int32
}

type CourseRestriction struct {
	ID               uuid.UUID
	CompanyID        uuid.UUID
	CourseID         uuid.UUID
	Type             string
	Reason           string
	CreatedByID      uuid.UUID
	CreatedAt        time.Time
	ResolvedByID     *uuid.UUID
	ResolvedAt       *time.Time
	ResolutionReason *string
}

type CourseOrigin struct {
	Type                    string
	SourceCourseID          *uuid.UUID
	SourceCourseVersionID   *uuid.UUID
	SourcePartnerID         *uuid.UUID
	SourceTemplateID        *uuid.UUID
	SourceTemplateVersionID *uuid.UUID
	InstantiatedByID        uuid.UUID
	InstantiatedAt          time.Time
	AcquisitionType         string
	EntitlementID           *uuid.UUID
}

type PartnerCourseCopyResult struct {
	Course Course
	Draft  CourseVersion
	Origin CourseOrigin
}

type PartnerCourseGroup struct {
	PartnerID uuid.UUID
	Courses   []Course
}

type PartnerCourseReportSummary struct {
	TotalCourses    int32
	ActiveCourses   int32
	ArchivedCourses int32
	DeletedCourses  int32
	PausedCourses   int32
	BlockedCourses  int32
}

type PartnerCourseOperationalReport struct {
	Course                   Course
	VersionCount             int32
	EnrollmentCount          int32
	ActiveEnrollmentCount    int32
	CompletedEnrollmentCount int32
	AverageProgressPercent   int32
}

type PartnerCoursesReport struct {
	PartnerID       uuid.UUID
	Summary         PartnerCourseReportSummary
	Courses         []PartnerCourseOperationalReport
	ExternalCourses []CourseExternalReport
}

type CourseVersionPreview struct {
	Course           Course
	Version          CourseVersionContent
	PersistsProgress bool
}

type CoursePreviewQuizAttemptResult struct {
	QuizVersionID uuid.UUID
	Score         int32
	Passed        bool
	PendingReview bool
}

type CourseSection struct {
	ID        uuid.UUID
	CompanyID uuid.UUID
	CourseID  uuid.UUID
	Title     string
	Order     int32
}

type Lesson struct {
	ID              uuid.UUID
	CompanyID       uuid.UUID
	CourseID        uuid.UUID
	SectionID       uuid.UUID
	Title           string
	Order           int32
	Content         json.RawMessage
	SourceArticleID *uuid.UUID
	SourceMode      *string
	QuizID          *uuid.UUID
}

type Quiz struct {
	ID           uuid.UUID
	CompanyID    uuid.UUID
	LessonID     uuid.UUID
	Questions    json.RawMessage
	PassingScore int32
	MaxAttempts  *int32
}

type Assignment struct {
	ID              uuid.UUID
	CompanyID       uuid.UUID
	CourseID        uuid.UUID
	CourseVersionID *uuid.UUID
	AssigneeType    string
	AssigneeID      *uuid.UUID
	InviteToken     *string
	DueDate         *time.Time
	AssignedByID    uuid.UUID
	CreatedAt       time.Time
}

type QuizAttempt struct {
	ID            uuid.UUID
	QuizID        uuid.UUID
	UserID        uuid.UUID
	Score         int32
	Passed        bool
	PendingReview bool
	CreatedAt     time.Time
}

type Progress struct {
	UserID                 uuid.UUID
	CourseID               uuid.UUID
	EnrollmentID           *uuid.UUID
	CourseVersionID        *uuid.UUID
	Status                 string
	ProgressPercent        *int32
	CurrentLessonVersionID *uuid.UUID
	CompletedLessonIDs     []uuid.UUID
	QuizAttempts           []QuizAttempt
	StartedAt              *time.Time
	CompletedAt            *time.Time
}

// Enrollment is the version-pinned source of truth for every course run. The
// same model is used by employees now and by external learners in later phases.
type Enrollment struct {
	ID                     uuid.UUID
	CompanyID              uuid.UUID
	CourseID               uuid.UUID
	CourseVersionID        uuid.UUID
	VersionNumber          int32
	LearnerType            string
	UserID                 *uuid.UUID
	ExternalLearnerID      *uuid.UUID
	SourceType             string
	SourceID               *uuid.UUID
	AttemptNumber          int32
	ProgressStatus         string
	AccessStatus           string
	CurrentLessonVersionID *uuid.UUID
	ProgressPercent        int32
	DueDate                *time.Time
	Overdue                bool
	ActivatedAt            *time.Time
	AccessUntil            *time.Time
	StartedAt              *time.Time
	CompletedAt            *time.Time
	LastActivityAt         *time.Time
	FrozenAt               *time.Time
	SuspendedAt            *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type EnrollmentLessonProgress struct {
	CompanyID       uuid.UUID
	EnrollmentID    uuid.UUID
	LessonVersionID uuid.UUID
	Status          string
	FirstOpenedAt   *time.Time
	CompletedAt     *time.Time
	ActiveSeconds   int64
	LastPosition    *string
}

type EnrollmentQuizAttempt struct {
	ID            uuid.UUID
	CompanyID     uuid.UUID
	EnrollmentID  uuid.UUID
	QuizVersionID uuid.UUID
	AttemptNumber int32
	Answers       json.RawMessage
	Score         int32
	Passed        bool
	PendingReview bool
	ReviewedByID  *uuid.UUID
	ReviewedAt    *time.Time
	ReviewComment *string
	CreatedAt     time.Time
}

type EnrollmentQuizAnswer struct {
	QuestionID        string   `json:"questionId"`
	SelectedOptionIDs []string `json:"selectedOptionIds,omitempty"`
	Text              *string  `json:"text,omitempty"`
}

type EnrollmentOutlineLesson struct {
	CourseVersionLesson
	Status        string
	LockReason    *string
	FirstOpenedAt *time.Time
	CompletedAt   *time.Time
}

type EnrollmentOutlineSection struct {
	CourseVersionSection
	Lessons []EnrollmentOutlineLesson
}

type EnrollmentOutline struct {
	Enrollment Enrollment
	Sections   []EnrollmentOutlineSection
}

type EnrollmentLesson struct {
	Enrollment Enrollment
	Lesson     CourseVersionLesson
	Quiz       *CourseVersionQuiz
	Progress   EnrollmentLessonProgress
}

type EnrollmentProgressSnapshot struct {
	Enrollment   Enrollment
	Lessons      []EnrollmentLessonProgress
	QuizAttempts []EnrollmentQuizAttempt
}

type EnrollmentReport struct {
	Enrollment    Enrollment
	Version       CourseVersion
	Lessons       []EnrollmentLessonProgress
	QuizAttempts  []EnrollmentQuizAttempt
	ActiveSeconds int64
}

type PublicCourse struct {
	Course   Course
	Sections []CourseSection
	Lessons  []Lesson
}

// KbArticle is the subset of a knowledge-base article academy copies into
// lessons; fetched over gRPC with the actor's forwarded token.
type KbArticle struct {
	ID        uuid.UUID
	SectionID uuid.UUID
	Title     string
	Content   json.RawMessage
}

// KbArticleSnapshot is an immutable, permission-checked copy returned by KB
// for embedding into Academy-owned version content.
type KbArticleSnapshot struct {
	ArticleID        uuid.UUID
	ArticleVersionID uuid.UUID
	Version          int32
	Title            string
	Content          json.RawMessage
	FileIDs          []uuid.UUID
	ContentHash      string
	CapturedAt       time.Time
}

type KbSection struct {
	ID         uuid.UUID
	Name       string
	Visibility string
}

type KbClient interface {
	GetArticle(ctx context.Context, token string, id uuid.UUID) (KbArticle, error)
	GetPublicArticle(ctx context.Context, id uuid.UUID) (KbArticle, error)
	GetArticlesByIds(ctx context.Context, token string, ids []uuid.UUID) ([]KbArticle, error)
	GetSections(ctx context.Context, token string) ([]KbSection, error)
	GetArticleSnapshotForCourseCopy(
		ctx context.Context,
		token string,
		articleID uuid.UUID,
		articleVersionID *uuid.UUID,
		targetPartnerID *uuid.UUID,
	) (KbArticleSnapshot, error)
}

type CompanyClient interface {
	ValidateUser(ctx context.Context, token string, userID uuid.UUID) error
	GetManagerUserIDs(ctx context.Context, token string) ([]uuid.UUID, error)
	ResolvePositionUsers(ctx context.Context, token string, positionID uuid.UUID) ([]uuid.UUID, error)
	ResolveDepartmentUsers(ctx context.Context, token string, departmentID uuid.UUID) ([]uuid.UUID, error)
}

type FileCloneResult struct {
	State string
	Files map[uuid.UUID]uuid.UUID
}

type FilesClient interface {
	CloneFilesForOwner(
		ctx context.Context,
		companyID, userID uuid.UUID,
		role, idempotencyKey string,
		targetOwnerType string,
		targetOwnerID uuid.UUID,
		sourceFileIDs []uuid.UUID,
	) (FileCloneResult, error)
}
