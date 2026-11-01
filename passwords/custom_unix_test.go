//go:build !windows

package passwords

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// script writes an executable shell script, as a stand-in for a password tool.
func script(t *testing.T, body string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(file, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return file
}

func customManager(t *testing.T, list, get string) (*Manager, string) {
	t.Helper()
	manager := newManager(t, nil)
	id, err := manager.SaveCustom("", "Tool", list, get)
	if err != nil {
		t.Fatal(err)
	}
	return manager, customSourcePrefix + id
}

func TestACustomSourceListsAndFetches(t *testing.T) {
	list := script(t, `printf 'Mail\nWork/Prod DB\n'`)
	get := script(t, `printf 'pw-for-%s\n' "$1"`)
	manager, source := customManager(t, list, get+" {name}")
	ctx := context.Background()
	entries, failures := manager.List(ctx, false)
	if len(failures) != 0 || len(entries) != 2 {
		t.Fatalf("entries = %+v, failures = %v", entries, failures)
	}
	if entries[1].Name != "Prod DB" || entries[1].Group != "Work" || entries[1].ID != "Work/Prod DB" || entries[1].Source != source {
		t.Fatalf("entry = %+v", entries[1])
	}
	secret, err := manager.Secret(ctx, source, "Work/Prod DB")
	if err != nil || secret != "pw-for-Work/Prod DB" {
		t.Fatalf("secret = %q, %v", secret, err)
	}
}

// A name is the tool's data, and can come from anywhere its vault syncs from.
func TestANameIsNeverCode(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	name := `$(touch ` + marker + `); touch ` + marker + ` 'x"`
	list := script(t, `printf '%s\n' "$(cat <<'NAME'
`+name+`
NAME
)"`)
	got := filepath.Join(t.TempDir(), "argument")
	get := script(t, `printf '%s' "$1" > `+got+`; echo pw`)
	manager, source := customManager(t, list, get+" {name}")
	ctx := context.Background()
	manager.List(ctx, false)
	if _, err := manager.Secret(ctx, source, name); err != nil {
		t.Fatal(err)
	}
	received, _ := os.ReadFile(got)
	if string(received) != name {
		t.Fatalf("argument = %q", received)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the name was run")
	}
}

func TestTheNameIsAlsoInTheEnvironment(t *testing.T) {
	list := script(t, `echo Mail`)
	get := script(t, `printf 'pw-%s' "$ASMGR_SECRET_NAME"`)
	manager, source := customManager(t, list, "sh -c '"+get+" # ASMGR_SECRET_NAME'")
	ctx := context.Background()
	manager.List(ctx, false)
	if secret, err := manager.Secret(ctx, source, "Mail"); err != nil || secret != "pw-Mail" {
		t.Fatalf("secret = %q, %v", secret, err)
	}
}

func TestOnlyAListedNameCanBeAskedFor(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	manager, source := customManager(t, script(t, `echo Mail`), script(t, `touch `+marker)+" {name}")
	ctx := context.Background()
	manager.List(ctx, false)
	if _, err := manager.Secret(ctx, source, "Other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the command ran")
	}
}

