#!/usr/bin/env bash
#
# Build a throwaway demo environment and photograph the app for the README.
#
# Screenshots go in the README, so they must not contain a real username, a real
# path, or a real session. This builds an entirely synthetic HOME — invented
# repositories, invented session names, /tmp paths — runs the app against it,
# and leaves the real config untouched. A previous screenshot leaked a username
# through the sidebar status lines (fixed in 958b34d); this is how that stops
# happening again.
#
# Usage:
#   scripts/demo-screenshots.sh            # set up, launch, wait for a keypress
#   scripts/demo-screenshots.sh --shoot    # ... and capture the dashboard
#   scripts/demo-screenshots.sh --clean    # tear the demo down
#
# Requires: tmux, xdotool, ImageMagick (import/convert), a built binary.

set -uo pipefail

DEMO=/tmp/asmgr-demo
HOME_DIR="$DEMO/home"
CONFIG="$HOME_DIR/.config/agent-session-manager-desktop"
PROJECTS="$HOME_DIR/projects"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP="$REPO_ROOT/build/bin/asmgr-desktop"

# Five of the eleven sessions run, so the dashboard shows a real mix of states
# and the sidebar has status lines to read. Without live tmux sessions every
# card says "Stopped" and the sidebar is bare — which is what makes a demo
# screenshot look thinner than the app really is.
declare -A RUNNING_OUTPUT=(
  [d1]='while :; do echo "$(date +%H:%M:%S) INFO request POST /v1/tokens 201 8ms"; sleep 2; done'
  [d2]='while :; do echo "$(date +%H:%M:%S) rotating signing key kid=2026-08"; sleep 3; done'
  [d4]='while :; do echo "a Retry-After header on 429 responses. All mi..."; sleep 3; done'
  [d7]='i=24; while :; do echo "Training... (esc to interrupt · $i/50 epochs · 3..."; i=$((i+1)); sleep 4; done'
  [d10]='while :; do echo "$(date +%H:%M:%S) terraform plan: no changes"; sleep 5; done'
)

clean() {
  for s in "${!RUNNING_OUTPUT[@]}"; do tmux kill-session -t "$s" 2>/dev/null; done
  if [[ -f "$DEMO/demo.pid" ]]; then
    local pid; pid=$(cat "$DEMO/demo.pid")
    # By PID, never by name: a pkill pattern for the binary also matches the
    # user's own running instance, and killing that drops their agents' GUI
    # mirrors. (It happened once. Don't repeat it.)
    kill "$pid" 2>/dev/null
  fi
  rm -rf "$DEMO"
  echo "demo torn down; your own sessions untouched"
}

[[ "${1:-}" == "--clean" ]] && { clean; exit 0; }

echo "==> building the synthetic HOME at $HOME_DIR"
rm -rf "$DEMO"; mkdir -p "$CONFIG" "$PROJECTS"

# Ten repositories, three of them with uncommitted work, so the dashboard's
# "dirty repositories" count is not zero.
declare -A REPOS=(
  [api-gateway]='add rate limiting'   [auth-service]='rotate signing keys'
  [billing]='add refund handling'     [web-dashboard]='fix retry banner'
  [mobile-app]='initial commit'       [search-index]='initial commit'
  [ml-pipeline]='add training script' [feature-store]='initial commit'
  [docs-site]='initial commit'        [infra]='initial commit'
)
DIRTY="api-gateway ml-pipeline billing"

for name in "${!REPOS[@]}"; do
  d="$PROJECTS/$name"; mkdir -p "$d/src"
  printf 'def run():\n    return "%s"\n' "$name" > "$d/src/main.py"
  git -C "$d" init -q
  git -C "$d" config user.email dev@example.com
  git -C "$d" config user.name dev
  git -C "$d" add -A
  git -C "$d" commit -qm "${REPOS[$name]}"
  if [[ " $DIRTY " == *" $name "* ]]; then
    printf '\n\ndef extra():\n    return 42\n' >> "$d/src/main.py"
    printf '# added\n' > "$d/src/new.py"
  fi
