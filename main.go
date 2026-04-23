package main

import (
	"context"
	"embed"
	_ "embed"
	"os"

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

	// Create application with options
	err := wails.Run(&options.App{
		Title:            appTitle,
		Width:            1280,
		Height:           800,
		MinWidth:         800,
		MinHeight:        600,
		DisableResize:    false,
		Fullscreen:       false,
		Frameless:        true,
		StartHidden:      false,
		HideWindowOnClose: false,
		BackgroundColour: &options.RGBA{R: 26, G: 26, B: 46, A: 255},
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
			Icon:             appIcon,
			ProgramName:      "asmgr-desktop",
			WebviewGpuPolicy: linux.WebviewGpuPolicyAlways, // Force GPU acceleration
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
