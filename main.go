package main

import (
	"context"
	"embed"
	_ "embed"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

func main() {
	// Normalize TERM for everything we spawn. Launched from a desktop menu /
	// KRunner (rather than a shell) the process inherits TERM=dumb or an empty
	// TERM, which makes tmux refuse to attach with
	//   "open terminal failed: terminal does not support clear".
	// Set a sane value once here so every child (tmux new-session/new-window,
	// attach-session) inherits it. A real terminal launch already exports a
	// usable TERM, which we leave untouched.
	if t := os.Getenv("TERM"); t == "" || t == "dumb" {
		os.Setenv("TERM", "xterm-256color")
	}

	// Route logs: full output to a per-launch log file, only whitelisted
	// prefixes mirrored to stderr (keeps the console readable).
	if lf := setupLogging(); lf != nil {
		defer lf.Close()
	}
	log.SetOutput(logOut)

	// Create an instance of the app structure
	app := NewApp()

	// Create dictation service instance for binding
	dictationService := NewDictationService()

	// Dev mode: modify icon and title
	appTitle := "Agent Session Manager"
	appIcon := icon
	if isDevMode {
		appTitle = "[DEV] Agent Session Manager"
		appIcon = addDevBadge(icon)
	}

	// Force X11 backend on Wayland to ensure frameless mode works correctly.
	// Under native Wayland, compositors ignore gtk_window_set_decorated(FALSE)
	// and add their own titlebar on top of our custom one. XWayland respects it.
	if os.Getenv("GDK_BACKEND") == "" {
		os.Setenv("GDK_BACKEND", "x11")
	}

	// On machines without a usable GPU stack (VMs, headless/remote sessions,
	// missing or broken drivers) WebKit fails with "Could not create GBM EGL
	// display: EGL_NOT_INITIALIZED" and aborts the whole process from inside
	// cgo — too late for Go to recover. Detect that case up front and fall
	// back to software rendering so the app starts instead of crashing.
	applyGpuFallback()

	// NOTE: an earlier experiment forced SOFTWARE rendering here
	// (LIBGL_ALWAYS_SOFTWARE / GALLIUM_DRIVER=llvmpipe / Mesa EGL vendor) to get
	// WebKit off the NVIDIA GPU. It DID move rendering to software (no more
	// /dev/nvidia0 handles) — but made things WORSE: idle WebKitWebProcess CPU
	// rose to ~51% because the app's continuous CSS animations (infinite status
	// "pulse" dots, spinners) then repaint on the CPU every frame instead of the
	// GPU compositor. The real idle-CPU fix is to stop those animations from
	// running forever (see StatusIndicator/SessionTree/Preview), NOT to disable
	// the GPU. So we leave GPU rendering ON by default and only keep the env
	// overridable for per-machine A/B (ASMGR_GPU / WEBKIT_DISABLE_* still work).

	// Create application with options
	err := wails.Run(&options.App{
		Title:             appTitle,
		Width:             1280,
		Height:            800,
		MinWidth:          800,
		MinHeight:         600,
		DisableResize:     false,
		Fullscreen:        false,
		Frameless:         true,
		StartHidden:       false,
		HideWindowOnClose: false,
		BackgroundColour:  &options.RGBA{R: 26, G: 26, B: 46, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: func(ctx context.Context) {
			app.dictation = dictationService
			app.startup(ctx)
		},
		OnDomReady: func(ctx context.Context) {
			screens, err := runtime.ScreenGetAll(ctx)
			if err == nil && len(screens) > 0 {
				screen := screens[0]
				w := screen.Size.Width * 80 / 100
				h := screen.Size.Height * 80 / 100
				x := (screen.Size.Width - w) / 2
				y := (screen.Size.Height - h) / 2
				runtime.WindowSetPosition(ctx, x, y)
				runtime.WindowSetSize(ctx, w, h)
			}
		},
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
			dictationService,
		},
		Linux: &linux.Options{
			Icon:        appIcon,
			ProgramName: "asmgr-desktop",
			// GPU acceleration policy. Profiling pointed at the WebKit
			// compositor (the main thread is blocked 50–480ms per frame while
			// typing, but our JS — terminal.write/raf/timeouts — measures
			// near zero). On many WebKitGTK + driver combos "force GPU" is
			// actually slower than software rendering because every frame is
			// synced to the GPU. Override via ASMGR_GPU=never|ondemand|always
			// (default: ondemand) to find the fastest for this machine.
			WebviewGpuPolicy: gpuPolicyFromEnv(),
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

// applyGpuFallback switches WebKit to software rendering when this machine
// has no usable GPU, because WebKit aborts the process rather than degrading
// gracefully:
//
//	Could not create GBM EGL display: EGL_NOT_INITIALIZED. Aborting...
//
// That happens on VMs, headless/remote sessions and boxes with missing or
// broken drivers. The crash comes from cgo, so it can't be recovered once
// wails.Run starts — the check has to happen before.
//
// An explicit ASMGR_GPU always wins: this only fills in a default.
func applyGpuFallback() {
	if os.Getenv("ASMGR_GPU") != "" {
		return // user made the call; don't second-guess it
	}
	if hasUsableGPU() {
		return
	}
	// Keep WebKit off the GPU paths that abort. DMABUF is the renderer that
	// needs the GBM EGL display the log complains about.
	os.Setenv("ASMGR_GPU", "never")
	os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	os.Setenv("WEBKIT_DISABLE_COMPOSITING_MODE", "1")
	log.Println("no usable GPU render node found; falling back to software rendering " +
		"(set ASMGR_GPU=ondemand|always to override)")
}

// hasUsableGPU reports whether a DRM render node exists AND can be opened.
// Presence alone isn't enough: in containers and locked-down setups the node
// is there but permissions deny it, which is exactly when EGL init fails.
func hasUsableGPU() bool {
	nodes, err := filepath.Glob("/dev/dri/renderD*")
	if err != nil || len(nodes) == 0 {
		return false
	}
	for _, n := range nodes {
		f, err := os.Open(n)
		if err == nil {
			f.Close()
			return true
		}
	}
	return false
}

// gpuPolicyFromEnv lets us A/B the WebView hardware-acceleration policy at
// launch without rebuilding: ASMGR_GPU=never|ondemand|always.
// Default is OnDemand, which lets WebKit pick per-content and avoids the
// always-sync-to-GPU cost that "always" imposes on slow driver stacks.
func gpuPolicyFromEnv() linux.WebviewGpuPolicy {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ASMGR_GPU"))) {
	case "never", "off", "software":
		return linux.WebviewGpuPolicyNever
	case "always", "force":
		return linux.WebviewGpuPolicyAlways
	default:
		// Default: OnDemand. Keeps GPU compositing available so the app's CSS
		// animations stay off the CPU. (Forcing software rendering was tried and
		// raised idle CPU to ~51%.) Override with ASMGR_GPU=never|always.
		return linux.WebviewGpuPolicyOnDemand
	}
}
