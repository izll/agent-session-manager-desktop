package remote

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

// Putting the helper on a server.
//
// Uploaded over the SSH connection that is already open, rather than through
// SFTP: that is one subsystem fewer to depend on, and some hardened servers
// disable it. The binary is small — a couple of megabytes, statically linked —
// so the encoding overhead of sending it as text costs less than the extra
// moving part would.

// installLockDir is created atomically so two app windows connecting to the
// same server at once cannot both write the binary.
const installLockDir = ".asmgr/.install.lock"

// InstallHelper uploads the helper and makes it executable.
//
// binary is the compiled helper for the server's architecture; the caller
// picks it, because only the caller knows what `uname -m` said.
func InstallHelper(ctx context.Context, client *Client, binary []byte) error {
	if len(binary) == 0 {
		return fmt.Errorf("no helper binary to install")
	}

	if err := takeInstallLock(ctx, client); err != nil {
		return err
	}
	defer releaseInstallLock(ctx, client)

	if _, err := client.Run(ctx, "mkdir -p $HOME/.asmgr"); err != nil {
		return fmt.Errorf("could not create the helper directory: %w", err)
	}

	// Written to a temporary name and moved into place. Overwriting a running
	// binary fails with "text file busy"; a rename only swaps the directory
	// entry, so a helper still serving an older connection keeps running from
	// the file it already opened.
	const tempPath = "$HOME/" + HelperPath + ".new"

	// base64 in chunks: a single command line long enough to hold two
	// megabytes would exceed what the shell accepts, and the failure would be
	// "argument list too long" rather than anything about the helper.
	encoded := base64.StdEncoding.EncodeToString(binary)
	if _, err := client.Run(ctx, "rm -f "+tempPath+".b64"); err != nil {
		return err
	}

	const chunkSize = 96 << 10
	for start := 0; start < len(encoded); start += chunkSize {
		end := start + chunkSize
		if end > len(encoded) {
			end = len(encoded)
		}
		// printf rather than echo: echo's handling of backslashes and leading
		// dashes varies between shells, and base64 output is not worth the
		// risk of a shell deciding one of its characters means something.
		command := fmt.Sprintf("printf %%s %s >> %s.b64", encoded[start:end], tempPath)
		if _, err := client.Run(ctx, command); err != nil {
			return fmt.Errorf("uploading the helper failed: %w", err)
		}
	}

	// Decoded, made executable, and moved — in one command, so a failure part
	// way through cannot leave a half-written binary in the final place.
	finish := strings.Join([]string{
		"base64 -d " + tempPath + ".b64 > " + tempPath,
		"rm -f " + tempPath + ".b64",
		"chmod 700 " + tempPath,
		"mv -f " + tempPath + " $HOME/" + HelperPath,
	}, " && ")
	if _, err := client.Run(ctx, finish); err != nil {
		return fmt.Errorf("installing the helper failed: %w", err)
	}

	installed, err := InspectHelper(ctx, client)
	if err != nil {
		return err
	}
	if !installed.Present {
		return fmt.Errorf("the helper was uploaded but does not run — " +
			"check that the architecture matches the server")
	}
	return nil
}

// takeInstallLock claims the right to install.
//
// mkdir is the lock: it either creates the directory or fails, with no window
// between checking and creating. A lock file written with > would be created
// by both writers.
func takeInstallLock(ctx context.Context, client *Client) error {
	out, err := client.Run(ctx, "mkdir -p $HOME/.asmgr && mkdir $HOME/"+installLockDir+
		" 2>/dev/null && echo taken || echo busy")
	if err != nil {
		return err
	}
	if !strings.Contains(string(out), "taken") {
		return fmt.Errorf("another window is installing the helper on this server; " +
			"try again in a moment")
	}
	return nil
}

func releaseInstallLock(ctx context.Context, client *Client) {
	// Best effort: a lock left behind by a crashed install would block the
	// next one, so it is removed even when the install failed.
	_, _ = client.Run(ctx, "rmdir $HOME/"+installLockDir+" 2>/dev/null || true")
}

// EnsureHelper installs the helper if the server has none, or the wrong one.
//
// binaryFor is asked for the binary only when one is actually needed, so the
// common case — a server that already has the right helper — costs one command
// and no upload.
func EnsureHelper(ctx context.Context, client *Client,
	binaryFor func(arch string) ([]byte, error)) (*InstalledHelper, error) {

	installed, err := InspectHelper(ctx, client)
	if err != nil {
		return nil, err
	}
	if installed.UpToDate() {
		return installed, nil
	}

	archOutput, err := client.Run(ctx, "uname -m")
	if err != nil {
		return nil, err
	}
	arch := normaliseArch(string(archOutput))
	if arch == "" {
		return nil, fmt.Errorf("this server's architecture (%s) is not one the helper is built for",
			strings.TrimSpace(string(archOutput)))
	}

	binary, err := binaryFor(arch)
	if err != nil {
		return nil, err
	}
	if err := InstallHelper(ctx, client, binary); err != nil {
		return nil, err
	}
	return InspectHelper(ctx, client)
}
