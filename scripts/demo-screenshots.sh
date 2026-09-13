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
# What the running sessions show.
#
# Echo loops made the screenshots useless: a terminal repeating one line says
# nothing about what this app is for. These are transcripts of the kind of work
# the agents actually do — a tool call, an edit, a test run, a question waiting
# on an answer — so the sidebar status lines and the terminal both read like a
# working day.
CONTENT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/scripts/demo-content"
# What the running sessions show. Echo loops made the screenshots useless: a
# terminal repeating one line says nothing about what this app is for. These
# are transcripts of the work the agents actually do.
CONTENT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/scripts/demo-content"
declare -A RUNNING_OUTPUT=(
  [d3]="bash $CONTENT/term_test.sh"
  [d4]="bash $CONTENT/claude_main.sh"
  [d5]="bash $CONTENT/codex_tab.sh"
  [d6]="bash $CONTENT/term_build.sh"
  [d7]="bash $CONTENT/claude_main.sh"
  [d8]="bash $CONTENT/claude_waiting.sh"
  [d9]="bash $CONTENT/term_test.sh"
  [d11]="bash $CONTENT/claude_waiting.sh"
  [d12]="bash $CONTENT/codex_tab.sh"
  [d13]="bash $CONTENT/term_test.sh"
  [d14]="bash $CONTENT/claude_main.sh"
  [d15]="bash $CONTENT/codex_tab.sh"
  [d18]="bash $CONTENT/term_build.sh"
  [d38]="bash $CONTENT/claude_main.sh"
  [d40]="bash $CONTENT/term_test.sh"
  [d41]="bash $CONTENT/claude_waiting.sh"
  [d44]="bash $CONTENT/term_test.sh"
  [d45]="bash $CONTENT/codex_tab.sh"
  [d46]="bash $CONTENT/term_build.sh"
  [d47]="bash $CONTENT/term_test.sh"
  [d48]="bash $CONTENT/train.sh"
)

declare -A TAB_CONTENT=(
  [1]="claude_main.sh" [2]="codex_tab.sh" [3]="term_build.sh"
  [4]="claude_waiting.sh" [5]="term_test.sh" [6]="train.sh"
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
  [api-gateway]='add rate limiting'      [auth-service]='rotate signing keys'
  [billing]='add refund handling'        [web-dashboard]='fix retry banner'
  [voice-relay]='initial commit'         [search-index]='initial commit'
  [ml-pipeline]='add training script'    [feature-store]='initial commit'
  [docs-site]='initial commit'           [infra]='initial commit'
  [cad-viewer]='initial commit'          [market-watch]='initial commit'
  [portal-gateway]='initial commit'      [usage-widget]='initial commit'
  [editor-bridge]='initial commit'       [dictation]='initial commit'
  [company-lookup]='initial commit'      [push-notify]='initial commit'
  [home-hub]='initial commit'            [grid-sim]='initial commit'
  [agent-runner]='initial commit'        [user-stats]='initial commit'
  [admin-console]='initial commit'       [newsroom]='initial commit'
  [inventory]='initial commit'           [session-manager]='initial commit'
  [social-sync]='initial commit'         [home-inventory]='initial commit'
  [budget-2026]='initial commit'         [form-builder]='initial commit'
  [tui-manager]='initial commit'         [discord-bot]='initial commit'
  [pool-booking]='initial commit'        [vm-manager]='initial commit'
  [misc]='initial commit'
)
DIRTY="api-gateway ml-pipeline billing voice-relay vm-manager"

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

groups = [{'id':'g1','name':'Work','collapsed':False},
          {'id':'g2','name':'Side projects','collapsed':False},
          {'id':'g3','name':'Misc','collapsed':False},
          {'id':'g4','name':'Platform','collapsed':False},
          {'id':'g5','name':'Games','collapsed':False},
          {'id':'g6','name':'Parked','collapsed':False},
          {'id':'g7','name':'Shared','collapsed':False}]

