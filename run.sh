#!/bin/bash
# WebKit GPU acceleration
export WEBKIT_DISABLE_COMPOSITING_MODE=0
export WEBKIT_FORCE_SANDBOX=0
export GDK_RENDERING=gl

cd "$(dirname "$0")"
./build/bin/asmgr-desktop "$@"
