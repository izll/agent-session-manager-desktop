// Package mobile lets a phone reach the app: a web server on this computer only,
// which a VPN such as Tailscale carries to the phone, and the devices allowed in.
//
// A device gets in once, with a pairing code the app shows as a QR code, and is
// known from then on by a secret of its own. Whoever holds that secret types into
// the user's terminals, so the code is short-lived and single-use, wrong codes lock
// pairing for a while, and a device can be revoked at any time.
package mobile

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"asmgr-desktop/session"
)

const (
	pairingLifetime = 5 * time.Minute
	// After this many wrong codes pairing is closed for pairingLockout.
	pairingFailureLimit = 10
	pairingLockout      = 15 * time.Minute
	// lastSeenWriteGap: LastSeen is for the user's eyes, and not worth a write
	// on every request.
	lastSeenWriteGap = time.Minute
)

var (
	// ErrPairingInvalid is a code that is wrong, used or expired.
	ErrPairingInvalid = errors.New("pairing code is not valid")
	// ErrPairingLocked follows too many wrong codes.
	ErrPairingLocked = errors.New("pairing is locked after too many wrong codes")
	// ErrDeviceNotFound is revoking a device that is not there.
	ErrDeviceNotFound = errors.New("device not found")
)

// Device is one phone or browser let in. It carries no secret.
type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	LastSeen  time.Time `json:"lastSeen"`
}

type storedDevice struct {
	Device
	// SecretHash is the SHA-256 of the device's secret: the file is not a key.
	SecretHash string `json:"secretHash"`
}

// Devices is the list of devices let in, and the pairing under way.
//
// The list is one file shared by every window of the app and by --serve. Each change
// is made to the file as it is on disk now, under a lock the others take too, and a
// device is checked against the file as it is now: revoking a lost phone in one window
// has to shut it out of the one serving.
type Devices struct {
	path string
	// loaded says which version of the file the list in memory is.
	loaded fileVersion

	mu       sync.Mutex
	devices  []storedDevice
	code     string
	expires  time.Time
	failures int
	lockedTo time.Time
}

// OpenDevices loads the list from path; a missing file is an empty list.
func OpenDevices(path string) (*Devices, error) {
	d := &Devices{path: path}
	if err := d.refreshLocked(); err != nil {
		return nil, err
	}
	return d, nil
}

type fileVersion struct {
	modified time.Time
	size     int64
	exists   bool
}

func versionOf(path string) (fileVersion, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileVersion{}, nil
	}
	if err != nil {
		return fileVersion{}, err
	}
	return fileVersion{modified: info.ModTime(), size: info.Size(), exists: true}, nil
}

// refreshLocked reads the file again if it changed since it was read.
func (d *Devices) refreshLocked() error {
	version, err := versionOf(d.path)
	if err != nil {
		return err
	}
	if version == d.loaded && (version.exists || d.devices == nil) {
		return nil
	}
	var devices []storedDevice
	if version.exists {
		data, err := os.ReadFile(d.path)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &devices); err != nil {
			return err
		}
	}
	d.devices, d.loaded = devices, version
	return nil
}

// change edits the list as it is on disk now, holding the lock the others take.
func (d *Devices) change(edit func() error) error {
	return session.WithCrossProcessFileLock(d.path+".lock", func() error {
		if err := d.refreshLocked(); err != nil {
			return err
		}
		if err := edit(); err != nil {
			return err
		}
		return d.saveLocked()
	})
}

func randomToken(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// NewPairing starts a pairing, ending any earlier one.
func (d *Devices) NewPairing(now time.Time) (string, time.Time, error) {
	code, err := randomToken(16)
	if err != nil {
		return "", time.Time{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.code, d.expires = code, now.Add(pairingLifetime)
	return code, d.expires, nil
}

// Redeem lets a device in with the pairing code, which is then used up.
func (d *Devices) Redeem(code, name string, now time.Time) (Device, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if now.Before(d.lockedTo) {
		return Device{}, "", ErrPairingLocked
	}
	valid := d.code != "" && now.Before(d.expires) &&
		subtle.ConstantTimeCompare([]byte(code), []byte(d.code)) == 1
	if !valid {
		d.failures++
		if d.failures >= pairingFailureLimit {
			d.failures = 0
			d.lockedTo = now.Add(pairingLockout)
			// A code seen by whoever is guessing is not to be trusted after.
			d.code = ""
		}
		return Device{}, "", ErrPairingInvalid
	}
	d.code = ""
	d.failures = 0
	secret, err := randomToken(32)
	if err != nil {
		return Device{}, "", err
	}
	id, err := randomToken(9)
	if err != nil {
		return Device{}, "", err
	}
	device := Device{ID: id, Name: name, CreatedAt: now, LastSeen: now}
	err = d.change(func() error {
		d.devices = append(d.devices, storedDevice{Device: device, SecretHash: hashSecret(secret)})
		return nil
	})
	if err != nil {
		d.loaded = fileVersion{}
		return Device{}, "", err
	}
	return device, secret, nil
}

// Authenticate finds the device a secret belongs to.
func (d *Devices) Authenticate(secret string, now time.Time) (Device, bool) {
	if secret == "" {
		return Device{}, false
	}
	hash := []byte(hashSecret(secret))
	d.mu.Lock()
	defer d.mu.Unlock()
	// A stat a request: revoked elsewhere is revoked here.
	if err := d.refreshLocked(); err != nil {
		return Device{}, false
	}
	index := d.find(hash)
	if index < 0 {
		return Device{}, false
	}
	if now.Sub(d.devices[index].LastSeen) >= lastSeenWriteGap {
		_ = d.change(func() error {
			// Found again on the file as it is now: it may have been revoked since.
			if at := d.find(hash); at >= 0 {
				d.devices[at].LastSeen = now
			}
			return nil
		})
		if index = d.find(hash); index < 0 {
			return Device{}, false
		}
	}
	return d.devices[index].Device, true
}

func (d *Devices) find(hash []byte) int {
	for i := range d.devices {
		if subtle.ConstantTimeCompare(hash, []byte(d.devices[i].SecretHash)) == 1 {
			return i
		}
	}
	return -1
}

// List gives the devices, oldest first.
func (d *Devices) List() []Device {
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = d.refreshLocked()
	list := make([]Device, 0, len(d.devices))
	for _, stored := range d.devices {
		list = append(list, stored.Device)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
	return list
}

// Revoke shuts a device out.
func (d *Devices) Revoke(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.change(func() error {
		for i, stored := range d.devices {
			if stored.ID == id {
				d.devices = append(append([]storedDevice{}, d.devices[:i]...), d.devices[i+1:]...)
				return nil
			}
		}
		return ErrDeviceNotFound
	})
}

func (d *Devices) saveLocked() error {
	data, err := json.MarshalIndent(d.devices, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(d.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".devices-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), d.path); err != nil {
		return err
	}
	version, err := versionOf(d.path)
	if err == nil {
		d.loaded = version
	}
	return err
}
