package activitybridge

import (
	"encoding/json"
	"errors"
	"regexp"

	"github.com/google/uuid"
	dto "github.com/sk1fy/team-os-backend/services/company/internal/activitycontract"
)

var accountPattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)

func ParseBindings(raw string) ([]dto.ActivityScope, error) {
	var bindings []dto.ActivityScope
	if json.Unmarshal([]byte(raw), &bindings) != nil || len(bindings) == 0 {
		return nil, errors.New("Activity bindings required")
	}
	companies := map[uuid.UUID]bool{}
	installations := map[uuid.UUID]bool{}
	for _, b := range bindings {
		if b.CompanyId == uuid.Nil || b.InstallationId == uuid.Nil || b.IntegrationId == uuid.Nil || !accountPattern.MatchString(b.AccountId) || companies[b.CompanyId] || installations[b.InstallationId] {
			return nil, errors.New("invalid or ambiguous Activity binding")
		}
		companies[b.CompanyId] = true
		installations[b.InstallationId] = true
	}
	return bindings, nil
}
