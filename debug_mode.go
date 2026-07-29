package main

import (
	"os"
	"strings"
)

// debugEnabled turns on verbose diagnostics in a build the user already has.
//
// The alternative — shipping a separate debug build — means the person hitting
// the bug has to install a different binary to describe it, which is the point
// at which most reports stop. This is the same binary, one flag away.
//
// Accepted as either a flag or an environment variable, because a desktop app
// is not always launched from a shell: ASMGR_DEBUG=1 works from a .desktop
// file or a shortcut's properties, --debug works from a terminal.
var debugEnabled = resolveDebugFlag(os.Args[1:], os.Getenv("ASMGR_DEBUG"))

func resolveDebugFlag(args []string, env string) bool {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "1", "true", "yes", "on":
		return true
	}
	for _, a := range args {
		// --debug and -debug, with or without an explicit value.
		switch strings.ToLower(a) {
		case "--debug", "-debug", "--debug=true", "-debug=true":
			return true
		}
	}
	return false
}
