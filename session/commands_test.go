package session

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestUpdateCommandsKeepsConcurrentEdits(t *testing.T) {
	dir := t.TempDir()
	storages := []*Storage{{configDir: dir}, {configDir: dir}}
	const count = 40
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- storages[i%len(storages)].UpdateCommands(func(lib *CommandLibrary) error {
				lib.Commands = append(lib.Commands, SavedCommand{ID: fmt.Sprintf("cmd-%d", i), Name: "name", Command: "true"})
				return nil
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	lib, err := storages[0].LoadCommands()
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Commands) != count {
		t.Fatalf("saved %d commands, want %d", len(lib.Commands), count)
	}
}

func TestPlaceholdersFindsNamesInOrder(t *testing.T) {
	got := Placeholders(`ssh {{szerver}} 'tail -f {{logfájl}}' # {{szerver}} again`)
	want := []string{"szerver", "logfájl"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("placeholders = %v, want %v (first appearance, no duplicates)", got, want)
	}
}

// Shell syntax uses braces too. Treating those as placeholders would make the
// picker ask for "print $1", so the pattern has to stay narrow.
func TestPlaceholdersIgnoresShellBraces(t *testing.T) {
	for _, cmd := range []string{
		`echo ${HOME}`,
		`awk '{print $1}'`,
		`awk '{sum += $2} END {print sum}'`,
		`awk '{print}' file.txt`,
		`jq '{name: .name}'`,
		`sed 's/{old}/{new}/'`,
		`find . -exec rm {} \;`,
		`echo {}`,
		`sed -n '1,${p}'`,
	} {
		if got := Placeholders(cmd); len(got) != 0 {
			t.Errorf("%q: got %v, want none", cmd, got)
		}
	}
}

func TestPlaceholdersAcceptsRealisticNames(t *testing.T) {
	got := Placeholders(`docker logs {{container name}} --tail {{sor_szám}} {{branch-name}}`)
	want := []string{"container name", "sor_szám", "branch-name"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("placeholders = %v, want %v", got, want)
	}
}

func TestExpandSubstitutesValues(t *testing.T) {
	cmd := `git commit -m "{{üzenet}}" && git push {{remote}}`
	got := Expand(cmd, map[string]string{"üzenet": "javítás", "remote": "origin"})
	want := `git commit -m "javítás" && git push origin`
	if got != want {
		t.Errorf("expanded = %q, want %q", got, want)
	}
}

// The same name twice is answered once and substituted everywhere.
func TestExpandRepeatsOneValue(t *testing.T) {
	got := Expand(`cp {{file}} {{file}}.bak`, map[string]string{"file": "notes.txt"})
	if got != "cp notes.txt notes.txt.bak" {
		t.Errorf("expanded = %q", got)
	}
}

// A missing value must stay visible rather than becoming an empty string:
// "rm -rf " would be considerably worse than an obviously unfinished command.
func TestExpandLeavesUnansweredPlaceholders(t *testing.T) {
	got := Expand(`rm -rf {{célmappa}}`, map[string]string{"másik": "x"})
	if got != `rm -rf {{célmappa}}` {
		t.Errorf("expanded = %q, want the placeholder left in place", got)
	}
	if got := Expand(`echo {{a}}`, nil); got != `echo {{a}}` {
		t.Errorf("expanded with no values = %q", got)
	}
}

func TestValidateRejectsEmptyFields(t *testing.T) {
	if err := (&SavedCommand{Name: "", Command: "ls"}).Validate(); err == nil {
		t.Error("a command without a name was accepted")
	}
	if err := (&SavedCommand{Name: "list", Command: "   "}).Validate(); err == nil {
		t.Error("an empty command was accepted")
	}
	if err := (&SavedCommand{Name: "list", Command: "ls"}).Validate(); err != nil {
		t.Errorf("a valid command was rejected: %v", err)
	}
}

func TestSortCommandsPutsUsedFirst(t *testing.T) {
	now := time.Now()
	cmds := []SavedCommand{
		{Name: "zebra"},
		{Name: "alma"},
		{Name: "gyakori", UseCount: 5, UsedAt: now.Add(-time.Hour)},
		{Name: "friss", UseCount: 1, UsedAt: now},
		{Name: "régi", UseCount: 1, UsedAt: now.Add(-24 * time.Hour)},
	}
	SortCommands(cmds)
	got := []string{cmds[0].Name, cmds[1].Name, cmds[2].Name, cmds[3].Name, cmds[4].Name}
	want := []string{"gyakori", "friss", "régi", "alma", "zebra"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestCommandLibraryRoundTrips(t *testing.T) {
	dir := t.TempDir()
	s := &Storage{configDir: dir}

	// Nothing saved yet is not an error.
	lib, err := s.LoadCommands()
	if err != nil {
		t.Fatalf("loading a missing library failed: %v", err)
	}
	if len(lib.Commands) != 0 {
		t.Errorf("fresh library has %d commands", len(lib.Commands))
	}

	lib.Groups = []CommandGroup{{ID: "g1", Name: "Deploy"}}
	lib.Commands = []SavedCommand{
		{ID: "c1", Name: "logs", Command: "docker logs -f {{konténer}}", GroupID: "g1"},
	}
	if err := s.SaveCommands(lib); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := s.LoadCommands()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(loaded.Commands) != 1 || loaded.Commands[0].Command != "docker logs -f {{konténer}}" {
		t.Errorf("commands lost: %+v", loaded.Commands)
	}
	if len(loaded.Groups) != 1 || loaded.Groups[0].Name != "Deploy" {
		t.Errorf("groups lost: %+v", loaded.Groups)
	}

	// The write must not leave temporary files lying around.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "commands.json" && e.Name() != "commands.lock" {
			t.Errorf("unexpected leftover file: %s", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "commands.json")); err != nil {
		t.Errorf("commands.json missing: %v", err)
	}
}

// A corrupt file must report a readable error rather than silently losing the
// library or crashing.
func TestLoadCommandsRejectsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "commands.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Storage{configDir: dir}).LoadCommands(); err == nil {
		t.Error("a corrupt library file was accepted")
	}
}

