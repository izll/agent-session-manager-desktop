# Beküldendő anyagok — awesome listák

Ez a fájl a beküldéshez előkészített szövegeket tartalmazza. Nem a repó
dokumentációja, hanem munkaanyag: ha a beküldések megtörténtek, nyugodtan
törölhető.

**A beküldést neked kell megtenned** — az egyik lista kifejezetten tiltja a
parancssori eszközöket, a többinél pedig a te nevedben nyitott pull request
lenne, ami a te döntésed.

---

## 1. awesome-ai-devtools (⭐ 3.9k) — a legreálisabb kezdés

**Repó:** https://github.com/jamesmurdza/awesome-ai-devtools
**Menet:** fork → szerkesztés → pull request
**Feltétel:** nincs kimondva; a lista sok kis projektet tartalmaz

### Hova kerüljön

Két külön szakasz, mindkét projektnek megvan a helye. Egy PR-ben mindkettő
mehet, vagy külön-külön — a lista mindkettőt elfogadja.

#### `### CLI Utilities` szakasz végére (a TUI):

```markdown
- [Agent Session Manager](https://github.com/izll/agent-session-manager) — Terminal UI for running several AI coding agents side by side in tmux. Claude, Gemini, Aider, Codex, Amazon Q and OpenCode, each in its own session with live preview, resume, activity detection and per-project workspaces. Go + Bubble Tea.
```

#### `## Desktop & Mobile Applications` szakasz végére (a desktop):

```markdown
- [ASMGR Desktop](https://github.com/izll/agent-session-manager-desktop) — Desktop app for running a team of AI coding agents, each in its own live terminal that survives closing the window. Attention inbox with one-click replies, mobile push, side-by-side diff with per-block revert, file browser and 20 languages. Wails + Svelte + xterm.js, for Linux, macOS and Windows.
```

---

## 2. awesome-claude-code (⭐ 52k) — a legnagyobb, de a legszigorúbb

**Repó:** https://github.com/hesreallyhim/awesome-claude-code
**Menet:** **kizárólag a webes issue-űrlapon.** A `CONTRIBUTING.md` szó szerint
figyelmeztet, hogy a `gh` CLI használata ideiglenes kitiltást vonhat maga után,
és pull requestet sem fogadnak.

**Űrlap:** https://github.com/hesreallyhim/awesome-claude-code/issues/new?template=recommend-resource.yml

**Egyszerre csak EGY erőforrás ajánlható.** Ha mindkettőt be akarod küldeni,
külön alkalommal kell.

### Feltételek — mindkét projekt megfelel

| | Feltétel | Desktop | TUI |
|---|---|---|---|
| Kor | 14+ nap az első committól | ✅ ~7 hét | ✅ ~8 hónap |
| Aktivitás | további commitok az első nap után | ✅ 269 commit | ✅ 59 commit |
| *vagy* | 100+ csillag | ❌ 0 | ❌ 4 |

Az életkor-feltétel teljesül, tehát a csillagszám nem kizáró ok.

### Amit érdemes tudni előre

A `CONTRIBUTING.md` szokatlanul őszinte erről:

> „Túl sokan gondolják így: (i) építs valami jót; (ii) küldd be az Awesome
> Claude Code-ra; (iii) fogadják el, mert jó; (iv) jönnek a felhasználók. A
> valószínűbb sorrend viszont: (i) építs valami jót; (ii) szerezz
> felhasználókat; (iii) küldd be. Ha a listára kerülés bármely része a projekted
> promóciós stratégiájának, legyen tartalék terved."

Emellett kiemelik, hogy elsősorban olyan projekteket keresnek, amelyek **Claude
Code sajátos képességeire** építenek. Az ASMGR ebből a szempontból erős: a fork
(`--fork-session`), a beszélgetés-folytatás, a háttérügynökök kezelése és a
`--dangerously-skip-permissions` állapotának élő kijelzése mind Claude
Code-specifikus.

### Javasolt szöveg az űrlaphoz

**Melyiket küldd be először:** a **desktopot**. Több Claude Code-specifikus
funkciója van, és vizuálisan is megmutatható (van képernyőkép a README-ben).

- **Name:** ASMGR Desktop
- **Link:** https://github.com/izll/agent-session-manager-desktop
- **Category:** Tooling / Workflows (amelyik a legközelebb áll)

**Description:**

```
A desktop app for running several Claude Code sessions at once, each in its own
live terminal inside tmux, so they keep working when the window closes.

Claude-specific features: fork a conversation into a new tab or session
(--fork-session), resume any past conversation, a panel for background agents
(--bg / Ctrl+B) that can attach one into a tab, a live YOLO indicator that
tracks the bypass-permissions state as you toggle it, and a rate-limit window
showing how much of your Claude usage is left.

An attention inbox collects every tab waiting on input and answers yes/no/Enter
from the dropdown without switching tabs; the same notifications can go to your
phone. Also has a side-by-side diff with per-block revert, a file browser with
syntax highlighting, and a UI translated into 20 languages.

Wails + Svelte + xterm.js. Linux, macOS and Windows.
```

---

## 3. awesome-generative-ai (⭐ 12k) — csak a Discoveries listára

**Repó:** https://github.com/steven2358/awesome-generative-ai
**Menet:** pull request

A **főlista** feltétele legalább **1000 követő** — ez most nem teljesül. A
`CONTRIBUTING.md` szerint az ez alatti projektek a *Discoveries* listára
kerülnek. Ha ez is megéri, ugyanaz a szöveg használható, mint az
awesome-ai-devtools-nál.

---

## 4. awesome-ai-agents — kihagyva

**Repó:** https://github.com/e2b-dev/awesome-ai-agents

Megnéztem, és **nem illik oda**: a lista autonóm ügynökökről szól (olyan
rendszerekről, amelyek maguktól végeznek feladatokat), nem az ügynökök
futtatását segítő eszközökről. A beküldés valószínűleg elutasításra kerülne, és
a téves helyre küldés a többi listánál is ronthatja a megítélést.

---

## Sorrend, amit javaslok

1. **awesome-ai-devtools** — a legkisebb ellenállás, mindkét projekt mehet
2. **awesome-claude-code** — a desktoppal, az űrlapon; ha megy, később a TUI
3. **awesome-generative-ai** — csak ha a Discoveries lista is megéri

Egy megjegyzés: ezek a listák a felfedezhetőséget javítják, de a Google
találatokat leginkább az hozza, ha **máshonnan is hivatkoznak** a projektre —
egy Reddit- vagy Hacker News-bejegyzés, blogposzt, vagy egy videó többet ér,
mint egy listasor.
