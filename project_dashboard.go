package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"asmgr-desktop/session"
)

const (
	projectDashboardTimeout = 12 * time.Second
	projectGitRepoTimeout   = 3 * time.Second
	projectGitWorkers       = 6
)

// ProjectGitSummary is the dashboard's Git snapshot for one session.
// Sessions sharing the same path receive the same repository snapshot while
// retaining their own session ID.
type ProjectGitSummary struct {
	SessionID         string `json:"sessionId"`
	Path              string `json:"path"`
	Repository        bool   `json:"repository"`
	RepositoryRoot    string `json:"repositoryRoot"`
	Branch            string `json:"branch"`
	Upstream          string `json:"upstream"`
	Dirty             bool   `json:"dirty"`
	ModifiedFiles     int    `json:"modifiedFiles"`
	Ahead             int    `json:"ahead"`
	Behind            int    `json:"behind"`
	LastCommitHash    string `json:"lastCommitHash"`
	LastCommitMessage string `json:"lastCommitMessage"`
	LastCommitAuthor  string `json:"lastCommitAuthor"`
	LastCommitAt      string `json:"lastCommitAt"`
	Error             string `json:"error,omitempty"`
}

type projectGitPathResult struct {
	path    string
	summary ProjectGitSummary
}

// collectProjectGitSummaries collects Git data with bounded concurrency and
// preserves the input session order. Git failures never fail the whole batch.
func collectProjectGitSummaries(parent context.Context, instances []*session.Instance) []ProjectGitSummary {
	return collectProjectGitSummariesWithInspector(parent, instances, inspectGitRepository)
}

type projectGitInspector func(context.Context, string) ProjectGitSummary

func collectProjectGitSummariesWithInspector(parent context.Context, instances []*session.Instance, inspect projectGitInspector) []ProjectGitSummary {
	if len(instances) == 0 {
		return []ProjectGitSummary{}
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, projectDashboardTimeout)
	defer cancel()

	type pathGroup struct {
		path    string
		indices []int
	}
	groups := make(map[string]*pathGroup, len(instances))
	orderedGroups := make([]*pathGroup, 0, len(instances))
	for idx, inst := range instances {
		path := normalizedDashboardPath(inst.Path)
		group, ok := groups[path]
		if !ok {
			group = &pathGroup{path: path}
			groups[path] = group
			orderedGroups = append(orderedGroups, group)
		}
		group.indices = append(group.indices, idx)
	}

	jobs := make(chan string, len(orderedGroups))
	results := make(chan projectGitPathResult, len(orderedGroups))
	for _, group := range orderedGroups {
		jobs <- group.path
	}
	close(jobs)

	workerCount := min(projectGitWorkers, len(orderedGroups))
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for path := range jobs {
				repoCtx, repoCancel := context.WithTimeout(ctx, projectGitRepoTimeout)
				summary := inspect(repoCtx, path)
				repoCancel()
				results <- projectGitPathResult{path: path, summary: summary}
			}
		}()
	}
	go func() {
		workers.Wait()
		close(results)
	}()

	byPath := make(map[string]ProjectGitSummary, len(orderedGroups))
	for result := range results {
		byPath[result.path] = result.summary
	}

	summaries := make([]ProjectGitSummary, len(instances))
	for path, group := range groups {
		base := byPath[path]
		for _, idx := range group.indices {
			summaries[idx] = base
			summaries[idx].SessionID = instances[idx].ID
			summaries[idx].Path = instances[idx].Path
		}
	}
	return summaries
}

func normalizedDashboardPath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return filepath.Clean(abs)
}

func inspectGitRepository(ctx context.Context, path string) ProjectGitSummary {
	summary := ProjectGitSummary{Path: path}
	if path == "" {
		summary.Error = "session path is empty"
		return summary
	}

	output, err := runDashboardGit(ctx, path, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(output) != "true" {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			summary.Error = "git inspection timed out"
		} else if err != nil && !strings.Contains(strings.ToLower(output), "not a git repository") {
			summary.Error = "git repository check failed"
		}
		return summary
	}
	summary.Repository = true
	if output, err = runDashboardGit(ctx, path, "rev-parse", "--show-toplevel"); err == nil {
		summary.RepositoryRoot = strings.TrimSpace(output)
	}

	if output, err = runDashboardGit(ctx, path, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		summary.Branch = strings.TrimSpace(output)
	} else if output, err = runDashboardGit(ctx, path, "rev-parse", "--short", "HEAD"); err == nil {
		summary.Branch = "detached@" + strings.TrimSpace(output)
	}

	if output, err = runDashboardGit(ctx, path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		summary.Upstream = strings.TrimSpace(output)
		if counts, countErr := runDashboardGit(ctx, path, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); countErr == nil {
			fields := strings.Fields(counts)
			if len(fields) == 2 {
				summary.Ahead, _ = strconv.Atoi(fields[0])
				summary.Behind, _ = strconv.Atoi(fields[1])
			}
		}
	}

	if output, err = runDashboardGit(ctx, path, "status", "--porcelain=v1", "-z", "--untracked-files=all"); err != nil {
		summary.addError("git status", err)
	} else {
		records := strings.Split(output, "\x00")
		for idx := 0; idx < len(records); idx++ {
			record := records[idx]
			if len(record) < 3 {
				continue
			}
			summary.ModifiedFiles++
			// With -z, rename/copy entries contain one extra NUL-delimited
			// source path. It belongs to the same changed file.
			if record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C' {
				idx++
			}
		}
		summary.Dirty = summary.ModifiedFiles > 0
	}

	const logFormat = "%h%x00%s%x00%an%x00%cI"
	if output, err = runDashboardGit(ctx, path, "log", "-1", "--format="+logFormat); err == nil {
		parts := strings.SplitN(strings.TrimSuffix(output, "\n"), "\x00", 4)
		if len(parts) == 4 {
			summary.LastCommitHash = parts[0]
			summary.LastCommitMessage = parts[1]
			summary.LastCommitAuthor = parts[2]
			summary.LastCommitAt = parts[3]
		}
	}

	if ctx.Err() != nil {
		summary.addError("git inspection", ctx.Err())
	}
	return summary
}

func runDashboardGit(ctx context.Context, path string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", path}, args...)...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func (s *ProjectGitSummary) addError(operation string, err error) {
	if err == nil {
		return
	}
	message := fmt.Sprintf("%s: %v", operation, err)
	if s.Error == "" {
		s.Error = message
		return
	}
	s.Error += "; " + message
}
