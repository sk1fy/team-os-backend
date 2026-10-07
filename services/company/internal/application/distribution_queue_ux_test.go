package application

import (
	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
	"testing"
	"time"
)

func TestDistributionQueueDeadlineExact72HourBoundary(t *testing.T) {
	created := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	deadline := created.Add(72 * time.Hour)
	row := db.DistributionQueue{WaitingDeadlineAt: deadline, OperationID: nullID(uuid.New()), Reason: "operation_unfinished"}
	for _, tc := range []struct {
		now  time.Time
		want string
	}{{deadline.Add(-time.Nanosecond), "operation_unfinished"}, {deadline, "waiting_expired_cancel_pending"}, {deadline.Add(time.Nanosecond), "waiting_expired_cancel_pending"}} {
		if got := queueVisibleReason(row, tc.now); got != tc.want {
			t.Fatalf("boundary %s got%s", tc.now, got)
		}
	}
	row.Settled = true
	row.State = "confirmed"
	row.Reason = "assignment_confirmed"
	if got := queueVisibleReason(row, deadline.Add(time.Hour)); got != "assignment_confirmed" {
		t.Fatal("confirmed result obscured by deadline")
	}
}
