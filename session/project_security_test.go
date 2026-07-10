package session

import (
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
	invalid := []string{".", "..", "../outside", "safe/../../outside", `safe\\outside`, "/absolute"}
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
