package clients

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
)

type Company struct {
	client companyv1.CompanyServiceClient
}

func NewCompany(client companyv1.CompanyServiceClient) *Company {
	return &Company{client: client}
}

var _ application.CompanyClient = (*Company)(nil)

func (c *Company) ResolvePositionUsers(ctx context.Context, token string, positionID uuid.UUID) ([]uuid.UUID, error) {
	callContext, cancel := outgoing(ctx, token)
	defer cancel()
	response, err := c.client.ResolvePositionUsers(callContext, &companyv1.ResolvePositionUsersRequest{
		PositionId: positionID.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("company.ResolvePositionUsers: %w", err)
	}
	return parseUserIDs(response.GetUserIds())
}

func (c *Company) ResolveDepartmentUsers(ctx context.Context, token string, departmentID uuid.UUID) ([]uuid.UUID, error) {
	callContext, cancel := outgoing(ctx, token)
	defer cancel()
	response, err := c.client.ResolveDepartmentUsers(callContext, &companyv1.ResolveDepartmentUsersRequest{
		DepartmentId: departmentID.String(), IncludeDescendants: true,
	})
	if err != nil {
		return nil, fmt.Errorf("company.ResolveDepartmentUsers: %w", err)
	}
	return parseUserIDs(response.GetUserIds())
}

func parseUserIDs(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("invalid user id %q", value)
		}
		result = append(result, parsed)
	}
	return result, nil
}
