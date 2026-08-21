package session

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleInstances() ([]*Instance, []*Group) {
	groups := []*Group{
		{ID: "grp_1", Name: "Work", Color: "#ff0000"},
		{ID: "grp_2", Name: "Unused", Color: "#00ff00"},
	}
	instances := []*Instance{
		{
			ID:      "asm_claude_alpha_1",
			Name:    "alpha",
			Path:    "/home/someone/projects/alpha",
			Agent:   AgentClaude,
			Status:  StatusRunning, // must NOT survive the round trip
			GroupID: "grp_1",
			Notes:   "keep me",
			Color:   "#abcdef",
			FollowedWindows: []FollowedWindow{
				{Index: 7, Name: "tests", Agent: AgentTerminal, Notes: "tab note"},
			},
			BaseCommitSHA:   "deadbeef", // machine-specific, must be dropped
			ResumeSessionID: "resume-123",
		},
		{
			ID:    "asm_terminal_beta_2",
			Name:  "beta",
			Path:  "/home/someone/projects/beta",
			Agent: AgentTerminal,
		},
	}
	return instances, groups
}

func TestPortableRoundTripKeepsConfiguration(t *testing.T) {
	instances, groups := sampleInstances()

	var buf bytes.Buffer
	if err := WritePortable(&buf, ToPortable(instances, groups, "9.9.9")); err != nil {
		t.Fatalf("write: %v", err)
	}

	bundle, err := ReadPortable(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(bundle.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(bundle.Sessions))
	}

	alpha := bundle.Sessions[0]
	if alpha.Name != "alpha" || alpha.Notes != "keep me" || alpha.Color != "#abcdef" {
		t.Errorf("configuration lost: %+v", alpha)
	}
	if alpha.GroupName != "Work" {
		t.Errorf("group name = %q, want Work (IDs must not travel)", alpha.GroupName)
	}
	if len(alpha.Tabs) != 1 || alpha.Tabs[0].Name != "tests" {
		t.Errorf("tabs lost: %+v", alpha.Tabs)
	}

	// Only the group actually in use should be exported.
	if len(bundle.Groups) != 1 || bundle.Groups[0].Name != "Work" {
		t.Errorf("groups = %+v, want only Work", bundle.Groups)
	}
}

// Runtime state describes the exporting machine at one moment; carrying it
// over would produce sessions that look live but point at nothing.
func TestPortableDropsMachineState(t *testing.T) {
	instances, groups := sampleInstances()
	var buf bytes.Buffer
	if err := WritePortable(&buf, ToPortable(instances, groups, "")); err != nil {
		t.Fatal(err)
	}
	raw := buf.String()

	for _, leaked := range []string{"deadbeef", "resume-123", "asm_claude_alpha_1", "grp_1"} {
		if strings.Contains(raw, leaked) {
			t.Errorf("export leaked machine-specific value %q", leaked)
		}
	}
}

func TestFromPortableStartsStoppedWithFreshIdentity(t *testing.T) {
	ps := PortableSession{
		Name:  "alpha",
		Path:  "/tmp/whatever",
		Agent: AgentClaude,
		Tabs:  []PortableTab{{Name: "one"}, {Name: "two"}},
	}
	inst := ps.FromPortable("grp_local")

	if inst.Status != StatusStopped {
		t.Errorf("status = %v, want stopped: nothing is running yet on this machine", inst.Status)
	}
	if inst.ID == "" || inst.ID == "asm_claude_alpha_1" {
		t.Errorf("ID = %q, want a freshly generated one", inst.ID)
	}
	if inst.GroupID != "grp_local" {
		t.Errorf("group = %q, want the local group ID", inst.GroupID)
	}
	if inst.BaseCommitSHA != "" || inst.ResumeSessionID != "" {
		t.Error("machine-specific state was restored")
	}
	// Tab indices are assigned locally; the exporter's tmux numbers are
	// meaningless here.
	if len(inst.FollowedWindows) != 2 {
		t.Fatalf("tabs = %d, want 2", len(inst.FollowedWindows))
	}
	if inst.FollowedWindows[0].Index != 1 || inst.FollowedWindows[1].Index != 2 {
		t.Errorf("tab indices = %d,%d, want 1,2",
			inst.FollowedWindows[0].Index, inst.FollowedWindows[1].Index)
	}
}

