package clients

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"google.golang.org/grpc/metadata"
)

type Company struct {
	client       companyv1.CompanyServiceClient
	serviceToken string
}

func NewCompany(client companyv1.CompanyServiceClient, serviceToken string) *Company {
	return &Company{client: client, serviceToken: serviceToken}
}
func (c *Company) ResolvePositionUsers(ctx context.Context, companyID uuid.UUID, token string, positionID uuid.UUID) ([]uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	} else {
		if c.serviceToken == "" {
			return nil, fmt.Errorf("tasks service credential is not configured")
		}
		ctx = metadata.AppendToOutgoingContext(ctx, "x-tasks-service-token", c.serviceToken, "x-company-id", companyID.String())
	}
	response, err := c.client.ResolvePositionUsers(ctx, &companyv1.ResolvePositionUsersRequest{PositionId: positionID.String()})
	if err != nil {
		return nil, fmt.Errorf("company.ResolvePositionUsers: %w", err)
	}
	result := make([]uuid.UUID, 0, len(response.GetUserIds()))
	for _, raw := range response.GetUserIds() {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			return nil, fmt.Errorf("company.ResolvePositionUsers: invalid user id")
		}
		result = append(result, id)
	}
	return result, nil
}
