package session

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadImageForBrowseReturnsADataURL(t *testing.T) {
	root := t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\nnot really an image")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "Logo.PNG"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	inst := &Instance{Path: root}

	got, err := inst.ReadImageForBrowse("docs/Logo.PNG")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A README can name any path. Only images inside the browsed directory are
// handed to the page.
func TestReadImageForBrowseRefusals(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	for _, name := range []string{"notes.txt", "pic.png"} {
		if err := os.WriteFile(filepath.Join(outside, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "pic.png"), filepath.Join(root, "leak.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir.png"), 0o755); err != nil {
		t.Fatal(err)
	}
	big, err := os.Create(filepath.Join(root, "huge.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := big.Truncate(MaxBrowseImageBytes + 1); err != nil {
		t.Fatal(err)
	}
	big.Close()
	inst := &Instance{Path: root}

	cases := map[string]string{
		"notes.txt": "not an image",
		"../" + filepath.Base(outside) + "/pic.png": "escapes",
		filepath.Join(outside, "pic.png"):           "relative",
		"leak.png":                                  "",
		"dir.png":                                   "not a regular file",
		"huge.png":                                  "too large",
		"missing.png":                               "could not open",
		"":                                          "no file given",
	}
	for path, wantErr := range cases {
		got, err := inst.ReadImageForBrowse(path)
		if err == nil {
			t.Errorf("%q was read (%d bytes), want it refused", path, len(got))
			continue
		}
		if wantErr != "" && !strings.Contains(err.Error(), wantErr) {
			t.Errorf("%q: error %q, want it to mention %q", path, err, wantErr)
		}
	}
}