done

# The diff shot wants a change worth reading, not a one-line stub.
BILL="$PROJECTS/billing/src/refunds.py"
cat > "$BILL" <<'PY'
def refund(order, amount):
    if amount <= 0:
        raise ValueError("amount must be positive")
    if amount > order.total:
        raise ValueError("cannot refund more than the order total")
    order.refunded += amount
    return Receipt(order.id, amount)


def eligible(order, today):
    return (today - order.placed_at).days <= 30
PY
git -C "$PROJECTS/billing" add -A
git -C "$PROJECTS/billing" commit -qm "Add refund handling"
cat > "$BILL" <<'PY'
from decimal import Decimal


def refund(order, amount):
    amount = Decimal(amount)
    if amount <= 0:
        raise ValueError("amount must be positive")
    if amount > order.refundable():
        raise ValueError("cannot refund more than is left on the order")
    order.refunded += amount
    audit.record("refund", order.id, amount)
    return Receipt(order.id, amount)


def eligible(order, today):
    window = order.policy.refund_days or 30
    return (today - order.placed_at).days <= window
PY

echo "==> writing sessions.json"
# Built from the user's own store so every field the app expects is present,
# then overwritten with invented values. Reading the real file is safe: nothing
# from it survives into the demo except the schema.
REAL_STORE="$HOME/.config/agent-session-manager-desktop/sessions.json" \
CONFIG="$CONFIG" PROJECTS="$PROJECTS" python3 - <<'PY'
import json, os
src = json.load(open(os.environ['REAL_STORE']))
tmpl = src['instances'][0]
P = os.environ['PROJECTS']

groups = [{'id':'g1','name':'Backend','collapsed':True},
          {'id':'g2','name':'Frontend','collapsed':True},
          {'id':'g3','name':'Data & ML','collapsed':False}]

# Distinct name colours, and NO background colour: the session colour tints the
# card's header band, and a saturated background behind a card of small text is
# tiring to read.
colours = {'api-gateway':'#7dd3fc','auth-service':'#a78bfa','billing':'#fbbf24',
           'web-dashboard':'#34d399','mobile-app':'#f472b6','search-index':'#60a5fa',
           'ml-pipeline':'#fb923c','feature-store':'#22d3ee','docs-site':'#c4b5fd',
           'infra-terraform':'#94a3b8','release-notes':'#f87171'}

def mk(i, name, agent, status, repo, gid='', fav=False):
    o = dict(tmpl)
    o.update(id=f'd{i}', name=name, agent=agent, status=status,
             path=f'{P}/{repo}', group_id=gid, color=colours.get(name,''),
             bg_color='', full_row_color=False, favorite=fav,
             resume_session_id='', base_commit_sha='')
    for k in ('followedWindows','windows','notes'):
        o.pop(k, None)
    return o

instances = [
    mk(1,'api-gateway','claude','running','api-gateway','g1',True),
    mk(2,'auth-service','codex','running','auth-service','g1'),
    mk(3,'billing','claude','stopped','billing','g1'),
    mk(4,'web-dashboard','claude','running','web-dashboard','g2',True),
    mk(5,'mobile-app','gemini','stopped','mobile-app','g2'),
    mk(6,'search-index','aider','stopped','search-index','g2'),
    mk(7,'ml-pipeline','claude','running','ml-pipeline','g3',True),
    mk(8,'feature-store','codex','stopped','feature-store','g3'),
    mk(9,'docs-site','opencode','stopped','docs-site','g3'),
    mk(10,'infra-terraform','terminal','running','infra'),
    mk(11,'release-notes','claude','stopped','docs-site'),
]

settings = {**src.get('settings', {}), 'language': 'en',
            'showAgentIcons': True, 'markedSessionId': '', 'splitView': False}
json.dump({'schema_version': src['schema_version'], 'revision': 1,
           'instances': instances, 'groups': groups,
           'settings': settings, 'trash': []},
          open(os.path.join(os.environ['CONFIG'], 'sessions.json'), 'w'), indent=2)
