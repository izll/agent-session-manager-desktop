package mobile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func openTestDevices(t *testing.T) (*Devices, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "devices.json")
	devices, err := OpenDevices(path)
	if err != nil {
		t.Fatal(err)
	}
	return devices, path
}

var start = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func TestPairingWorksOnceWithinFiveMinutes(t *testing.T) {
	devices, _ := openTestDevices(t)
	code, expires, err := devices.NewPairing(start)
	if err != nil {
		t.Fatal(err)
	}
	if !expires.Equal(start.Add(5 * time.Minute)) {
		t.Fatalf("expires = %v", expires)
	}
	if len(code) < 22 {
		t.Fatalf("code too short for 128 bits: %q", code)
	}
	device, secret, err := devices.Redeem(code, "Telefon", start.Add(4*time.Minute+59*time.Second))
	if err != nil || device.Name != "Telefon" || device.ID == "" || len(secret) < 43 {
		t.Fatalf("device = %+v, secret %d chars, err = %v", device, len(secret), err)
	}
	if _, _, err := devices.Redeem(code, "Másik", start.Add(5*time.Minute-time.Second)); !errors.Is(err, ErrPairingInvalid) {
		t.Fatalf("second use: %v", err)
	}
}

func TestExpiredPairingFails(t *testing.T) {
	devices, _ := openTestDevices(t)
	code, _, _ := devices.NewPairing(start)
	if _, _, err := devices.Redeem(code, "Telefon", start.Add(5*time.Minute+time.Second)); !errors.Is(err, ErrPairingInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewPairingReplacesTheOld(t *testing.T) {
	devices, _ := openTestDevices(t)
	old, _, _ := devices.NewPairing(start)
	fresh, _, _ := devices.NewPairing(start.Add(time.Second))
	if _, _, err := devices.Redeem(old, "x", start.Add(2*time.Second)); !errors.Is(err, ErrPairingInvalid) {
		t.Fatalf("old code: %v", err)
	}
	if _, _, err := devices.Redeem(fresh, "x", start.Add(3*time.Second)); err != nil {
		t.Fatalf("new code: %v", err)
	}
}

func TestTenFailuresLockPairingForFifteenMinutes(t *testing.T) {
	devices, _ := openTestDevices(t)
	code, _, _ := devices.NewPairing(start)
	for i := range 10 {
		if _, _, err := devices.Redeem("wrong", "x", start.Add(time.Duration(i)*time.Second)); !errors.Is(err, ErrPairingInvalid) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, _, err := devices.Redeem(code, "x", start.Add(20*time.Second)); !errors.Is(err, ErrPairingLocked) {
		t.Fatalf("right code while locked: %v", err)
	}
	later := start.Add(20*time.Second + 15*time.Minute)
	code, _, _ = devices.NewPairing(later)
	if _, _, err := devices.Redeem(code, "x", later.Add(time.Second)); err != nil {
		t.Fatalf("after the lock: %v", err)
	}
}

func TestSecretIsStoredOnlyHashed(t *testing.T) {
	devices, path := openTestDevices(t)
	code, _, _ := devices.NewPairing(start)
	_, secret, err := devices.Redeem(code, "Telefon", start)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || strings.Contains(string(data), code) {
		t.Fatalf("a secret on disk: %s", data)
	}
	// Windows keeps no such bits; the file is in the user's own profile there.
	if info, _ := os.Stat(path); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode())
	}
}

func TestAuthenticateAndRevoke(t *testing.T) {
	devices, path := openTestDevices(t)
	code, _, _ := devices.NewPairing(start)
	device, secret, _ := devices.Redeem(code, "Telefon", start)
	if got, ok := devices.Authenticate(secret, start); !ok || got.ID != device.ID {
		t.Fatalf("authenticate = %+v, %v", got, ok)
	}
	if _, ok := devices.Authenticate(secret+"x", start); ok {
		t.Fatal("a wrong secret passed")
	}
	if _, ok := devices.Authenticate("", start); ok {
		t.Fatal("an empty secret passed")
	}
	reopened, err := OpenDevices(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Authenticate(secret, start); !ok {
		t.Fatal("not known after reopening")
	}
	if list := reopened.List(); len(list) != 1 || list[0].Name != "Telefon" {
		t.Fatalf("list = %+v", list)
	}
	if err := reopened.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Authenticate(secret, start); ok {
		t.Fatal("passed after revoking")
	}
	again, _ := OpenDevices(path)
	if _, ok := again.Authenticate(secret, start); ok {
		t.Fatal("revoking did not last")
	}
}

// The list is one file for every window of the app and for --serve. Revoking a lost
// phone in one of them must shut it out of the one serving, and stay revoked.
func TestARevokeElsewhereShutsTheDeviceOut(t *testing.T) {
	serving, path := openTestDevices(t)
	code, _, _ := serving.NewPairing(start)
	device, secret, _ := serving.Redeem(code, "Telefon", start)
	other, err := OpenDevices(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := serving.Authenticate(secret, start.Add(time.Second)); ok {
		t.Fatal("the serving process still lets it in")
	}
	// Its next LastSeen write must not bring it back.
	if _, ok := serving.Authenticate(secret, start.Add(2*time.Minute)); ok {
		t.Fatal("let in later")
	}
	again, _ := OpenDevices(path)
	if len(again.List()) != 0 {
		t.Fatalf("back in the file: %+v", again.List())
	}
}

// Two processes letting devices in at once each keep the other's.
func TestDevicesAddedElsewhereAreKept(t *testing.T) {
	first, path := openTestDevices(t)
	second, _ := OpenDevices(path)
	code, _, _ := first.NewPairing(start)
	if _, _, err := first.Redeem(code, "A", start); err != nil {
		t.Fatal(err)
	}
	code, _, _ = second.NewPairing(start)
	if _, _, err := second.Redeem(code, "B", start); err != nil {
		t.Fatal(err)
	}
	reopened, _ := OpenDevices(path)
	if len(reopened.List()) != 2 {
		t.Fatalf("list = %+v", reopened.List())
	}
}
