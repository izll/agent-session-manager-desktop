package main

import (
	"os"
	"strings"
	"testing"
)

// The Windows installer is per-user, and every part of it has to agree on that.
//
// It was machine-wide, into Program Files. The app updates itself by writing the
// new build beside the running executable and swapping it, which Program Files
// does not allow without elevation — the update failed with "Access is denied"
// and the only way forward was to download the installer by hand. Elevating for
// each update would also mean a UAC prompt every time, ruling out the quiet
// background update the other platforms get.
//
// Getting one of these settings out of step is worse than not changing them at
// all: an installer that writes to HKLM without elevation fails silently,
// leaving an app with no entry in Add/Remove Programs.
func TestWindowsInstallerIsPerUser(t *testing.T) {
	b, err := os.ReadFile("build/windows/installer/project.nsi")
	if err != nil {
		t.Fatalf("reading project.nsi: %v", err)
	}
	src := string(b)

	if !strings.Contains(src, "RequestExecutionLevel user") {
		t.Error("the installer still asks for elevation; a per-user install needs none")
	}
	if strings.Contains(src, "$PROGRAMFILES64") || strings.Contains(src, "$PROGRAMFILES\\") {
		t.Error("the installer still targets Program Files, which the self-update " +
			"cannot write to without administrator rights")
	}
	if !strings.Contains(src, "$LOCALAPPDATA") {
		t.Error("the installer does not install under LocalAppData")
	}
	if !strings.Contains(src, "SetShellVarContext current") {
		t.Error("shortcuts would go to all users' Start menu, which needs elevation")
	}
	if strings.Contains(src, `InstallDirRegKey HKLM`) {
		t.Error("the install location is looked up in HKLM, where this installer " +
			"never writes; an upgrade would not find the existing install")
	}
}

// The uninstall entry has to land where a non-elevated installer can write it.
func TestUninstallEntryIsPerUser(t *testing.T) {
	b, err := os.ReadFile("build/windows/installer/wails_tools.nsh")
	if err != nil {
		t.Fatalf("reading wails_tools.nsh: %v", err)
	}

	for i, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, "#") {
			continue // a mention in a comment
		}
		if !strings.Contains(line, "HKLM") {
			continue
		}
		// Reading the machine-wide WebView2 version is correct: that component
		// is installed system-wide and is only read, never written.
		if strings.Contains(line, "ReadRegStr") && strings.Contains(line, "EdgeUpdate") {
			continue
		}
		t.Errorf("wails_tools.nsh:%d writes to HKLM; without elevation that fails "+
			"silently and the app gets no Add/Remove Programs entry:\n  %s",
			i+1, strings.TrimSpace(line))
	}
}
