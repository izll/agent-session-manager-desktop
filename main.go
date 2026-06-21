package main

import (
	"context"
	"embed"
	_ "embed"
	"log"
	"os"
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
