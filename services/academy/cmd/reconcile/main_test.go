package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRequiresDatabaseURL(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	err := run(context.Background(), func(string) string { return "" }, &output)
	if err == nil || !strings.Contains(err.Error(), "ACADEMY_DB_URL") {
		t.Fatalf("run() error = %v, ожидалась ошибка ACADEMY_DB_URL", err)
	}
	if output.Len() != 0 {
		t.Fatalf("run() вывел данны без подключения: %q", output.String())
	}
}

func TestReconciliationReportIsReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		report reconciliationReport
		want   bool
	}{
		{
			name: "полное совпадение",
			report: reconciliationReport{
				LegacyCoursesTotal:           3,
				VersionedCoursesTotal:        3,
				DeprecatedPublicCoursesTotal: 2,
			},
			want: true,
		},
		{
			name: "курс без версии",
			report: reconciliationReport{
				LegacyCoursesTotal:    3,
				VersionedCoursesTotal: 2,
				CoursesWithoutVersion: 1,
			},
			want: false,
		},
		{
			name: "расхождение прогресса",
			report: reconciliationReport{
				LegacyCoursesTotal:       3,
				VersionedCoursesTotal:    3,
				ProgressStatusMismatches: 1,
			},
			want: false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.report.isReady(); got != test.want {
				t.Fatalf("isReady() = %v, ожидалось %v", got, test.want)
			}
		})
	}
}
