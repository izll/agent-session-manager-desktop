package store

import (
	"context"
	"fmt"
	"sort"

	"asmgr-desktop/tokenusage"
)

// Source is where a machine's logs are read: this computer's files, or a server
// through its helper.
type Source interface {
	List(ctx context.Context) ([]tokenusage.FileInfo, error)
	Read(ctx context.Context, requests []tokenusage.ReadRequest) ([]tokenusage.FileResult, error)
}

// LocalSource reads this computer's logs.
type LocalSource struct {
	Roots tokenusage.Roots
}

// List implements Source.
func (l LocalSource) List(context.Context) ([]tokenusage.FileInfo, error) {
	return tokenusage.ListFiles(l.Roots)
}

// Read implements Source.
func (l LocalSource) Read(_ context.Context, requests []tokenusage.ReadRequest) ([]tokenusage.FileResult, error) {
	return tokenusage.ReadBatch(l.Roots, requests, tokenusage.DefaultLimits), nil
}

// syncBatchFiles caps how many files one request names. The list crosses the
// wire with every call to a server, so it stays short; files with nothing new
// were dropped before this.
const syncBatchFiles = 200

// Progress reports how far a sync has come.
type Progress func(done, total int)

type pendingFile struct {
	info     tokenusage.FileInfo
	cursor   tokenusage.Cursor
	expected int64
}

// Sync brings one machine's numbers up to date, reading only what is new.
func (s *Store) Sync(ctx context.Context, machine string, source Source, progress Progress) error {
	files, err := source.List(ctx)
	if err != nil {
		return err
	}
	states, err := s.FileStates(ctx, machine)
	if err != nil {
		return err
	}

	pending := make([]*pendingFile, 0, 64)
	for _, info := range files {
		state, known := states[info.ID]
		if known && state.Size == info.Size {
			continue
		}
		file := &pendingFile{info: info, cursor: state.Cursor, expected: state.Cursor.Offset}
		if info.Size < state.Cursor.Offset {
			// Shorter than what was read means rewritten. Read again from the
			// top: a Claude row already counted is recognised by its key.
			// Codex rollouts are only ever appended to, and their rows carry
			// no key, so one that shrank is left where it is rather than
			// counted a second time.
			if agentOf(info.ID) == tokenusage.AgentCodex {
				continue
			}
			file.cursor = tokenusage.Cursor{}
		}
		pending = append(pending, file)
	}
	// Oldest first. Resuming a conversation copies its responses into a new file,
	// and whichever is read first gets the credit: it belongs to the original.
	sort.SliceStable(pending, func(i, j int) bool {
		return pending[i].info.ModMs < pending[j].info.ModMs
	})

	total := len(pending)
	done := 0
	if progress != nil {
		progress(done, total)
	}
	for len(pending) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		size := len(pending)
		if size > syncBatchFiles {
			size = syncBatchFiles
		}
		requests := make([]tokenusage.ReadRequest, size)
		byPath := make(map[string]*pendingFile, size)
		for i, file := range pending[:size] {
			requests[i] = tokenusage.ReadRequest{Path: file.info.Path, Cursor: file.cursor}
			byPath[file.info.Path] = file
		}

		results, err := source.Read(ctx, requests)
		if err != nil {
			return err
		}
		if len(results) == 0 {
			return fmt.Errorf("the log reader returned nothing for %d files", size)
		}

		finished := map[string]bool{}
		batch := make([]Applied, 0, len(results))
		for _, result := range results {
			file := byPath[result.Path]
			if file == nil {
				continue
			}
			if result.Err != "" {
				// Gone or unreadable: nothing was counted, and the next sync
				// tries again if the file is still there.
				finished[result.Path] = true
				continue
			}
			batch = append(batch, Applied{
				FileID:         file.info.ID,
				Size:           sizeWhenRead(file.info.Size, result),
				ExpectedOffset: file.expected,
				Result:         result,
			})
			file.cursor = result.Cursor
			file.expected = result.Cursor.Offset
			if !result.More {
				finished[result.Path] = true
			}
		}
		if err := s.Apply(ctx, machine, batch); err != nil {
			return err
		}

		remaining := pending[:0]
		for _, file := range pending {
			if finished[file.info.Path] {
				done++
				continue
			}
			remaining = append(remaining, file)
		}
		pending = remaining
		if progress != nil {
			progress(done, total)
		}
	}
	return nil
}

// sizeWhenRead is the size to remember. A file stopped by the limits is not
// finished, and recording its full size would make the next sync skip the rest
// if this one is cut short.
func sizeWhenRead(listed int64, result tokenusage.FileResult) int64 {
	if result.More {
		return result.Cursor.Offset
	}
	return listed
}

func agentOf(fileID string) string {
	if len(fileID) >= 6 && fileID[:6] == "codex:" {
		return tokenusage.AgentCodex
	}
	return tokenusage.AgentClaude
}
