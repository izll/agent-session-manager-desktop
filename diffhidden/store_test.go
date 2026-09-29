package diffhidden

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestRulesRoundTripThroughTheFile(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	if got, err := store.Rules("/repo"); err != nil || len(got) != 0 {
		t.Fatalf("empty store: got %v, %v", got, err)
	}
	if _, err := store.Add("/repo", "src/gen/"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("/repo", "package-lock.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("/other", "*.lock"); err != nil {
		t.Fatal(err)
	}

	// A fresh store reads what the first one wrote.
	reread := New(dir)
	got, err := reread.Rules("/repo")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"package-lock.json", "src/gen/"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rules for /repo = %v, want %v", got, want)
	}
	if got, _ := reread.Rules("/other"); !reflect.DeepEqual(got, []string{"*.lock"}) {
		t.Fatalf("each repository keeps its own list; /other = %v", got)
	}
}

func TestAddNormalisesAndDoesNotDuplicate(t *testing.T) {
	store := New(t.TempDir())
	for _, rule := range []string{"./src\\gen/", "/src/gen/", "  src//gen/  "} {
		if _, err := store.Add("/repo", rule); err != nil {
			t.Fatalf("Add(%q): %v", rule, err)
		}
	}
	got, _ := store.Rules("/repo")
	if !reflect.DeepEqual(got, []string{"src/gen/"}) {
		t.Fatalf("got %v, want one normalised rule", got)
	}
}

func TestInvalidRulesAreRefused(t *testing.T) {
	store := New(t.TempDir())
	for _, rule := range []string{"", "   ", "/", "./", "a\nb", "a\x00b", strings.Repeat("a", MaxRuleLength+1)} {
		if _, err := store.Add("/repo", rule); !errors.Is(err, ErrInvalidRule) {
			t.Errorf("Add(%q) = %v, want ErrInvalidRule", rule, err)
		}
	}
	if _, err := store.Add("", "a.txt"); !errors.Is(err, ErrNoRepository) {
		t.Errorf("no repository: %v", err)
	}
}

func TestRemoveShowsAgainAndForgetsAnEmptyRepository(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	store.Add("/repo", "a.txt")
	store.Add("/repo", "build/")

	left, err := store.Remove("/repo", []string{"./a.txt", "not-there"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(left, []string{"build/"}) {
		t.Fatalf("left = %v", left)
	}
	if _, err := store.Remove("/repo", []string{"build/"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "/repo") {
		t.Fatalf("an emptied repository should leave the file: %s", raw)
	}
}

func TestAMalformedFileIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := New(dir)
	if _, err := store.Add("/repo", "a.txt"); err == nil {
		t.Fatal("adding to an unreadable store should fail rather than replace it")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "{not json" {
		t.Fatalf("file was rewritten: %q", raw)
	}
}

func TestTheListIsBounded(t *testing.T) {
	store := New(t.TempDir())
	for i := 0; i < MaxRulesPerRepo; i++ {
		if _, err := store.Add("/repo", "f"+strconv.Itoa(i)); err != nil {
			t.Fatalf("rule %d: %v", i, err)
		}
	}
	if _, err := store.Add("/repo", "one-more"); !errors.Is(err, ErrTooManyRules) {
		t.Fatalf("got %v, want ErrTooManyRules", err)
	}
}

func TestConcurrentAddsKeepEveryRule(t *testing.T) {
	store := New(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := store.Add("/repo", "file"+strconv.Itoa(i)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	got, _ := store.Rules("/repo")
	if len(got) != 20 {
		t.Fatalf("got %d rules, want 20: %v", len(got), got)
	}
}
