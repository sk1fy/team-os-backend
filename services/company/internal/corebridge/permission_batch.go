package corebridge

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/google/uuid"
)

// Permissions is additive to the single-lead API. Rollout fallback is permitted
// only when an older Core does not implement the endpoint (404), using the same
// caller budget and four workers. Authorization/outage errors never trigger it.
func (c *Client) Permissions(ctx context.Context, scope Scope, actor PermissionInput, ids []string) (map[string]Permission, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, errors.New("Некорректная страница прав")
	}
	requested := make(map[string]bool, len(ids))
	for _, id := range ids {
		value, err := strconv.ParseInt(id, 10, 64)
		if err != nil || value <= 0 || strconv.FormatInt(value, 10) != id || requested[id] {
			return nil, errors.New("Некорректный или повторный ID сделки")
		}
		requested[id] = true
	}
	var out struct {
		Items []struct {
			Permission
			LeadID string `json:"leadId"`
		} `json:"items"`
	}
	err := c.call(ctx, "POST", "/internal/v1/distribution/bindings/"+scope.BindingID.String()+"/permissions/batch?bindingRevision="+strconv.FormatInt(scope.BindingRevision, 10), scope, struct {
		EmployeeID uuid.UUID `json:"employeeId"`
		UserID     string    `json:"userId"`
		LeadIDs    []string  `json:"leadIds"`
	}{actor.EmployeeID, actor.UserID, ids}, &out)
	if err != nil {
		var response *Error
		if !errors.As(err, &response) || response.Status != 404 {
			return nil, err
		}
		results := make([]Permission, len(ids))
		errs := make([]error, len(ids))
		jobs := make(chan int, len(ids))
		for i := range ids {
			jobs <- i
		}
		close(jobs)
		var workers sync.WaitGroup
		for i := 0; i < 4 && i < len(ids); i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for j := range jobs {
					if e := ctx.Err(); e != nil {
						errs[j] = e
						continue
					}
					in := actor
					in.LeadID = ids[j]
					results[j], errs[j] = c.Permission(ctx, scope, in)
				}
			}()
		}
		workers.Wait()
		permissions := make(map[string]Permission, len(ids))
		for i, id := range ids {
			if errs[i] != nil {
				return nil, errs[i]
			}
			permissions[id] = results[i]
		}
		return permissions, nil
	}
	permissions := make(map[string]Permission, len(ids))
	if len(out.Items) != len(ids) {
		return nil, errors.New("Неполный ответ прав Core")
	}
	for _, item := range out.Items {
		if !requested[item.LeadID] {
			return nil, errors.New("Некорректный ответ прав Core")
		}
		if _, ok := permissions[item.LeadID]; ok {
			return nil, errors.New("Повторные права в ответе Core")
		}
		permissions[item.LeadID] = item.Permission
	}
	return permissions, nil
}
