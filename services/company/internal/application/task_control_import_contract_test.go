package application

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	tc "github.com/sk1fy/team-os-backend/services/company/internal/domain/taskcontrol"
)

func TestTaskControlImportAcceptsActualWidgetCaptureFixture(t *testing.T) {
	raw, e := os.ReadFile("../../../../contracts/fixtures/task-control-import-source-result.json")
	if e != nil {
		t.Fatal(e)
	}
	var result TaskControlSourceResult
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&result); e != nil {
		t.Fatal("widget wire differs from company DTO", e)
	}
	if !result.Complete {
		t.Fatal("fixture is partial")
	}
	_, checksum := tc.Legacy(result.Payload.Rules)
	if checksum != result.SourceChecksum {
		t.Fatal("widget checksum differs from server")
	}
	if issues := tc.ValidateImportCapture(result.Payload.Rules, result.Payload.Raw, result.Payload.FieldIDs, result.Payload.Code, "catalog", result.Payload.CatalogID, "123"); len(issues) != 0 {
		t.Fatal("actual capture rejected", issues)
	}
	if len(result.Payload.Rules) != 1 || result.Payload.Rules[0].Enabled || !result.Payload.Rules[0].Archived || len(result.Payload.Rules[0].Pipelines[0].StatusIDs) != 3 {
		t.Fatal("legacy fields or closed stages lost")
	}
}
