package application

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/sk1fy/team-os-backend/services/academy/internal/storage/db"
)

func TestRequireCompletedFileCloneJobs(t *testing.T) {
	tests := []struct {
		name     string
		statuses []string
		wantErr  bool
	}{
		{name: "нет заданий"},
		{name: "все завершены", statuses: []string{"completed", "completed"}},
		{name: "ожидает", statuses: []string{"pending"}, wantErr: true},
		{name: "выполняется", statuses: []string{"running"}, wantErr: true},
		{name: "ошибка с возможностью повтора", statuses: []string{"failed"}, wantErr: true},
		{name: "одно из заданий не завершено", statuses: []string{"completed", "pending"}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			jobs := make([]db.LockCourseVersionFileCloneJobsRow, len(test.statuses))
			for index, status := range test.statuses {
				jobs[index] = db.LockCourseVersionFileCloneJobsRow{ID: uuid.New(), Status: status}
			}
			err := requireCompletedFileCloneJobs(jobs)
			if test.wantErr {
				var applicationErr *Error
				if !errors.As(err, &applicationErr) || applicationErr.Kind != ErrorConflict {
					t.Fatalf("ожидался conflict, получено %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
		})
	}
}

func TestRequireSingleFileCloneRewrite(t *testing.T) {
	databaseErr := errors.New("ошибка базы")
	tests := []struct {
		name       string
		affected   int64
		rewriteErr error
		wantErr    bool
	}{
		{name: "одна строка", affected: 1},
		{name: "цель уже не черновик", affected: 0, wantErr: true},
		{name: "неожиданно несколько строк", affected: 2, wantErr: true},
		{name: "ошибка SQL", rewriteErr: databaseErr, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := requireSingleFileCloneRewrite(test.affected, test.rewriteErr, "перезаписать файл")
			if (err != nil) != test.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, test.wantErr)
			}
			if test.rewriteErr != nil && !errors.Is(err, databaseErr) {
				t.Fatalf("исходная ошибка потеряна: %v", err)
			}
		})
	}
}
