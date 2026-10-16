package application

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
	"github.com/sk1fy/team-os-backend/services/company/internal/storage/db"
)

// Export resolves every saved rule, including disabled/archive, without editing
// the publication. A runtime actor projection is never an export source.
func taskControlExportIntents(original []tc.IntentRule) ([]tc.IntentRule, error) {
	raw, e := json.Marshal(original)
	if e != nil {
		return nil, e
	}
	var rules []tc.IntentRule
	if e = json.Unmarshal(raw, &rules); e != nil {
		return nil, e
	}
	for i := range rules {
		if r := rules[i].Dynamic; r != nil {
			r.Enabled = true
			r.Archived = false
		} else if r := rules[i].Legacy; r != nil {
			r.Enabled = true
			r.Archived = false
		}
	}
	return rules, nil
}
func taskControlRestoreExportFlags(original []tc.IntentRule, resolved tc.Resolution) (tc.Resolution, error) {
	if resolved.HasBlockingIssues() {
		for _, issue := range resolved.Issues {
			if issue.Code == "runtime_payload_too_large" {
				return resolved, coded(ErrorPayloadTooLarge, issue.Code, issue.Message)
			}
		}
		return resolved, coded(ErrorValidation, "export_unrepresentable", "Невозможно экспортировать недоступные ссылки")
	}
	// Preserve R11's explicit all-empty export confirmation. Inactive rules must
	// remain representable; their metadata cannot silently disappear from export.
	if len(resolved.Rules) == 0 && len(original) > 0 {
		allActive := true
		for _, item := range original {
			if r := item.Dynamic; r != nil {
				allActive = allActive && r.Enabled && !r.Archived
			} else if r := item.Legacy; r != nil {
				allActive = allActive && r.Enabled && !r.Archived
			}
		}
		if allActive {
			resolved.RuntimeRules, resolved.ResolvedChecksum = tc.Legacy([]tc.Rule{})
			return resolved, nil
		}
	}
	if len(resolved.Rules) != len(original) {
		return resolved, coded(ErrorValidation, "export_unrepresentable", "Невозможно экспортировать все правила без потери состава; исправьте недоступные или пустые ссылки")
	}
	flags := map[uuid.UUID][2]bool{}
	for _, item := range original {
		if r := item.Dynamic; r != nil {
			flags[r.RuleUID] = [2]bool{r.Enabled, r.Archived}
		} else if r := item.Legacy; r != nil {
			flags[r.RuleUID] = [2]bool{r.Enabled, r.Archived}
		}
	}
	for i := range resolved.Rules {
		flag, ok := flags[resolved.Rules[i].RuleUID]
		if !ok {
			return resolved, validation("Состав экспорта не соответствует намерению")
		}
		resolved.Rules[i].Enabled = flag[0]
		resolved.Rules[i].Archived = flag[1]
	}
	resolved.RuntimeRules, resolved.ResolvedChecksum = tc.Legacy(resolved.Rules)
	raw, e := json.Marshal(resolved.RuntimeRules)
	if e != nil {
		return resolved, e
	}
	if len(raw) > tc.RuntimeMaxBytes {
		return resolved, coded(ErrorPayloadTooLarge, "runtime_payload_too_large", "Полный экспорт превышает 1 МиБ")
	}
	return resolved, nil
}
func (s *Service) taskControlExportTx(ctx context.Context, q *db.Queries, c db.TaskControlConnection, original []tc.IntentRule) (tc.Resolution, TaskControlSourceRevisions, time.Time, error) {
	rules, e := taskControlExportIntents(original)
	if e != nil {
		return tc.Resolution{}, TaskControlSourceRevisions{}, time.Time{}, e
	}
	resolved, src, fresh, e := s.taskControlResolveTx(ctx, q, c, rules, true)
	if e != nil {
		return resolved, src, fresh, e
	}
	resolved, e = taskControlRestoreExportFlags(original, resolved)
	if e == nil && !fresh.After(s.now()) {
		e = coded(ErrorUpstream, "references_unavailable", "Источники экспорта устарели")
	}
	return resolved, src, fresh, e
}
