package clients

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	companyv1 "github.com/sk1fy/team-os-backend/contracts/gen/go/company/v1"
	"github.com/sk1fy/team-os-backend/services/academy/internal/application"
)

type Company struct {
	client  companyv1.CompanyServiceClient
	breaker *circuitBreaker
}

func NewCompany(client companyv1.CompanyServiceClient) *Company {
	return &Company{client: client, breaker: newCircuitBreaker()}
}

var _ application.CompanyClient = (*Company)(nil)

func (c *Company) ValidateUser(ctx context.Context, token string, userID uuid.UUID) error {
	response, err := callWithResilience(ctx, token, c.breaker, func(callContext context.Context) (*companyv1.GetUsersByIdsResponse, error) {
		return c.client.GetUsersByIds(callContext, &companyv1.GetUsersByIdsRequest{
			UserIds: []string{userID.String()},
		})
	})
	if err != nil {
		return fmt.Errorf("company.GetUsersByIds: %w", err)
	}
	users := response.GetUsers()
	if len(users) != 1 || users[0].GetId() != userID.String() ||
		users[0].GetStatus() != companyv1.UserStatus_USER_STATUS_ACTIVE {
		return fmt.Errorf("company.GetUsersByIds: active user %s not found", userID)
	}
	return nil
}

func (c *Company) GetManagerUserIDs(ctx context.Context, token string) ([]uuid.UUID, error) {
	response, err := callWithResilience(ctx, token, c.breaker, func(callContext context.Context) (*companyv1.GetUsersResponse, error) {
		return c.client.GetUsers(callContext, &companyv1.GetUsersRequest{})
	})
	if err != nil {
		return nil, fmt.Errorf("company.GetUsers: %w", err)
	}
	result := make([]uuid.UUID, 0, len(response.GetUsers()))
	for _, user := range response.GetUsers() {
		if user.GetStatus() != companyv1.UserStatus_USER_STATUS_ACTIVE ||
			(user.GetRole() != companyv1.UserRole_USER_ROLE_OWNER && user.GetRole() != companyv1.UserRole_USER_ROLE_ADMIN) {
			continue
		}
		id, parseErr := uuid.Parse(user.GetId())
		if parseErr != nil {
			return nil, fmt.Errorf("company.GetUsers: invalid manager id %q", user.GetId())
		}
		result = append(result, id)
	}
	return result, nil
}

func (c *Company) ResolvePositionUsers(ctx context.Context, token string, positionID uuid.UUID) ([]uuid.UUID, error) {
	response, err := callWithResilience(ctx, token, c.breaker, func(callContext context.Context) (*companyv1.ResolvePositionUsersResponse, error) {
		return c.client.ResolvePositionUsers(callContext, &companyv1.ResolvePositionUsersRequest{
			PositionId: positionID.String(),
		})
	})
	if err != nil {
		return nil, fmt.Errorf("company.ResolvePositionUsers: %w", err)
	}
	return parseUserIDs(response.GetUserIds())
}

func (c *Company) ResolveDepartmentUsers(ctx context.Context, token string, departmentID uuid.UUID) ([]uuid.UUID, error) {
	response, err := callWithResilience(ctx, token, c.breaker, func(callContext context.Context) (*companyv1.ResolveDepartmentUsersResponse, error) {
		return c.client.ResolveDepartmentUsers(callContext, &companyv1.ResolveDepartmentUsersRequest{
			DepartmentId: departmentID.String(), IncludeDescendants: true,
		})
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
