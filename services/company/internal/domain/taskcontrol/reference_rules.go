package taskcontrol

import "fmt"

// ValidateReferenceRules checks active legacy selections against the full CRM source.
// Explicit legacy CRM IDs do not require a TeamOS employee mapping.
func ValidateReferenceRules(rules []Rule, refs ReferenceSnapshot) []Issue {
	issues := []Issue{}
	users := map[string]bool{}
	for _, user := range refs.Users {
		users[user.ID] = user.IsActive
	}
	pipelines := map[string]map[string]bool{}
	for _, pipeline := range refs.Pipelines {
		statuses := map[string]bool{}
		for _, item := range pipeline.Statuses {
			statuses[item.ID] = true
		}
		pipelines[pipeline.ID] = statuses
	}
	customers := map[string]bool{}
	for _, item := range refs.Customers.Statuses {
		customers[item.ID] = true
	}
	add := func(path, code, message string) {
		issues = append(issues, Issue{Path: path, Code: code, Message: message})
	}
	for i, rule := range rules {
		if !rule.Enabled || rule.Archived {
			continue
		}
		path := fmt.Sprintf("rules[%d]", i)
		for j, id := range rule.Users {
			active, exists := users[id]
			if !exists {
				add(fmt.Sprintf("%s.users[%d]", path, j), "unknown_crm_id", "Пользователь отсутствует в справочнике выбранного аккаунта")
			} else if !active {
				add(fmt.Sprintf("%s.users[%d]", path, j), "inactive_user", "Пользователь amoCRM неактивен")
			}
		}
		for j, pipeline := range rule.Pipelines {
			statuses, exists := pipelines[pipeline.PipelineID]
			if !exists {
				add(fmt.Sprintf("%s.pipelines[%d].pipelineId", path, j), "unknown_crm_id", "Воронка отсутствует в выбранном аккаунте")
				continue
			}
			for k, id := range pipeline.StatusIDs {
				if !statuses[id] {
					add(fmt.Sprintf("%s.pipelines[%d].statusIds[%d]", path, j, k), "unknown_crm_id", "Этап отсутствует или принадлежит другой воронке")
				}
			}
		}
		if rule.Customers {
			switch refs.Customers.Availability {
			case "supported":
				for j, id := range rule.CustomerStatuses {
					if !customers[id] {
						add(fmt.Sprintf("%s.customerStatuses[%d]", path, j), "unknown_crm_id", "Статус покупателя отсутствует в выбранном аккаунте")
					}
				}
			case "unsupported":
				add(path+".customers", "unknown_crm_id", "Покупатели недоступны в выбранном аккаунте")
			default:
				add(path+".customers", "incomplete_source", "Не удалось подтвердить справочник покупателей")
			}
		}
	}
	return issues
}
