package seed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSeparateFilesWithWrappers(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, directory, "company.json", `{
  "company": {"id":"company-1","name":"Ромашка","ownerId":"legacy-owner"}
}`)
	writeFixture(t, directory, "users.json", `{
  "data": [{"id":"user-1","email":"owner@example.com","firstName":"Анна","lastName":"Смирнова","role":"owner","status":"active","positionIds":[]}]
}`)
	writeFixture(t, directory, "departments.json", `{"departments":[]}`)
	writeFixture(t, directory, "positions.json", `[]`)
	writeFixture(t, directory, "invites.json", `{"items":[]}`)
	writeFixture(t, directory, "metadata.json", `{"CURRENT_USER_ID":"user-1"}`)

	fixtures, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if fixtures.Company.ID != "company-1" || fixtures.Company.Name != "Ромашка" {
		t.Fatalf("unexpected company: %#v", fixtures.Company)
	}
	if len(fixtures.Users) != 1 || fixtures.Users[0].ID != "user-1" {
		t.Fatalf("unexpected users: %#v", fixtures.Users)
	}
	if fixtures.CurrentUserID != "user-1" {
		t.Fatalf("CurrentUserID = %q, want user-1", fixtures.CurrentUserID)
	}
}

func TestLoadManifestWrapper(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, directory, "fixtures.json", `{
  "manifest": {
    "metadata": {"currentUser":{"id":"user-owner"}},
    "data": {
      "company": {"id":"company-1","name":"Ромашка","ownerId":"outdated-owner"},
      "users": [{"id":"user-owner","email":"owner@example.com","firstName":"Анна","lastName":"Смирнова","role":"owner","status":"active","positionIds":[]}],
      "departments": [],
      "positions": [],
      "invites": []
    }
  }
}`)

	fixtures, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if fixtures.CurrentUserID != "user-owner" {
		t.Fatalf("CurrentUserID = %q, want user-owner", fixtures.CurrentUserID)
	}
	if fixtures.Company.OwnerID != "outdated-owner" {
		t.Fatalf("company owner should remain raw until Normalize: %#v", fixtures.Company)
	}
}

func TestLoadReportsMissingEntityFiles(t *testing.T) {
	directory := t.TempDir()
	writeFixture(t, directory, "company.json", `{"id":"company-1","name":"Ромашка","ownerId":"user-1"}`)

	_, err := Load(directory)
	if err == nil {
		t.Fatal("Load() expected an error")
	}
	for _, expected := range []string{"users", "departments", "positions", "invites"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error %q does not mention %q", err, expected)
		}
	}
}

func writeFixture(t *testing.T, directory, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
