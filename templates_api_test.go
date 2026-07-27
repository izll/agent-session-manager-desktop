package main

import (
	"testing"

	"asmgr-desktop/session"
)

// Two templates with the same name are indistinguishable in the picker, so a
// collision is suffixed rather than allowed through.
func TestUniqueTemplateNameSuffixesCollisions(t *testing.T) {
	existing := []session.SessionTemplate{
		{ID: "a", Name: "Fejlesztés"},
		{ID: "b", Name: "Fejlesztés (2)"},
	}

	if got := uniqueTemplateName("Fejlesztés", existing, ""); got != "Fejlesztés (3)" {
		t.Errorf("name = %q, want %q", got, "Fejlesztés (3)")
	}
	if got := uniqueTemplateName("Teszt", existing, ""); got != "Teszt" {
		t.Errorf("a free name was changed to %q", got)
	}
	if got := uniqueTemplateName("  ", nil, ""); got != "Template" {
		t.Errorf("a blank name became %q", got)
	}
}

// Renaming a template must not collide with itself: editing "Fejlesztés"
// without touching its name would otherwise turn it into "Fejlesztés (2)".
func TestUniqueTemplateNameIgnoresTheEntryBeingEdited(t *testing.T) {
	existing := []session.SessionTemplate{
		{ID: "a", Name: "Fejlesztés"},
		{ID: "b", Name: "Kiadás"},
	}

	if got := uniqueTemplateName("Fejlesztés", existing, "a"); got != "Fejlesztés" {
		t.Errorf("editing renamed the template to %q", got)
	}
	// Taking another entry's name is still a collision.
	if got := uniqueTemplateName("Kiadás", existing, "a"); got != "Kiadás (2)" {
		t.Errorf("name = %q, want %q", got, "Kiadás (2)")
	}
}
