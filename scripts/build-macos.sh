#!/usr/bin/env bash
# Build the macOS app on a Mac over SSH, for testing without waiting on CI.
#
# A release round trip through GitHub Actions is ~25 minutes for the macOS job
# alone. This syncs the working tree to a Mac, builds there, and installs the
# result — which matters when a fix needs several attempts against real macOS
# behaviour.
#
# The Mac needs: Homebrew, go, node, portaudio, and the Wails CLI. Everything
# else is carried over by the sync.
#
# Releases still come from CI, which is the reproducible build. This is a
# testing aid.
set -euo pipefail

cd "$(dirname "$0")/.."

MAC_HOST="${ASMGR_MAC_HOST:-izll@192.168.1.120}"
MAC_DIR="${ASMGR_MAC_DIR:-\$HOME/asmgr-build}"
REMOTE_PATH='export PATH=/opt/homebrew/bin:$HOME/go/bin:$PATH'

echo "==> Syncing source to $MAC_HOST"
# node_modules and build outputs are rebuilt there; .git is not needed to build.
tar czf - \
  --exclude=node_modules \
  --exclude=build/bin \
  --exclude=frontend/dist \
  --exclude=.git \
  . | ssh "$MAC_HOST" "$REMOTE_PATH; rm -rf $MAC_DIR && mkdir -p $MAC_DIR && tar xzf - -C $MAC_DIR"

VERSION="$(sed -n 's/^var Version = "\(.*\)"$/\1/p' version.go)"
echo "==> Building $VERSION on $MAC_HOST"
ssh "$MAC_HOST" "$REMOTE_PATH; cd $MAC_DIR && wails build -platform darwin/arm64 -clean -ldflags '-X main.Version=$VERSION'"

echo "==> Installing to ~/Applications"
# Replaced rather than merged: a stale file left inside an .app bundle from a
# previous build is the kind of thing that only shows up at runtime.
ssh "$MAC_HOST" "$REMOTE_PATH; pkill -f asmgr-desktop 2>/dev/null || true; \
  rm -rf \$HOME/Applications/asmgr-desktop.app && \
  cp -R $MAC_DIR/build/bin/asmgr-desktop.app \$HOME/Applications/ && \
  codesign --force --deep --sign - \$HOME/Applications/asmgr-desktop.app && \
  echo 'installed:' && ls -ld \$HOME/Applications/asmgr-desktop.app"

echo "==> Done. Launch it from ~/Applications on the Mac."