# Distinct name colours, and NO background colour: the session colour tints the
# card's header band, and a saturated background behind a card of small text is
# tiring to read.
colours = {'api-gateway':'#7dd3fc','auth-service':'#a78bfa','billing':'#fbbf24',
           'web-dashboard':'#34d399','voice-relay':'#f472b6','search-index':'#60a5fa',
           'ml-pipeline':'#fb923c','feature-store':'#22d3ee','docs-site':'#c4b5fd',
           'infra-terraform':'#94a3b8','release-notes':'#f87171','cad-viewer':'#fdba74',
           'market-watch':'#86efac','shell':'#e2e8f0','portal-gateway':'#93c5fd',
           'usage-widget':'#f9a8d4','editor-bridge':'#a5b4fc','dictation':'#fca5a5',
           'company-lookup':'#5eead4','push-notify':'#d8b4fe','home-hub':'#fde047',
           'grid-sim':'#67e8f9','agent-runner':'#bef264','user-stats':'#fdba74',
           'admin-console':'#c7d2fe','lookup-grab':'#f0abfc','newsroom':'#7dd3fc',
           'aider-trial':'#94a3b8','opencode-trial':'#94a3b8','amazonq-trial':'#94a3b8',
           'inventory':'#fbbf24','crawl-test':'#94a3b8','session-manager':'#a78bfa',
           'social-sync':'#60a5fa','checklists':'#94a3b8','disk-tree':'#94a3b8',
           'disk-tree-gen':'#94a3b8','display-switch':'#94a3b8','public-site':'#94a3b8',
           'codex-trial':'#94a3b8','documents':'#94a3b8','downloads':'#94a3b8',
           'legacy-api':'#94a3b8','home-inventory':'#34d399','agent-comms':'#94a3b8',
           'folder-perms':'#94a3b8','budget-2026':'#fbbf24','form-builder':'#c4b5fd',
           'election-map':'#94a3b8','tui-manager':'#a78bfa','discord-bot':'#bef264',
           'pool-booking':'#5eead4','vm-manager':'#93c5fd'}

def mk(i, name, agent, status, repo, gid='', fav=False, tabs=()):
    o = dict(tmpl)
    o.update(id=f'd{i}', name=name, agent=agent, status=status,
             path=f'{P}/{repo}', group_id=gid, color=colours.get(name,''),
             bg_color='', full_row_color=False, favorite=fav,
             resume_session_id='', base_commit_sha='')
    # snake_case, as the store writes them. Spelled only in camelCase these
    # popped nothing, so every demo session inherited the template's real
    # tabs - a tab name from the author's own config reached a screenshot
    # meant to contain nothing real.
    for k in ('followed_windows', 'followedWindows', 'windows',
              'tab_order', 'notes', 'main_window_stopped'):
        o.pop(k, None)
    # Tabs, because a session without them looks nothing like a real one: the
    # sidebar row collapses to a single line and the tab strip is empty, which
    # is the opposite of what this app is for.
    if tabs:
        o['followed_windows'] = [
            {'index': n + 1, 'agent': ag, 'name': nm, 'custom_command': '',
             'auto_yes': False, 'resume_session_id': '', 'hide_status_bar': 0,
             'work_dir': ''}
            for n, (nm, ag) in enumerate(tabs)]
    return o

# Tab shapes follow how the app is actually used: several agents and a shell
# in one session, not a single agent on its own.
# The shape of a real working set: a few dozen sessions, a third of them
# running, most with several tabs. A demo of five empty sessions says nothing
# about what this is for.
def T(*names):
    return [(n, a) for n, a in names]

