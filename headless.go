package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"
)

// asmgr-desktop --serve: the phone's page without the desktop window, for a machine
// nobody sits at, or a user who keeps the app closed.
//
// It is the same backend: the sweep, the watchers, the terminal bridge. It serves only
// if it owns the project, which an open desktop window does, so the two never let
// devices in to the same terminals side by side.

type serveOptions struct {
	port int
	pair bool
}

func parseServeArgs(args []string) (serveOptions, error) {
	opts := serveOptions{port: defaultMobilePort}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--pair":
			opts.pair = true
		case "--port":
			if i+1 >= len(args) {
				return opts, errors.New("--port needs a number")
			}
			port, err := strconv.Atoi(args[i+1])
			if err != nil || port < 1024 || port > 65535 {
				return opts, errMobilePort
			}
			opts.port = port
			i++
		default:
			return opts, fmt.Errorf("unknown option %q (use --port N and --pair)", args[i])
		}
	}
	return opts, nil
}

// runHeadless starts the backend without a window and serves until interrupted.
func runHeadless(args []string) int {
	opts, err := parseServeArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app := NewApp()
	windowless := headlessContext(ctx)
	app.startup(windowless)
	if app.storage == nil {
		fmt.Fprintln(os.Stderr, "could not open the settings; see the log")
		return 1
	}
	code := serveHeadless(ctx, app, opts, os.Stdout)
	app.shutdown(windowless)
	return code
}

// serveHeadless serves the phone's page on an app already started, until ctx ends.
// It does not change the desktop's own setting: serving from the command line is a
// choice for this run.
func serveHeadless(ctx context.Context, app *App, opts serveOptions, out io.Writer) int {
	if !app.projectLocked {
		fmt.Fprintf(out, "Another asmgr (pid %d) has this project open. Close it, or turn mobile access on there.\n", app.otherInstancePID)
		return 1
	}
	if err := app.startMobile(opts.port); err != nil {
		fmt.Fprintf(out, "Could not serve on 127.0.0.1:%d: %v\n", opts.port, err)
		return 1
	}
	defer app.stopMobile()
	fmt.Fprintf(out, "Serving the phone page on http://127.0.0.1:%d\n", opts.port)
	fmt.Fprintf(out, "To reach it from a phone: tailscale serve --bg http://127.0.0.1:%d\n", opts.port)
	if opts.pair {
		pairing, err := app.CreateMobilePairing()
		if err != nil {
			fmt.Fprintf(out, "Could not make a pairing code: %v\n", err)
			return 1
		}
		fmt.Fprintf(out, "Pair a phone (once, until %s): %s\n", pairing.ExpiresAt.Local().Format("15:04"), pairing.URL)
	}
	<-ctx.Done()
	return 0
}