func TestPlaceholderDefaults(t *testing.T) {
	got := PlaceholderList(`docker logs -f {{konténer}} --tail {{sorok:100}}`)
	want := []Placeholder{
		{Name: "konténer"},
		{Name: "sorok", Default: "100"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("placeholders = %+v, want %+v", got, want)
	}
}

// A default may contain spaces, dashes and punctuation — anything but the
// closing brace.
func TestPlaceholderDefaultsAllowRichValues(t *testing.T) {
	got := PlaceholderList(`git commit -m "{{üzenet:wip: mentés}}" {{flags:--no-verify}}`)
	if len(got) != 2 {
		t.Fatalf("got %d placeholders: %+v", len(got), got)
	}
	if got[0].Default != "wip: mentés" {
		t.Errorf("first default = %q, want %q", got[0].Default, "wip: mentés")
	}
	if got[1].Default != "--no-verify" {
		t.Errorf("second default = %q", got[1].Default)
	}
}

// Without a supplied value the default is used, so a command can be run
// straight from the picker without typing anything.
func TestExpandFallsBackToDefault(t *testing.T) {
	cmd := `docker logs -f {{konténer}} --tail {{sorok:100}}`
	got := Expand(cmd, map[string]string{"konténer": "web-1"})
	want := `docker logs -f web-1 --tail 100`
	if got != want {
		t.Errorf("expanded = %q, want %q", got, want)
	}
}

// An explicit value still wins over the default.
func TestExpandPrefersSuppliedValue(t *testing.T) {
	got := Expand(`tail -n {{sorok:100}} log`, map[string]string{"sorok": "5"})
	if got != "tail -n 5 log" {
		t.Errorf("expanded = %q", got)
	}
}

// A placeholder with no value AND no default must stay visible; RunCommand
// refuses on what is left rather than sending a half-built command.
func TestExpandStillFlagsMissingWithoutDefault(t *testing.T) {
	got := Expand(`rm -rf {{célmappa}} --tail {{sorok:100}}`, nil)
	if got != `rm -rf {{célmappa}} --tail 100` {
		t.Errorf("expanded = %q", got)
	}
	if names := Placeholders(got); len(names) != 1 || names[0] != "célmappa" {
		t.Errorf("remaining placeholders = %v, want [célmappa]", names)
	}
}

// The default must not turn shell syntax into a placeholder.
func TestDefaultsDoNotBreakShellBraces(t *testing.T) {
	for _, cmd := range []string{
		`echo ${HOME:-/tmp}`,
		`awk '{print $1}'`,
		`find . -exec rm {} \;`,
	} {
		if got := PlaceholderList(cmd); len(got) != 0 {
			t.Errorf("%q: got %+v, want none", cmd, got)
		}
	}
}

// Single braces are shell, awk and jq syntax. With {{name}} they can be
// pasted verbatim, which is the reason for the doubled form.
func TestSingleBracesAreNeverPlaceholders(t *testing.T) {
	for _, cmd := range []string{
		`awk '{sum += $2} END {print sum}'`,
		`jq '{name: .name}'`,
		`awk '{print}' file.txt`,
		`echo ${HOME:-/tmp}`,
	} {
		if got := PlaceholderList(cmd); len(got) != 0 {
			t.Errorf("%q: asked for %+v, want nothing", cmd, got)
		}
		if got := Expand(cmd, nil); got != cmd {
			t.Errorf("%q was rewritten to %q", cmd, got)
		}
	}
}

// Real placeholders still work beside untouched shell braces.
func TestPlaceholderNextToShellBraces(t *testing.T) {
	cmd := `awk '{print $1}' {{fájl}} --tail {{sorok:50}}`
	got := PlaceholderList(cmd)
	if len(got) != 2 || got[0].Name != "fájl" || got[1].Name != "sorok" {
		t.Fatalf("placeholders = %+v, want fájl and sorok", got)
	}
	expanded := Expand(cmd, map[string]string{"fájl": "adat.txt"})
	want := `awk '{print $1}' adat.txt --tail 50`
	if expanded != want {
		t.Errorf("expanded = %q, want %q", expanded, want)
	}
}