CL, CX, GM, TM = 'claude', 'codex', 'gemini', 'terminal'
instances = [
    mk(1,'cad-viewer',CL,'stopped','cad-viewer','g1',False,
       T(('build',TM),('review',GM))),
    mk(2,'market-watch',CL,'stopped','market-watch','g2',False,T(('Terminal',TM))),
    mk(3,'shell',TM,'running','misc','g1',True,T(('Terminal',TM),('Terminal',TM))),
    mk(4,'editor-bridge',CL,'running','editor-bridge','g2',True,
       T(('cmd',TM),('claude tab',CL),('save test',CL),('codex tab',CX),
         ('claude tab',CL),('gemini test',GM))),
    mk(5,'cad-suite',CL,'running','cad-viewer','g1',True,
       T(('Terminal',TM),('codex tab',CX),('claude tab',CL),('Terminal',TM),
         ('codex tab',CX),('Terminal',TM),('Terminal',TM),('nesting',CL),
         ('claude tab',CL),('nesting codex',CX))),
    mk(6,'billing',CL,'running','billing','g4',True,
       T(('backend',TM),('database',CL),('frontend',TM),('port review',CL),
         ('codex',CX),('crawling',CL))),
    mk(7,'voice-relay',CL,'running','voice-relay','',True,
       T(('claude tab',CL),('Terminal',TM),('claude tab',CL),('claude tab',CL),
         ('claude tab',CL),('claude tab sonnet',CL),('codex tab',CX))),
    mk(8,'portal-gateway',CL,'running','portal-gateway','g2',True,
       T(('claude tab',CL),('codex tab',CX),('Terminal',TM))),
    mk(9,'usage-widget',CL,'running','usage-widget','g1',True,T(('Terminal',TM))),
    mk(10,'notes-app',CL,'stopped','misc','g2',False,
       T(('Terminal',TM),('codex tab',CX),('codex tab',CX))),
    mk(11,'dictation',CL,'running','dictation','g2',True,T(('cmd',TM))),
    mk(12,'company-lookup',CL,'running','company-lookup','g1',True,
       T(('codex tab',CX),('Terminal',TM))),
    mk(13,'push-notify',CL,'running','push-notify','g1',True,
       T(('Terminal',TM),('codex',CX))),
    mk(14,'home-hub',CL,'running','home-hub','g2',True,T(('claude 2',CL))),
    mk(15,'grid-sim',CL,'running','grid-sim','g5',True,
       T(('Terminal',TM),('codex tab',CX))),
    mk(16,'agent-runner',CL,'stopped','agent-runner','g4',True,T(('Terminal',TM))),
    mk(17,'user-stats',CL,'stopped','user-stats','g1',True,T(('Terminal',TM))),
    mk(18,'admin-console',CL,'running','admin-console','g2',True,T(('Terminal',TM))),
    mk(19,'lookup-grab',CL,'stopped','company-lookup','g2',True,
       T(('codex tab',CX),('Terminal',TM))),
    mk(20,'newsroom',CL,'stopped','newsroom','g2',True),
    mk(21,'aider-trial','aider','stopped','misc','g1'),
    mk(22,'opencode-trial','opencode','stopped','misc','g1'),
    mk(23,'amazonq-trial','amazonq','stopped','misc','g1'),
    mk(24,'inventory',CL,'stopped','inventory','g4',True,
       T(('Terminal',TM),('codex tab',CX))),
    mk(25,'crawl-test',CL,'stopped','misc','g4'),
    mk(26,'session-manager',CL,'stopped','session-manager','g2'),
    mk(27,'social-sync',CL,'stopped','social-sync','g3',True,T(('Terminal',TM))),
    mk(28,'push-notify-old',CL,'stopped','push-notify','g6'),
    mk(29,'checklists',CL,'stopped','misc','g4'),
    mk(30,'disk-tree',CL,'stopped','misc','g6',False,T(('cmd',TM))),
    mk(31,'disk-tree-gen',CL,'stopped','misc','g6'),
    mk(32,'display-switch',CL,'stopped','misc','g6'),
    mk(33,'public-site',GM,'stopped','misc','g2'),
    mk(34,'codex-trial',CX,'stopped','misc','g1'),
    mk(35,'documents',CL,'stopped','misc','g2'),
    mk(36,'downloads',CL,'stopped','misc','g2'),
    mk(37,'legacy-api',CL,'stopped','misc',''),
    mk(38,'home-inventory',CL,'running','home-inventory','',True,
       T(('claude test 2',CL))),
    mk(39,'agent-comms',CL,'stopped','misc','g1'),
    mk(40,'folder-perms',CL,'running','misc','g1'),
    mk(41,'budget-2026',CL,'running','budget-2026','g1',True),
    mk(42,'form-builder',CL,'stopped','form-builder','g3',True),
    mk(43,'election-map',CX,'stopped','misc','g2',False,
       T(('Terminal',TM),('claude tab',CL))),
    mk(44,'tui-manager',CL,'running','tui-manager','g1'),
    mk(45,'discord-bot',CL,'running','discord-bot','g7',False,
       T(('Terminal',TM),('claude tab',CL))),
    mk(46,'pool-booking',CL,'running','pool-booking','g2'),
    mk(47,'vm-manager',CL,'running','vm-manager','g2',False,
       T(('Terminal',TM),('codex tab',CX),('Terminal',TM),('Terminal',TM))),
    mk(48,'ml-pipeline',CL,'running','ml-pipeline','g1',True,
       T(('training',TM),('claude tab',CL),('codex tab',CX),('eval',CL))),
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

# The pane the screenshots are taken in, in characters. Measured from the
# window the app opens at 2560x1385: the terminal area is about 150 columns
# by 60 rows at the default font size.
DEMO_COLS=150
DEMO_ROWS=60

echo "==> starting tmux sessions for the running ones"
# The app looks up a session's multiplexer session by its instance id
# (Instance.TmuxSessionName returns the id), so the names must match d1, d2, ...
declare -A PATHS=([d3]=misc [d4]=editor-bridge [d5]=cad-viewer [d6]=billing
                  [d7]=voice-relay [d8]=portal-gateway [d9]=usage-widget
                  [d11]=dictation [d12]=company-lookup [d13]=push-notify
                  [d14]=home-hub [d15]=grid-sim [d18]=admin-console
                  [d38]=home-inventory [d40]=misc [d41]=budget-2026
                  [d44]=tui-manager [d45]=discord-bot [d46]=pool-booking
                  [d47]=vm-manager [d48]=ml-pipeline)
# A tab in the store is only half of one: the app reads its content from a
# multiplexer window of the same index, and without it the tab strip is there
# but every tab opens on nothing.
declare -A TAB_COUNT=([d3]=2 [d4]=6 [d5]=10 [d6]=6 [d7]=7 [d8]=3 [d9]=1
                      [d11]=1 [d12]=2 [d13]=2 [d14]=1 [d15]=2 [d18]=1
                      [d38]=1 [d40]=0 [d41]=0 [d44]=0 [d45]=2 [d46]=0
                      [d47]=4 [d48]=4)
for s in "${!RUNNING_OUTPUT[@]}"; do
  script="$DEMO/s_$s.sh"
  printf '%s\n' "${RUNNING_OUTPUT[$s]}" > "$script"
  # -x/-y size the window for the pane it will be photographed in. Without
  # them tmux opens at its 80x24 default, the transcript wraps short and the
  # bottom two-thirds of the terminal sit empty in every screenshot.
  tmux new-session -d -s "$s" -n "claude" -x "$DEMO_COLS" -y "$DEMO_ROWS" \
    -c "$PROJECTS/${PATHS[$s]}" "bash $script" 2>/dev/null
  # Manual, or tmux drags the window back to the size of whichever client
  # attaches next.
  tmux set-option -t "$s" -w window-size manual 2>/dev/null
  # -n names the window. Without it every tab reads "bash", which is both
  # wrong and the one thing a screenshot of a multi-agent session must not say.
  declare -a TAB_NAMES=('claude tab' 'codex tab' 'Terminal' 'eval' 'tests' 'notebook')
  for ((w = 1; w <= ${TAB_COUNT[$s]:-0}; w++)); do
    tab="${TAB_CONTENT[$(( (w % 6) + 1 ))]:-term_test.sh}"
    tmux new-window -d -t "$s:$w" -n "${TAB_NAMES[$(( (w - 1) % 6 ))]}" \
      -c "$PROJECTS/${PATHS[$s]}" "bash $CONTENT/$tab" 2>/dev/null
    tmux resize-window -t "$s:$w" -x "$DEMO_COLS" -y "$DEMO_ROWS" 2>/dev/null
  done
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
