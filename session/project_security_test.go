package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedProjectIDIsOpaqueAndSafe(t *testing.T) {
	id := generateProjectID("../../../tmp/owned")
	if !strings.HasPrefix(id, "proj_") {
		t.Fatalf("generated ID %q has no project prefix", id)
	}
	if !validProjectID(id) {
		t.Fatalf("generated ID %q is not accepted by validation", id)
	}
	if strings.Contains(id, "tmp") || strings.ContainsAny(id, `/\\`) {
		t.Fatalf("generated ID leaks caller-controlled path data: %q", id)
	}
}

func TestProjectIDRejectsPathTraversal(t *testing.T) {
	invalid := []string{".", "..", "../outside", "safe/../../outside", `safe\\outside`, "/absolute", "project.", "CON", "con.json", "COM1", "lpt9.data"}
	for _, id := range invalid {
		if validProjectID(id) {
			t.Errorf("validProjectID(%q) = true, want false", id)
		}
	}

	valid := []string{"", "proj_legacy-name_123", "proj_550e8400-e29b-41d4-a716-446655440000"}
	for _, id := range valid {
		if !validProjectID(id) {
			t.Errorf("validProjectID(%q) = false, want true", id)
		}
	}
}

func TestProjectCatalogRejectsCaseAliasedPortableIDs(t *testing.T) {
	storage := newTestStorage(t)
	catalog := &ProjectsData{Projects: []*Project{
		{ID: "proj_A", Name: "one"},
		{ID: "proj_a", Name: "two"},
	}}
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storage.configDir, "projects.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.LoadProjects(); err == nil || !strings.Contains(err.Error(), "duplicate project ID") {
		t.Fatalf("case-aliased project catalog error = %v, want duplicate refusal", err)
	}
}
