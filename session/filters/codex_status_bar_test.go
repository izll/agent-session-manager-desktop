package filters

import "testing"

func codexFilter() *FilterConfig { return getDefaultFilters()["codex"] }

// The bar was matched by listing model-name prefixes, so every model OpenAI
// shipped leaked into the session's status line until someone added it. This
// is the one that was reported: "gpt-6-astra medium · ~/... · Main [default]"
// shown as a session's status.
func TestCodexStatusBarIsSkippedForAnyModel(t *testing.T) {
	for _, line := range []string{
		"gpt-6-astra medium · ~/NetBeansProjects/nesting-project · Main [default]",
		"gpt-5.5 high · ~/some/path · main",
		"gpt-5-codex low · /home/izll/project · feature/x",
		"o4-mini high · ~/work · main",
		"some-model-nobody-has-shipped-yet high · ~/work · main",
	} {
		if skip, _ := ApplyFilter(codexFilter(), line); !skip {
			t.Errorf("the bottom bar was shown as a status line: %q", line)
		}
	}
}

// The bar is recognised by shape, and shape alone would take prose with it.
// A path is what separates chrome from something worth reading.
func TestCodexProseWithDotsIsKept(t *testing.T) {
	for _, line := range []string{
		"Elkészült a javítás · a tesztek zöldek · commitolható",
		"Done · 3 files changed · ready for review",
	} {
		if skip, _ := ApplyFilter(codexFilter(), line); skip {
			t.Errorf("a real status line was filtered out as chrome: %q", line)
		}
	}
}

// What the user actually wants to see must survive.
func TestCodexWorkLinesAreKept(t *testing.T) {
	for _, line := range []string{
		"• Waiting for agents",
		"Read improve.rs",
		"Search compact_with_config_impl( in improve.rs",
	} {
		if skip, _ := ApplyFilter(codexFilter(), line); skip {
			t.Errorf("a work line was filtered out: %q", line)
		}
	}
}

// Two fields are not the bar, even with a path: that shape shows up in
// ordinary output.
func TestCodexTwoFieldsAreNotTheBar(t *testing.T) {
	line := "wrote /home/izll/out.txt · 4 lines"
	if skip, _ := ApplyFilter(codexFilter(), line); skip {
		t.Errorf("a two-field line was taken for the bottom bar: %q", line)
	}
}
