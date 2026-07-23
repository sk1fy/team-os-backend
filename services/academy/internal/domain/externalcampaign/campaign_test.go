package externalcampaign

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/course"
	"github.com/sk1fy/team-os-backend/services/academy/internal/domain/courseversion"
)

var campaignNow = time.Date(2026, time.July, 22, 14, 0, 0, 0, time.UTC)

func TestNewCampaignEnforcesOwnerAndPurpose(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*NewParams)
		want   error
	}{
		{name: "company candidate"},
		{name: "partner promo", mutate: func(p *NewParams) {
			ownerID := ID("partner-1")
			p.OwnerType = OwnerPartner
			p.OwnerUserID = &ownerID
			p.Purpose = PurposePartnerPromo
			p.CreatedByID = ownerID
		}},
		{name: "company owner user", mutate: func(p *NewParams) { p.OwnerUserID = idPointer("owner-1") }, want: ErrCompanyOwnerForbidden},
		{name: "company promo purpose", mutate: func(p *NewParams) { p.Purpose = PurposePartnerPromo }, want: ErrOwnerPurposeMismatch},
		{name: "partner owner missing", mutate: func(p *NewParams) {
			p.OwnerType = OwnerPartner
			p.Purpose = PurposePartnerPromo
		}, want: ErrPartnerOwnerRequired},
		{name: "partner candidate purpose", mutate: func(p *NewParams) {
			p.OwnerType = OwnerPartner
			p.OwnerUserID = idPointer("partner-1")
			p.CreatedByID = "partner-1"
		}, want: ErrOwnerPurposeMismatch},
		{name: "other partner creator", mutate: func(p *NewParams) {
			p.OwnerType = OwnerPartner
			p.OwnerUserID = idPointer("partner-1")
			p.Purpose = PurposePartnerPromo
		}, want: ErrPartnerCreatorMismatch},
		{name: "unknown owner", mutate: func(p *NewParams) { p.OwnerType = "other" }, want: ErrUnknownOwnerType},
		{name: "unknown purpose", mutate: func(p *NewParams) { p.Purpose = "other" }, want: ErrUnknownPurpose},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			params := validNewParams()
			if test.mutate != nil {
				test.mutate(&params)
			}
			campaign, err := New(params)
			if !errors.Is(err, test.want) {
				t.Fatalf("New() = %v, want %v", err, test.want)
			}
			if test.want == nil {
				snapshot := campaign.Snapshot()
				if snapshot.Status != StatusActive || snapshot.Name != "Кандидаты — август" ||
					snapshot.CreatedAt != campaignNow || snapshot.UpdatedAt != campaignNow {
					t.Fatalf("snapshot = %#v", snapshot)
				}
			}
		})
	}
}

func TestCampaignDeadlineAndPersistentInvariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Snapshot)
		want   error
	}{
		{name: "valid"},
		{name: "id", mutate: func(s *Snapshot) { s.ID = "" }, want: ErrCampaignIDRequired},
		{name: "company", mutate: func(s *Snapshot) { s.CompanyID = "" }, want: ErrCompanyRequired},
		{name: "course", mutate: func(s *Snapshot) { s.CourseID = "" }, want: ErrCourseRequired},
		{name: "version", mutate: func(s *Snapshot) { s.CourseVersionID = "" }, want: ErrCourseVersionRequired},
		{name: "name", mutate: func(s *Snapshot) { s.Name = "  " }, want: ErrCampaignNameRequired},
		{name: "deadline zero", mutate: func(s *Snapshot) { s.DeadlineDays = 0 }, want: ErrDeadlineDaysInvalid},
		{name: "deadline eight", mutate: func(s *Snapshot) { s.DeadlineDays = 8 }, want: ErrDeadlineDaysInvalid},
		{name: "token hash", mutate: func(s *Snapshot) { s.TokenHash = nil }, want: ErrTokenHashRequired},
		{name: "token prefix", mutate: func(s *Snapshot) { s.TokenPrefix = "bad prefix" }, want: ErrTokenPrefixRequired},
		{name: "creator", mutate: func(s *Snapshot) { s.CreatedByID = "" }, want: ErrCreatorRequired},
		{name: "created at", mutate: func(s *Snapshot) { s.CreatedAt = time.Time{} }, want: ErrCreatedAtRequired},
		{name: "updated before created", mutate: func(s *Snapshot) { s.UpdatedAt = campaignNow.Add(-time.Second) }, want: ErrUpdatedAtInvalid},
		{name: "unknown status", mutate: func(s *Snapshot) { s.Status = "other" }, want: ErrUnknownStatus},
		{name: "active pause metadata", mutate: func(s *Snapshot) { s.PausedAt = timePointer(campaignNow) }, want: ErrPausedStateInvalid},
		{name: "paused without time", mutate: func(s *Snapshot) { s.Status = StatusPaused }, want: ErrPausedStateInvalid},
		{name: "revoked without time", mutate: func(s *Snapshot) { s.Status = StatusRevoked }, want: ErrRevokedStateInvalid},
		{name: "closed without time", mutate: func(s *Snapshot) { s.Status = StatusClosed }, want: ErrClosedStateInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := validSnapshot()
			if test.mutate != nil {
				test.mutate(&value)
			}
			_, err := Rehydrate(value)
			if !errors.Is(err, test.want) {
				t.Fatalf("Rehydrate() = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCampaignPauseResumeTransitions(t *testing.T) {
	t.Parallel()

	campaign := mustCampaign(t)
	beforePause := campaign.Snapshot()
	pausedAt := campaignNow.Add(time.Hour)
	if err := campaign.Pause(pausedAt); err != nil {
		t.Fatal(err)
	}
	paused := campaign.Snapshot()
	if paused.Status != StatusPaused || paused.PausedAt == nil || !paused.PausedAt.Equal(pausedAt) {
		t.Fatalf("paused = %#v", paused)
	}
	if beforePause.CourseVersionID != paused.CourseVersionID || beforePause.DeadlineDays != paused.DeadlineDays {
		t.Fatal("pause changed immutable campaign data")
	}
	if err := campaign.Pause(pausedAt.Add(time.Minute)); !errors.Is(err, ErrCampaignAlreadyPaused) {
		t.Fatalf("second Pause() = %v", err)
	}

	root := validCourse()
	version := validVersion()
	resumedAt := pausedAt.Add(time.Hour)
	if err := campaign.Resume(root, version, resumedAt); err != nil {
		t.Fatal(err)
	}
	resumed := campaign.Snapshot()
	if resumed.Status != StatusActive || resumed.PausedAt != nil || !resumed.UpdatedAt.Equal(resumedAt) {
		t.Fatalf("resumed = %#v", resumed)
	}
	if err := campaign.Resume(root, version, resumedAt.Add(time.Minute)); !errors.Is(err, ErrCampaignNotPaused) {
		t.Fatalf("active Resume() = %v", err)
	}
}

func TestResumeRequiresUsableCourseAndPublishedVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*course.Course, *courseversion.Snapshot)
		allowed bool
		want    error
	}{
		{name: "active", allowed: true},
		{name: "administratively paused course", mutate: func(c *course.Course, _ *courseversion.Snapshot) {
			c.DistributionStatus = course.DistributionPaused
		}, allowed: true},
		{name: "blocked", mutate: func(c *course.Course, _ *courseversion.Snapshot) {
			c.DistributionStatus = course.DistributionBlocked
		}, want: ErrCampaignResumeDenied},
		{name: "archived", mutate: func(c *course.Course, _ *courseversion.Snapshot) {
			c.LifecycleStatus = course.CourseArchived
		}, want: ErrCampaignResumeDenied},
		{name: "deleted", mutate: func(c *course.Course, _ *courseversion.Snapshot) {
			c.LifecycleStatus = course.CourseDeleted
		}, want: ErrCampaignResumeDenied},
		{name: "retired version", mutate: func(_ *course.Course, v *courseversion.Snapshot) {
			v.Status = courseversion.StatusRetired
		}, want: ErrCampaignResumeDenied},
		{name: "wrong version", mutate: func(_ *course.Course, v *courseversion.Snapshot) {
			v.ID = "version-2"
		}, want: ErrCampaignVersionMismatch},
		{name: "wrong course", mutate: func(c *course.Course, _ *courseversion.Snapshot) {
			c.ID = "course-2"
		}, want: ErrCampaignScopeMismatch},
		{name: "wrong course owner", mutate: func(c *course.Course, _ *courseversion.Snapshot) {
			ownerID := course.ID("partner-1")
			c.OwnerType = course.CourseOwnerPartner
			c.OwnerUserID = &ownerID
		}, want: ErrCampaignOwnerMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			campaign := mustCampaign(t)
			if err := campaign.Pause(campaignNow.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			root := validCourse()
			version := validVersion()
			if test.mutate != nil {
				test.mutate(&root, &version)
			}
			before := campaign.Snapshot()
			err := campaign.Resume(root, version, campaignNow.Add(2*time.Hour))
			if !errors.Is(err, test.want) {
				t.Fatalf("Resume() = %v, want %v", err, test.want)
			}
			if (err == nil) != test.allowed {
				t.Fatalf("Resume allowed = %v, want %v", err == nil, test.allowed)
			}
			if err != nil && !reflect.DeepEqual(before, campaign.Snapshot()) {
				t.Fatal("failed resume mutated campaign")
			}
		})
	}
}

