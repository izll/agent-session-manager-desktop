//go:build windows

package session

import (
	"context"

	"github.com/shirou/gopsutil/v4/process"
)

func detectCodexSessionIDFromLiveProcessTree(ctx context.Context, sessionsRoot string, rootPID int, expectedCWD string) string {
	root, err := process.NewProcessWithContext(ctx, int32(rootPID))
	if err != nil {
		return ""
	}
	queue := []*process.Process{root}
	seen := make(map[int32]struct{})
	var paths []string
	for len(queue) > 0 && ctx.Err() == nil {
		current := queue[0]
		queue = queue[1:]
		if _, ok := seen[current.Pid]; ok {
			continue
		}
		seen[current.Pid] = struct{}{}
		if files, fileErr := current.OpenFilesWithContext(ctx); fileErr == nil {
			for _, file := range files {
				paths = append(paths, file.Path)
			}
		}
		if children, childErr := current.ChildrenWithContext(ctx); childErr == nil {
			queue = append(queue, children...)
		}
	}
	return detectCodexSessionIDFromOpenPaths(sessionsRoot, expectedCWD, paths)
}