// A file that isn't ours, or is newer than we understand, must be refused with
// an explanation rather than parsed into nonsense.
func TestReadPortableRejectsForeignFiles(t *testing.T) {
	cases := map[string]string{
		"not json":       `hello`,
		"other app":      `{"format":"something-else","version":1,"sessions":[{"name":"x"}]}`,
		"newer format":   `{"format":"asmgr-sessions","version":999,"sessions":[{"name":"x"}]}`,
		"no sessions":    `{"format":"asmgr-sessions","version":1,"sessions":[]}`,
		"empty document": ``,
	}
	for name, body := range cases {
		if _, err := ReadPortable(strings.NewReader(body)); err == nil {
			t.Errorf("%s: accepted, want an error", name)
		}
	}
}

func TestReadPortableAcceptsOlderFormat(t *testing.T) {
	// Version 0 predates the field; it must still load.
	body := `{"format":"asmgr-sessions","sessions":[{"name":"x","path":"/tmp"}]}`
	bundle, err := ReadPortable(strings.NewReader(body))
	if err != nil {
		t.Fatalf("an older export should still load: %v", err)
	}
	if len(bundle.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(bundle.Sessions))
	}
}

func TestPathExists(t *testing.T) {
	dir := t.TempDir()
	if !PathExists(dir) {
		t.Error("an existing directory was reported missing")
	}
	if PathExists(dir + "/nope") {
		t.Error("a missing directory was reported present")
	}
	if PathExists("") {
		t.Error("an empty path must not count as existing")
	}
}

func TestToPortableRecordsExportTime(t *testing.T) {
	instances, groups := sampleInstances()
	b := ToPortable(instances, groups, "1.2.3")
	if b.AppVersion != "1.2.3" {
		t.Errorf("app version = %q", b.AppVersion)
	}
	if time.Since(b.ExportedAt) > time.Minute {
		t.Errorf("export time looks wrong: %v", b.ExportedAt)
	}
}

func TestImportPortableSessionsCreatesFreshStoppedIdentityAndRemapsGroups(t *testing.T) {
	dir := t.TempDir()
	storage := &Storage{configDir: dir, configPath: filepath.Join(dir, "sessions.json")}
	existing := &Instance{ID: "source-id", Name: "alpha", Status: StatusStopped}
	existingGroup := &Group{ID: "source-group", Name: "Other"}
	if err := storage.SaveAll([]*Instance{existing}, []*Group{existingGroup}, DefaultSettings()); err != nil {
		t.Fatal(err)
	}

	portable := PortableSession{
		Name: "alpha", Path: "/tmp/source", Agent: AgentClaude, GroupName: "Work",
		Tabs: []PortableTab{{Name: "terminal", Agent: AgentTerminal}},
	}
	count, err := storage.ImportPortableSessions([]PortableSession{portable}, []PortableGroup{{
		Name: "Work", Color: "#123456",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	instances, groups, err := storage.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 2 {
		t.Fatalf("instances = %d, want 2", len(instances))
	}
	imported := instances[1]
	if imported.ID == "" || imported.ID == "source-id" {
		t.Fatalf("import reused source identity: %q", imported.ID)
	}
	if imported.Name != "alpha (2)" || imported.Status != StatusStopped {
		t.Fatalf("imported runtime/config state = %+v", imported)
	}
	if imported.ResumeSessionID != "" || imported.BaseCommitSHA != "" {
		t.Fatalf("runtime state survived import: %+v", imported)
	}
	if len(imported.FollowedWindows) != 1 || imported.FollowedWindows[0].Index != 1 {
		t.Fatalf("tab identity was not regenerated: %+v", imported.FollowedWindows)
	}
	if imported.GroupID == "" || imported.GroupID == existingGroup.ID {
		t.Fatalf("group ID was not remapped around target collision: %q", imported.GroupID)
	}
	if len(groups) != 2 || groups[1].Name != "Work" || groups[1].ID != imported.GroupID {
		t.Fatalf("unexpected imported groups: %+v", groups)
	}
}