func TestTokenRotationPreservesCampaignAndInvalidatesOldToken(t *testing.T) {
	t.Parallel()

	campaign := mustCampaign(t)
	if err := campaign.Pause(campaignNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	before := campaign.Snapshot()
	if !campaign.MatchesTokenHash(campaignHash("old")) || campaign.TokenUsable() {
		t.Fatal("paused token state is incorrect")
	}
	rotatedAt := campaignNow.Add(2 * time.Hour)
	newHash := campaignHash("new")
	if err := campaign.RotateToken(newHash, "newpref", rotatedAt); err != nil {
		t.Fatal(err)
	}
	after := campaign.Snapshot()
	if campaign.MatchesTokenHash(campaignHash("old")) || !campaign.MatchesTokenHash(newHash) {
		t.Fatal("rotation did not invalidate the old token")
	}
	if after.Status != before.Status || after.CourseVersionID != before.CourseVersionID ||
		after.DeadlineDays != before.DeadlineDays || after.TokenRotatedAt == nil ||
		!after.TokenRotatedAt.Equal(rotatedAt) {
		t.Fatalf("rotation changed campaign semantics: before=%#v after=%#v", before, after)
	}
	if err := campaign.RotateToken(newHash, "otherpr", rotatedAt.Add(time.Hour)); !errors.Is(err, ErrTokenRotationInvalid) {
		t.Fatalf("same token RotateToken() = %v", err)
	}
}

func TestCampaignTerminalTransitions(t *testing.T) {
	t.Parallel()

	campaign := mustCampaign(t)
	if err := campaign.Pause(campaignNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := campaign.Revoke(campaignNow.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if campaign.Snapshot().Status != StatusRevoked || campaign.Snapshot().PausedAt != nil || campaign.TokenUsable() {
		t.Fatalf("revoked = %#v", campaign.Snapshot())
	}
	if err := campaign.Revoke(campaignNow.Add(3 * time.Hour)); err != nil {
		t.Fatalf("idempotent Revoke() = %v", err)
	}
	if err := campaign.RotateToken(campaignHash("new"), "newpref", campaignNow.Add(3*time.Hour)); !errors.Is(err, ErrCampaignRevoked) {
		t.Fatalf("RotateToken(revoked) = %v", err)
	}
	if err := campaign.Close(campaignNow.Add(3 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	closed := campaign.Snapshot()
	if closed.Status != StatusClosed || closed.ClosedAt == nil || closed.RevokedAt == nil {
		t.Fatalf("closed = %#v", closed)
	}
	if err := campaign.Close(campaignNow.Add(4 * time.Hour)); err != nil {
		t.Fatalf("idempotent Close() = %v", err)
	}
	if err := campaign.Revoke(campaignNow.Add(4 * time.Hour)); !errors.Is(err, ErrCampaignClosed) {
		t.Fatalf("Revoke(closed) = %v", err)
	}
}

func TestEffectiveAvailabilityPriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     Status
		lifecycle  course.LifecycleStatus
		distribute course.DistributionStatus
		version    courseversion.Status
		want       Availability
	}{
		{name: "available", status: StatusActive, lifecycle: course.CourseActive, distribute: course.DistributionActive, version: courseversion.StatusPublished, want: AvailabilityAvailable},
		{name: "campaign paused", status: StatusPaused, lifecycle: course.CourseActive, distribute: course.DistributionActive, version: courseversion.StatusPublished, want: AvailabilityCampaignPaused},
		{name: "campaign revoked", status: StatusRevoked, lifecycle: course.CourseActive, distribute: course.DistributionActive, version: courseversion.StatusPublished, want: AvailabilityCampaignRevoked},
		{name: "campaign closed", status: StatusClosed, lifecycle: course.CourseActive, distribute: course.DistributionActive, version: courseversion.StatusPublished, want: AvailabilityCampaignClosed},
		{name: "course paused", status: StatusActive, lifecycle: course.CourseActive, distribute: course.DistributionPaused, version: courseversion.StatusPublished, want: AvailabilityCoursePaused},
		{name: "course pause overrides campaign pause", status: StatusPaused, lifecycle: course.CourseActive, distribute: course.DistributionPaused, version: courseversion.StatusPublished, want: AvailabilityCoursePaused},
		{name: "course blocked overrides revoked", status: StatusRevoked, lifecycle: course.CourseActive, distribute: course.DistributionBlocked, version: courseversion.StatusPublished, want: AvailabilityCourseBlocked},
		{name: "archive overrides revoked", status: StatusRevoked, lifecycle: course.CourseArchived, distribute: course.DistributionActive, version: courseversion.StatusPublished, want: AvailabilityCourseArchived},
		{name: "delete overrides closed", status: StatusClosed, lifecycle: course.CourseDeleted, distribute: course.DistributionBlocked, version: courseversion.StatusPublished, want: AvailabilityCourseDeleted},
		{name: "retired version", status: StatusActive, lifecycle: course.CourseActive, distribute: course.DistributionActive, version: courseversion.StatusRetired, want: AvailabilityVersionUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			campaign := campaignWithStatus(t, test.status)
			root := validCourse()
			root.LifecycleStatus = test.lifecycle
			root.DistributionStatus = test.distribute
			version := validVersion()
			version.Status = test.version
			got, err := campaign.EffectiveAvailability(root, version)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("EffectiveAvailability() = %q, want %q", got, test.want)
			}
			if campaign.CanAcceptLearner(root, version) != (test.want == AvailabilityAvailable) {
				t.Fatal("CanAcceptLearner disagrees with effective availability")
			}
		})
	}
}

func TestCampaignSnapshotIsDefensive(t *testing.T) {
	t.Parallel()

	campaign := mustCampaign(t)
	snapshot := campaign.Snapshot()
	snapshot.TokenHash[0] ^= 0xff
	if bytes.Equal(snapshot.TokenHash, campaign.Snapshot().TokenHash) {
		t.Fatal("aggregate token hash mutated through snapshot")
	}
	ownerID := ID("partner-1")
	params := validNewParams()
	params.OwnerType = OwnerPartner
	params.OwnerUserID = &ownerID
	params.Purpose = PurposePartnerPromo
	params.CreatedByID = ownerID
	partnerCampaign, err := New(params)
	if err != nil {
		t.Fatal(err)
	}
	partnerSnapshot := partnerCampaign.Snapshot()
	*partnerSnapshot.OwnerUserID = "partner-2"
	if *partnerCampaign.Snapshot().OwnerUserID != "partner-1" {
		t.Fatal("aggregate owner mutated through snapshot")
	}
}

func validNewParams() NewParams {
	return NewParams{
		ID: "campaign-1", CompanyID: "company-1", CourseID: "course-1",
		CourseVersionID: "version-1", OwnerType: OwnerCompany,
		Purpose: PurposeCompanyCandidate, Name: "  Кандидаты — август  ", DeadlineDays: 3,
		TokenHash: campaignHash("old"), TokenPrefix: "oldpref",
		CreatedByID: "owner-1", CreatedAt: campaignNow,
	}
}

func validSnapshot() Snapshot {
	return Snapshot{
		ID: "campaign-1", CompanyID: "company-1", CourseID: "course-1",
		CourseVersionID: "version-1", OwnerType: OwnerCompany,
		Purpose: PurposeCompanyCandidate, Name: "Кандидаты — август", DeadlineDays: 3,
		Status: StatusActive, TokenHash: campaignHash("old"), TokenPrefix: "oldpref",
		CreatedByID: "owner-1", CreatedAt: campaignNow, UpdatedAt: campaignNow,
	}
}

func mustCampaign(t *testing.T) *Campaign {
	t.Helper()
	campaign, err := Rehydrate(validSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	return campaign
}

func campaignWithStatus(t *testing.T, status Status) *Campaign {
	t.Helper()
	campaign := mustCampaign(t)
	switch status {
	case StatusActive:
	case StatusPaused:
		if err := campaign.Pause(campaignNow.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	case StatusRevoked:
		if err := campaign.Revoke(campaignNow.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	case StatusClosed:
		if err := campaign.Close(campaignNow.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unsupported test status %q", status)
	}
	return campaign
}

func validCourse() course.Course {
	return course.Course{
		ID: "course-1", CompanyID: "company-1", OwnerType: course.CourseOwnerCompany,
		LifecycleStatus: course.CourseActive, DistributionStatus: course.DistributionActive,
	}
}

func validVersion() courseversion.Snapshot {
	publisherID := courseversion.ID("owner-1")
	publishedAt := campaignNow.Add(-time.Hour)
	return courseversion.Snapshot{
		ID: "version-1", CompanyID: "company-1", CourseID: "course-1", Number: 1,
		Status: courseversion.StatusPublished, CreatedByID: publisherID,
		CreatedAt: publishedAt.Add(-time.Hour), PublishedByID: &publisherID,
		PublishedAt: &publishedAt, ContentHash: "hash",
	}
}

func campaignHash(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return digest[:]
}

func idPointer(value ID) *ID { return &value }

func timePointer(value time.Time) *time.Time { return &value }
