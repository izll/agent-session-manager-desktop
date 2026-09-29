// Package diffhidden remembers which files the diff view keeps out of sight.
//
// Hiding is a matter of the view only. Nothing here touches git: no
// .gitignore, no assume-unchanged, no skip-worktree. The file still has its
// changes, `git status` still shows them, and a commit still takes them — the
// diff simply stops listing them among the files to review.
//
// The rules are kept per repository, keyed by the top of the working tree, so
// every session and tab in the same checkout shares them. A worktree is a
// checkout of its own and keeps its own list.
package diffhidden

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// FileName is the store's file inside the app's configuration directory.
const FileName = "diff_hidden.json"

// MaxRuleLength bounds one rule. A path longer than this is not one a person
// typed or clicked.
const MaxRuleLength = 1024

// MaxRulesPerRepo bounds one repository's list, so a runaway caller cannot
// grow the file without limit.
const MaxRulesPerRepo = 1000

// maxFileSize bounds the read. The store holds short strings; anything this
// large is not ours.
const maxFileSize = 8 << 20

// ErrInvalidRule reports a rule that is empty or cannot be a path.
var ErrInvalidRule = errors.New("error.diffHiddenInvalidRule")

// ErrNoRepository reports a directory that is not in a git repository, so
// there is nothing to key the rules on.
var ErrNoRepository = errors.New("error.diffHiddenNoRepository")

// ErrTooManyRules reports a repository whose list is full.
var ErrTooManyRules = errors.New("error.diffHiddenTooManyRules")

type fileFormat struct {
	Version int                 `json:"version"`
	Repos   map[string][]string `json:"repos"`
}

// Store reads and writes the rules file. Every change is read-modify-write
// under one lock, so two diff views hiding files at the same moment cannot
// drop each other's rule.
type Store struct {
	path string
	mu   sync.Mutex
}

// New returns a store kept in dir.
func New(dir string) *Store {
	return &Store{path: filepath.Join(dir, FileName)}
}

// DefaultDir is the app's configuration directory — the same one the session
// store and the updater use.
func DefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "agent-session-manager-desktop")
}

// NormaliseRule puts a rule in the one form it is stored and matched in:
// forward slashes, relative to the repository, no leading "./" or "/". A
// trailing slash is kept — it is what marks a folder.
//
// The frontend applies the same steps before it asks; doing them here as well
// means a rule stored from anywhere compares equal to the one that removes it.
func NormaliseRule(rule string) (string, error) {
	rule = strings.TrimSpace(strings.ReplaceAll(rule, "\\", "/"))
	for strings.HasPrefix(rule, "./") {
		rule = rule[2:]
	}
	rule = strings.TrimLeft(rule, "/")
	for strings.Contains(rule, "//") {
		rule = strings.ReplaceAll(rule, "//", "/")
	}
	if rule == "" || len(rule) > MaxRuleLength || strings.ContainsAny(rule, "\x00\r\n") {
		return "", ErrInvalidRule
	}
	return rule, nil
}

// Rules returns the repository's rules, sorted. None is an empty list, not
// an error.
func (s *Store) Rules(repo string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return []string{}, err
	}
	return cloneRules(data.Repos[repo]), nil
}

// Add stores one rule for the repository and returns the list as it now
// stands. A rule already present is not added twice.
func (s *Store) Add(repo, rule string) ([]string, error) {
	if repo == "" {
		return nil, ErrNoRepository
	}
	rule, err := NormaliseRule(rule)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return nil, err
	}
	rules := data.Repos[repo]
	for _, have := range rules {
		if have == rule {
			return cloneRules(rules), nil
		}
	}
	if len(rules) >= MaxRulesPerRepo {
		return cloneRules(rules), ErrTooManyRules
	}
	data.Repos[repo] = append(rules, rule)
	if err := s.save(data); err != nil {
		return nil, err
	}
	return cloneRules(data.Repos[repo]), nil
}

// Remove drops the given rules from the repository and returns what is left.
// A rule that is not there is ignored: showing a file again that something
// else already showed is not a failure.
func (s *Store) Remove(repo string, remove []string) ([]string, error) {
	if repo == "" {
		return nil, ErrNoRepository
	}
	drop := make(map[string]bool, len(remove))
	for _, rule := range remove {
		if normal, err := NormaliseRule(rule); err == nil {
			drop[normal] = true
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return nil, err
	}
	kept := make([]string, 0, len(data.Repos[repo]))
	for _, rule := range data.Repos[repo] {
		if !drop[rule] {
			kept = append(kept, rule)
		}
	}
	if len(kept) == len(data.Repos[repo]) {
		return cloneRules(kept), nil
	}
	if len(kept) == 0 {
		delete(data.Repos, repo)
	} else {
		data.Repos[repo] = kept
	}
	if err := s.save(data); err != nil {
		return nil, err
	}
	return cloneRules(kept), nil
}

func cloneRules(rules []string) []string {
	out := append([]string{}, rules...)
	sort.Strings(out)
	return out
}

// load reads the file. A missing file is an empty store; an unreadable or
// malformed one is an error rather than an empty store, because saving over
// it would throw away every repository's rules for the sake of one change.
func (s *Store) load() (*fileFormat, error) {
	data := &fileFormat{Version: 1, Repos: map[string][]string{}}
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return data, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFileSize {
		return nil, fmt.Errorf("%s is too large", FileName)
	}
	if err := json.NewDecoder(f).Decode(data); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	if data.Repos == nil {
		data.Repos = map[string][]string{}
	}
	return data, nil
}

// save writes beside the target and renames over it, so a crash mid-write
// cannot leave half a file behind.
func (s *Store) save(data *fileFormat) error {
	data.Version = 1
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, FileName+".*.tmp")
	if err != nil {
		return err
	}
	_, writeErr := tmp.Write(append(encoded, '\n'))
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(tmp.Name())
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