print(f"    {len(instances)} sessions, {len(groups)} groups, 3 favourites")
PY

# Tasks live with the code, not in the config directory.
mkdir -p "$PROJECTS/billing/.taskmaster"
cat > "$PROJECTS/billing/.taskmaster/tasks.json" <<'JSON'
{"tasks":[
 {"id":"1","title":"Round refunds to the currency, not to two places",
  "description":"JPY has no minor unit; the current rounding invents one.",
  "status":"in-progress","priority":"high","dueAt":"2026-08-16T00:00:00Z",
  "subtasks":[{"id":"1.1","title":"Table of minor units","status":"done"},
              {"id":"1.2","title":"Property test against the table","status":"pending"}]},
 {"id":"2","title":"Retry webhooks with a backoff",
  "description":"Three immediate retries hammer a partner that is already down.",
  "status":"pending","priority":"high","dueAt":"2026-08-19T00:00:00Z","dependencies":["1"]},
 {"id":"3","title":"Document the refund window override",
  "status":"pending","priority":"medium","dueAt":"2026-08-26T00:00:00Z"},
 {"id":"4","title":"Drop the legacy /v1/refund alias","status":"done","priority":"low"}
]}
JSON

echo "==> starting tmux sessions for the running ones"
# The app looks up a session's multiplexer session by its instance id
# (Instance.TmuxSessionName returns the id), so the names must match d1, d2, ...
declare -A PATHS=([d1]=api-gateway [d2]=auth-service [d4]=web-dashboard
                  [d7]=ml-pipeline [d10]=infra)
for s in "${!RUNNING_OUTPUT[@]}"; do
  script="$DEMO/s_$s.sh"
  printf '%s\n' "${RUNNING_OUTPUT[$s]}" > "$script"
  tmux new-session -d -s "$s" -c "$PROJECTS/${PATHS[$s]}" "bash $script" 2>/dev/null
done
sleep 3

echo "==> launching the app against the demo HOME"
[[ -x "$APP" ]] || { echo "no binary at $APP — build first"; exit 1; }
HOME="$HOME_DIR" nohup "$APP" > "$DEMO/app.log" 2>&1 &
DEMO_PID=$!
echo "$DEMO_PID" > "$DEMO/demo.pid"
sleep 14

# Find the window BY PID. Two instances can be running (yours and this one) and
# they share a window title, so matching on the name picks the wrong one — and
# photographing the wrong one puts real session content in a README screenshot.
WIN=""
for w in $(xdotool search --name "Agent Session Manager" 2>/dev/null); do
  [[ "$(xdotool getwindowpid "$w" 2>/dev/null)" == "$DEMO_PID" ]] && WIN="$w"
done
[[ -n "$WIN" ]] || { echo "could not find the demo window"; exit 1; }
echo "    demo window $WIN (pid $DEMO_PID)"

if [[ "${1:-}" == "--shoot" ]]; then
  xdotool windowactivate "$WIN"; sleep 3
  eval "$(xdotool getwindowgeometry --shell "$WIN" | grep -E '^(X|Y)=')"
  # Park the pointer somewhere harmless: left where it clicked it raises a
  # tooltip or a hover state that has no business in a screenshot.
  xdotool mousemove $((X+1700)) $((Y+1020)); sleep 2
  import -window "$WIN" "$DEMO/dashboard.png"
  echo "    wrote $DEMO/dashboard.png"
  echo
  echo "For the diff shot, open a session and its Diff tab by hand — synthetic"
  echo "clicks do not reach the WebKit view bar — then:"
  echo "    import -window $WIN $DEMO/diff.png"
  echo "    convert $DEMO/diff.png -crop 2048x760+0+0 +repage -resize 1600x -quality 92 $DEMO/diff-crop.png"
else
  echo
  echo "App is up. Arrange what you want to photograph, then:"
  echo "    import -window $WIN $DEMO/shot.png"
fi

echo
echo "When finished:  scripts/demo-screenshots.sh --clean"
