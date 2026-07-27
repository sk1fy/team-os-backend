package application

import "testing"

func TestPersonalAccessAvailability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		status          string
		lifecycle       string
		distribution    string
		version         string
		deadlineExpired bool
		wantAvailable   bool
		wantReason      string
	}{
		{name: "active", status: "issued", lifecycle: "active", distribution: "active", version: "published", wantAvailable: true},
		{name: "distribution paused", status: "issued", lifecycle: "active", distribution: "paused", version: "published", wantReason: publicAccessReasonDistributionPaused},
		{name: "course blocked", status: "issued", lifecycle: "active", distribution: "blocked", version: "published", wantReason: publicAccessReasonCourseBlocked},
		{name: "course archived", status: "issued", lifecycle: "archived", distribution: "active", version: "published", wantReason: publicAccessReasonCourseArchived},
		{name: "course deleted", status: "issued", lifecycle: "deleted", distribution: "active", version: "published", wantReason: publicAccessReasonCourseDeleted},
		{name: "access revoked", status: "revoked", lifecycle: "active", distribution: "active", version: "published", wantReason: publicAccessReasonAccessRevoked},
		{name: "access closed", status: "closed", lifecycle: "active", distribution: "active", version: "published", wantReason: publicAccessReasonAccessRevoked},
		{name: "access expired", status: "activated", lifecycle: "active", distribution: "active", version: "published", deadlineExpired: true, wantReason: publicAccessReasonAccessExpired},
		{name: "already activated", status: "activated", lifecycle: "active", distribution: "paused", version: "published", wantReason: publicAccessReasonAlreadyActivated},
		{name: "version unavailable", status: "issued", lifecycle: "active", distribution: "active", version: "draft", wantReason: publicAccessReasonVersionUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := personalAccessAvailability(
				test.status,
				test.lifecycle,
				test.distribution,
				test.version,
				test.deadlineExpired,
			)
			if got.Available != test.wantAvailable || got.Reason != test.wantReason {
				t.Fatalf("availability = %#v, want available=%v reason=%q", got, test.wantAvailable, test.wantReason)
			}
			if !got.Available && got.Message == "" {
				t.Fatal("unavailable state must include a user-facing message")
			}
		})
	}
}

func TestCampaignAccessAvailability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		status            string
		lifecycle         string
		distribution      string
		version           string
		hasEnrollment     bool
		enrollmentExpired bool
		wantAvailable     bool
		wantReason        string
	}{
		{name: "active", status: "active", lifecycle: "active", distribution: "active", version: "published", wantAvailable: true},
		{name: "distribution paused", status: "active", lifecycle: "active", distribution: "paused", version: "published", wantReason: publicAccessReasonDistributionPaused},
		{name: "campaign paused", status: "paused", lifecycle: "active", distribution: "active", version: "published", wantReason: publicAccessReasonCampaignPaused},
		{name: "campaign revoked", status: "revoked", lifecycle: "active", distribution: "active", version: "published", wantReason: publicAccessReasonCampaignRevoked},
		{name: "course blocked", status: "active", lifecycle: "active", distribution: "blocked", version: "published", wantReason: publicAccessReasonCourseBlocked},
		{name: "course archived", status: "active", lifecycle: "archived", distribution: "active", version: "published", wantReason: publicAccessReasonCourseArchived},
		{name: "course deleted", status: "active", lifecycle: "deleted", distribution: "active", version: "published", wantReason: publicAccessReasonCourseDeleted},
		{name: "already activated survives pauses", status: "paused", lifecycle: "active", distribution: "paused", version: "published", hasEnrollment: true, wantReason: publicAccessReasonAlreadyActivated},
		{name: "existing access expired", status: "active", lifecycle: "active", distribution: "active", version: "published", hasEnrollment: true, enrollmentExpired: true, wantReason: publicAccessReasonAccessExpired},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := campaignAccessAvailability(
				test.status,
				test.lifecycle,
				test.distribution,
				test.version,
				test.hasEnrollment,
				test.enrollmentExpired,
			)
			if got.Available != test.wantAvailable || got.Reason != test.wantReason {
				t.Fatalf("availability = %#v, want available=%v reason=%q", got, test.wantAvailable, test.wantReason)
			}
			if !got.Available && got.Message == "" {
				t.Fatal("unavailable state must include a user-facing message")
			}
		})
	}
}