func TestANameThatLooksLikeAnOptionIsNotOffered(t *testing.T) {
	manager, _ := customManager(t, script(t, `printf -- '--delete\nMail\nbad\tname\n\n'`), script(t, `echo x`)+" {name}")
	entries, _ := manager.List(context.Background(), false)
	if len(entries) != 1 || entries[0].Name != "Mail" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestAFailureIsReportedWithoutItsOutput(t *testing.T) {
	const leaked = "hunter2-from-stdout"
	manager, source := customManager(t,
		script(t, `echo `+leaked+`; echo also-stderr >&2; exit 3`),
		script(t, `echo x`)+" {name}")
	entries, failures := manager.List(context.Background(), false)
	if len(entries) != 0 {
		t.Fatalf("entries = %+v", entries)
	}
	if failures[source] != ErrToolFailed.Error() {
		t.Fatalf("failures = %v", failures)
	}
}

func TestAFailedFetchTypesNothing(t *testing.T) {
	manager, source := customManager(t, script(t, `echo Mail`), script(t, `echo half-a-password; exit 1`)+" {name}")
	ctx := context.Background()
	manager.List(ctx, false)
	if secret, err := manager.Secret(ctx, source, "Mail"); !errors.Is(err, ErrToolFailed) || secret != "" {
		t.Fatalf("secret = %q, %v", secret, err)
	}
}

// One tool in trouble leaves the rest of the list.
func TestOneSourceFailingLeavesTheOthers(t *testing.T) {
	manager, source := customManager(t, script(t, `exit 1`), script(t, `echo x`)+" {name}")
	if _, err := manager.SaveCustom("", "Good", script(t, `echo Mail`), script(t, `echo x`)+" {name}"); err != nil {
		t.Fatal(err)
	}
	entries, failures := manager.List(context.Background(), false)
	if len(entries) != 1 || failures[source] == "" {
		t.Fatalf("entries = %+v, failures = %v", entries, failures)
	}
}

// A hung tool, with a child that would go on holding the output.
func TestATimeoutEndsTheToolAndWhatItStarted(t *testing.T) {
	pids := filepath.Join(t.TempDir(), "pid")
	manager, source := customManager(t, script(t, `echo Mail`),
		script(t, `sleep 100 & echo $! > `+pids+`; wait`)+" {name}")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	manager.List(ctx, false)
	short, stop := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer stop()
	started := time.Now()
	if _, err := manager.Secret(short, source, "Mail"); !errors.Is(err, ErrToolFailed) {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("took %v", elapsed)
	}
	data, err := os.ReadFile(pids)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatal("the child survived")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// What gpg or rbw do when the first command starts their agent.
func TestAHelperLeftRunningDoesNotLoseTheAnswer(t *testing.T) {
	original := customPipeGrace
	customPipeGrace = 100 * time.Millisecond
	t.Cleanup(func() { customPipeGrace = original })
	manager, source := customManager(t, script(t, `echo Mail`), script(t, `sleep 3 & echo pw; exit 0`)+" {name}")
	ctx := context.Background()
	manager.List(ctx, false)
	started := time.Now()
	if secret, err := manager.Secret(ctx, source, "Mail"); err != nil || secret != "pw" {
		t.Fatalf("secret = %q, %v", secret, err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("took %v", elapsed)
	}
}

func TestTooMuchOutputIsStoppedAndRefused(t *testing.T) {
	manager, source := customManager(t, script(t, `echo Mail`), script(t, `yes aaaaaaaaaaaaaaaa`)+" {name}")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	manager.List(ctx, false)
	if secret, err := manager.Secret(ctx, source, "Mail"); !errors.Is(err, ErrToolFailed) || secret != "" {
		t.Fatalf("secret len %d, %v", len(secret), err)
	}
	if ctx.Err() != nil {
		t.Fatal("ran until the timeout")
	}
}

// Finished before anything could stop it: the size alone has to refuse the answer.
func TestTooMuchOutputFromAToolThatSucceededIsRefused(t *testing.T) {
	stopped := func() {}
	output := &limitedOutput{limit: 4, stop: stopped}
	if _, err := output.Write([]byte("12345")); err != nil || !output.overflow || output.data != nil {
		t.Fatalf("output = %+v, %v", output, err)
	}
	manager, source := customManager(t, script(t, `echo Mail`), script(t, `head -c 70000 /dev/zero | tr '\0' a; exit 0`)+" {name}")
	ctx := context.Background()
	manager.List(ctx, false)
	// Killed or finished, what it wrote is never a password.
	if secret, err := manager.Secret(ctx, source, "Mail"); !errors.Is(err, ErrToolFailed) || secret != "" {
		t.Fatalf("secret len %d, %v", len(secret), err)
	}
}

// Nothing is offered on standard input, and a tool that reads it must not wait.
func TestAToolReadingItsInputSeesNothing(t *testing.T) {
	manager, source := customManager(t, script(t, `echo Mail`), script(t, `cat; echo done`)+" {name}")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	manager.List(ctx, false)
	if secret, err := manager.Secret(ctx, source, "Mail"); err != nil || secret != "done" {
		t.Fatalf("secret = %q, %v", secret, err)
	}
}

func TestLockingForgetsTheNames(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "count")
	manager, _ := customManager(t, script(t, `echo x >> `+counter+`; echo Mail`), script(t, `echo x`)+" {name}")
	ctx := context.Background()
	manager.List(ctx, false)
	manager.List(ctx, false)
	count := func() int {
		data, _ := os.ReadFile(counter)
		return strings.Count(string(data), "x")
	}
	if count() != 1 {
		t.Fatalf("asked %d times with a list in hand", count())
	}
	manager.Lock()
	manager.List(ctx, false)
	if count() != 2 {
		t.Fatalf("asked %d times after locking", count())
	}
}

// A tool that hangs must not hold up locking or the rest of the app.
func TestAHungToolDoesNotHoldTheLock(t *testing.T) {
	manager, source := customManager(t, script(t, `echo Mail`), script(t, `sleep 5`)+" {name}")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.List(ctx, false)
	done := make(chan struct{})
	go func() {
		manager.Secret(ctx, source, "Mail")
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	locked := make(chan struct{})
	go func() {
		manager.Lock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(2 * time.Second):
		t.Fatal("Lock waited for the tool")
	}
	cancel()
	<-done
}
