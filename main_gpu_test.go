package main

import (
	"os"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

// An explicit ASMGR_GPU must never be overwritten by the fallback: the user
// (or a launcher script) chose it deliberately.
func TestGpuFallbackRespectsExplicitChoice(t *testing.T) {
	t.Setenv("ASMGR_GPU", "always")
	t.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "")

	applyGpuFallback()

	if got := os.Getenv("ASMGR_GPU"); got != "always" {
		t.Fatalf("ASMGR_GPU = %q, want it left as \"always\"", got)
	}
	if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "1" {
		t.Fatal("fallback disabled the DMABUF renderer despite an explicit choice")
	}
}

func TestGpuFallbackSkipsNonLinuxPlatforms(t *testing.T) {
	t.Setenv("ASMGR_GPU", "")
	t.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "")

	// A non-Linux startup must not launch a second Wails application merely to
	// probe Linux's GBM/EGL stack.
	applyGpuFallbackForOS("windows")
	applyGpuFallbackForOS("darwin")

	if got := os.Getenv("ASMGR_GPU"); got != "" {
		t.Fatalf("non-Linux fallback changed ASMGR_GPU to %q", got)
	}
	if got := os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER"); got != "" {
		t.Fatalf("non-Linux fallback changed WebKitGTK environment to %q", got)
	}
}

// A probe that can't run (here: the test binary has no probe dispatch path and
// is detected before re-execution) must leave the rendering default alone
// rather than wrongly declaring the GPU broken and downgrading everyone to
// software rendering.
func TestGpuFallbackKeepsDefaultWhenProbeCannotRun(t *testing.T) {
	t.Setenv("ASMGR_GPU", "")
	t.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "")

	applyGpuFallback()

	if got := os.Getenv("ASMGR_GPU"); got != "" {
		t.Fatalf("ASMGR_GPU = %q, want it left empty when the probe is inconclusive", got)
	}
}

func TestGpuPolicyFromEnv(t *testing.T) {
	cases := map[string]string{
		"never": "never", "off": "never", "software": "never",
		"always": "always", "force": "always",
		"": "ondemand", "garbage": "ondemand", "  Always  ": "always",
	}
	for in, want := range cases {
		t.Setenv("ASMGR_GPU", in)
		got := gpuPolicyFromEnv()
		var name string
		switch got {
		case linux.WebviewGpuPolicyAlways:
			name = "always"
		case linux.WebviewGpuPolicyOnDemand:
			name = "ondemand"
		case linux.WebviewGpuPolicyNever:
			name = "never"
		}
		if name != want {
			t.Errorf("ASMGR_GPU=%q → %s, want %s", in, name, want)
		}
	}
}
