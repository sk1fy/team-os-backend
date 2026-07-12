package application

import (
	"context"

	"github.com/sk1fy/team-os-backend/services/tasks/internal/storage/db"
)

func (s *Service) GetLabels(ctx context.Context, actor Actor) ([]Label, error) {
	rows, err := db.New(s.pool).ListLabels(ctx, actor.CompanyID)
	if err != nil {
		return nil, internal("Не удалось получить метки", err)
	}
	labels := make([]Label, 0, len(rows))
	for _, row := range rows {
		labels = append(labels, labelFromDB(row))
	}
	return labels, nil
}
