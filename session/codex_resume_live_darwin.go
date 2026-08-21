//go:build darwin

package session

import (
	"context"
	"fmt"
	"strings"

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

		commandCtx, cancel := context.WithTimeout(ctx, TmuxCommandTimeout)
		out, lsofErr := CommandContext(commandCtx, "/usr/sbin/lsof", "-a", "-Fn", "-p", fmt.Sprint(current.Pid)).Output()
		cancel()
		if lsofErr == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(line, "n/") {
					paths = append(paths, line[1:])
				}
			}
		}
		if children, childErr := current.ChildrenWithContext(ctx); childErr == nil {
			queue = append(queue, children...)
		}
	}
	return detectCodexSessionIDFromOpenPaths(sessionsRoot, expectedCWD, paths)
}
