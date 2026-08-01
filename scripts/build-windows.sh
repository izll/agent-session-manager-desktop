#!/usr/bin/env bash
# Cross-compile the Windows build from Linux, for testing without waiting on CI.
#
# A release round trip through GitHub Actions is ~10 minutes; this is under two,
# which matters when a fix needs several attempts on real Windows behaviour.
#
# The output is meant to match the CI artifact: same Wails build, same CGO
# toolchain family (MinGW-w64), same PortAudio (the MSYS2 UCRT64 package the CI
# installs), and the same runtime DLLs copied beside the exe. It is a testing
# aid regardless — releases still come from CI, which is the reproducible build.
#
# Requires: gcc-mingw-w64-x86-64. PortAudio is fetched on first run and cached.
set -euo pipefail

cd "$(dirname "$0")/.."
REPO="$PWD"
CACHE="${ASMGR_BUILD_CACHE:-$HOME/.cache/asmgr-winbuild}"
PA_DIR="$CACHE/pa/ucrt64"
PA_PKG="mingw-w64-ucrt-x86_64-portaudio-1~19.7.0-5-any.pkg.tar.zst"

if ! command -v x86_64-w64-mingw32-gcc >/dev/null; then
  echo "Missing cross compiler. Install it with:" >&2
  echo "  sudo apt-get install -y gcc-mingw-w64-x86-64" >&2
  exit 1
fi

# PortAudio for Windows: headers, import library and the runtime DLL. Taken
# from the same MSYS2 package the CI installs, so the app links against the
# same library version it does.
if [ ! -f "$PA_DIR/include/portaudio.h" ]; then
  echo "==> Fetching PortAudio for Windows"
  mkdir -p "$CACHE/pa"
  curl -sL -o "$CACHE/pa.zst" "https://mirror.msys2.org/mingw/ucrt64/$PA_PKG"
  tar --zstd -xf "$CACHE/pa.zst" -C "$CACHE/pa"
  rm -f "$CACHE/pa.zst"
fi

# The packaged .pc file hardcodes /ucrt64 as its prefix; point it at the real
# location. Without this cgo falls back to defaults that ask for -lasound —
# ALSA, which does not exist on Windows.
mkdir -p "$CACHE/pkgconfig"
sed "s|^prefix=/ucrt64|prefix=$PA_DIR|" \
  "$PA_DIR/lib/pkgconfig/portaudio-2.0.pc" > "$CACHE/pkgconfig/portaudio-2.0.pc"

WAILS="${WAILS_BIN:-$HOME/go/bin/wails}"
if [ ! -x "$WAILS" ]; then
  echo "wails not found at $WAILS (set WAILS_BIN)" >&2
  exit 1
fi

VERSION="$(sed -n 's/^var Version = "\(.*\)"$/\1/p' version.go)"
echo "==> Building $VERSION for windows/amd64"

# Rebuilt from scratch: a stale frontend/dist is the classic way to test a
# binary that does not contain the change being tested.
rm -rf frontend/dist

export PKG_CONFIG_PATH="$CACHE/pkgconfig"
export CGO_ENABLED=1
export CC=x86_64-w64-mingw32-gcc
export CXX=x86_64-w64-mingw32-g++

# -clean empties build/bin, which is shared with the Linux build — cross-
# building for Windows would otherwise delete the binary the user actually
# runs (via ~/zbin/asmgrd), leaving "asmgr-desktop-run: not found" behind.
# Set them aside and put them back afterwards.
PRESERVE_DIR="$(mktemp -d)"
trap 'rm -rf "$PRESERVE_DIR"' EXIT
for f in asmgr-desktop asmgr-desktop-dev asmgr-desktop-run; do
  [ -f "$REPO/build/bin/$f" ] && cp -p "$REPO/build/bin/$f" "$PRESERVE_DIR/"
done

"$WAILS" build -platform windows/amd64 -clean -ldflags "-X main.Version=$VERSION"

# Restore whatever was preserved above; -clean has run by now.
for f in "$PRESERVE_DIR"/*; do
  [ -e "$f" ] && cp -p "$f" "$REPO/build/bin/"
done

# The DLLs the binary links against have to sit beside it, exactly as the
# release archive ships them — the app does not start without them.
echo "==> Bundling runtime DLLs"
OUT="$REPO/build/bin"
copied=0
for dll in libportaudio.dll libgcc_s_seh-1.dll libstdc++-6.dll libwinpthread-1.dll; do
  for src in "$PA_DIR/bin/$dll" "/usr/lib/gcc/x86_64-w64-mingw32/13-win32/$dll" \
             "/usr/x86_64-w64-mingw32/lib/$dll"; do
    if [ -f "$src" ]; then
      cp "$src" "$OUT/$dll"
      copied=$((copied + 1))
      break
    fi
  done
done
echo "    $copied DLL(s) beside the exe"

ls -la "$OUT/asmgr-desktop.exe"
echo "==> Done: $OUT/asmgr-desktop.exe"
