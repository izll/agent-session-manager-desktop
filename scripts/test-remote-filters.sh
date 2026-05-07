#!/usr/bin/env bash
# End-to-end test for the remote filter loader.
#
# Starts a local HTTPS server (self-signed cert) on port 18443 that serves a
# sample filter bundle. Pair this with a `-tags devremote` build of the app —
# that build tag flips the loader to point at https://127.0.0.1:18443/v1.json
# with cert verification disabled, exactly enough for local testing.
#
# Quickstart:
#   ./scripts/test-remote-filters.sh                   # start the server
#   /home/izll/go/bin/wails build -tags "webkit2_41,devmode,devremote" -devtools
#   ./build/bin/asmgr-desktop                          # in another terminal
#   # wait ~10 seconds, then:
#   cat ~/.cache/agent-session-manager/filters-remote.json
#
# Cleanup:
#   ./scripts/test-remote-filters.sh clean
set -euo pipefail

CACHE=~/.cache/agent-session-manager/filters-remote.json
TMP=/tmp/asmgr-test-filters
PORT=18443

if [[ "${1:-}" == "clean" ]]; then
  rm -rf "$TMP" "$CACHE"
  echo "cleaned cache and tmp dir"
  exit 0
fi

mkdir -p "$TMP"

# Sample bundle that's clearly distinguishable from the built-in defaults.
# The "__REMOTE_FILTER_TEST_MARKER__" entry is the easiest thing to verify
# afterwards — it appears in the cached file but never in the defaults.
cat > "$TMP/v1.json" <<'JSON'
{
  "schema_version": 1,
  "filters": {
    "codex": {
      "skip_contains": [
        "__REMOTE_FILTER_TEST_MARKER__",
        "context left",
        "? for",
        "esc to interrupt"
      ],
      "skip_prefixes": [
        ">", "›", "╭", "╰", "│",
        "Tip:",
        "gpt-5", "gpt-4", "gpt-3"
      ]
    }
  }
}
JSON

# Self-signed cert for localhost (valid one week, plenty for testing).
if [[ ! -f "$TMP/cert.pem" ]]; then
  openssl req -x509 -nodes -newkey rsa:2048 -days 7 \
    -keyout "$TMP/key.pem" -out "$TMP/cert.pem" \
    -subj "/CN=localhost" -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
    >/dev/null 2>&1
  echo "generated self-signed cert at $TMP/cert.pem"
fi

# Tiny HTTPS file server. Threaded so multiple requests don't block.
python3 - "$TMP" "$PORT" <<'PY' &
import http.server, ssl, sys, os
root, port = sys.argv[1], int(sys.argv[2])
os.chdir(root)
ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
ctx.load_cert_chain(certfile=os.path.join(root, "cert.pem"),
                    keyfile=os.path.join(root, "key.pem"))
srv = http.server.ThreadingHTTPServer(("127.0.0.1", port), http.server.SimpleHTTPRequestHandler)
srv.socket = ctx.wrap_socket(srv.socket, server_side=True)
print(f"serving https://127.0.0.1:{port}/v1.json — Ctrl-C to stop", flush=True)
srv.serve_forever()
PY
SERVER_PID=$!
trap "kill $SERVER_PID 2>/dev/null || true" EXIT

sleep 0.5

echo
echo "=== sanity check (curl, ignoring self-signed cert) ==="
curl -ks "https://127.0.0.1:${PORT}/v1.json" | head -8
echo
echo "=== next steps ==="
echo "  1. In a separate terminal:"
echo "       cd $PWD/.."
echo "       /home/izll/go/bin/wails build -tags \"webkit2_41,devmode,devremote\" -devtools"
echo "       ./build/bin/asmgr-desktop"
echo
echo "  2. Wait ~10 seconds. The first remote refresh runs after 10s, then"
echo "     every 30 minutes. Check the cache:"
echo "       ls -la $CACHE"
echo "       cat $CACHE | head"
echo
echo "  3. The marker '__REMOTE_FILTER_TEST_MARKER__' should be in the file."
echo "     If it is, the whole pipeline (HTTPS → validation → atomic write) works."
echo
echo "Press Ctrl-C here when done to stop the local server."
wait $SERVER_PID
