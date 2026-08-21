import {
  createTerminal,
  attachToSession,
  detachFromSession,
  fitTerminal,
  resendTerminalSize,
  sendVisibility,
  clearAwaitingRedraw,
  type TerminalInstance
} from './terminal';
import type { Terminal } from '@xterm/xterm';
import { themeFor, fontSizeFor, terminalFontStack } from './terminal';
import { LogFrontend, RedrawWindow } from '../../../wailsjs/go/main/App';

// Surface pool errors in the backend log file too — the packaged build has
// no devtools console, so console.error alone is invisible.
function logPoolError(msg: string, e: unknown): void {
  console.error(msg, e);
  try { LogFrontend(`${msg}: ${e}`); } catch { /* bridge not ready */ }
}

// Drop the renderer's cached glyphs so the next frame redraws them all.
//
// Both accelerated renderers keep a texture atlas of already-drawn glyphs. It
// is keyed on the attributes in play when a glyph was first drawn, and xterm
// does not always invalidate it when those attributes change underneath it
// (xterm.js#3548) — the stale atlas then renders characters as blank cells.
// A no-op on the DOM renderer, which has no atlas.
function clearGlyphCache(terminal: Terminal): void {
  try {
    terminal.clearTextureAtlas();
  } catch (e) {
    // Never let a repaint hint break the settings change that triggered it.
    logPoolError('pool: clearing glyph cache failed', e);
  }
}

/**
 * Ask the multiplexer to repaint a pane, without sending it any input.
 *
 * Debounced per pane: the settle step can run twice for one switch (once when
 * the size lands, once from the timeout path), and a second redraw a few
 * milliseconds later buys nothing.
 */
const lastRedrawAt = new Map<string, number>();

function requestRedraw(entry: PoolEntry): void {
  const ti = entry.terminalInstance;
  const sessionId = ti.sessionId;
  if (!sessionId) return;
  const windowIdx = ti.windowIdx ?? 0;

  const key = `${ti.projectId ?? ''}:${sessionId}:${windowIdx}`;
  const now = Date.now();
  const previous = lastRedrawAt.get(key) ?? 0;
  if (now - previous < 500) return;
  lastRedrawAt.set(key, now);

  void RedrawWindow(sessionId, windowIdx, entry.projectId).catch((e) => {
    // Never let a repaint hint break the switch that triggered it: a session
    // that stopped between the switch and this call fails here routinely.
    logPoolError('pool: redraw request failed', e);
  });
}

export interface PoolEntry {
  terminalInstance: TerminalInstance;
  containerEl: HTMLDivElement;
  key: string;
  projectId: string;
  sessionId: string;
  windowIdx: number;
  /** Palette inputs for this pane, so a settings change can re-resolve it. */
  themeCtx: { tabTheme?: string; agent?: string; fontSize?: number };
}

export class TerminalPool {
  private entries = new Map<string, PoolEntry>();
  private parentEl: HTMLElement;
  private activeKey: string | null = null;
  private showGeneration = 0;
  private disposed = false;
  /** Frame handle for fitActive()'s wait for the container to stop resizing. */
  private fitFrame: number | undefined;
  private connecting = new Map<string, Promise<void>>();
  /** The currently armed device-pixel-ratio watcher, released on dispose. */
  private pixelRatioWatch: { mql: MediaQueryList; onChange: () => void } | null = null;
  private terminalOptions: Partial<Terminal['options']>;
  /**
   * Told whether the visible pane is waiting for its redraw, so the view can
   * show a spinner. Set by the owner; the pool reports the ACTIVE pane only,
   * since a hidden one waiting has nothing to show for it.
   */
  onAwaitingRedraw?: (waiting: boolean) => void;

  constructor(parentEl: HTMLElement, terminalOptions: Partial<Terminal['options']> = {}) {
    this.parentEl = parentEl;
    this.terminalOptions = terminalOptions;
    this.watchPixelRatio();
  }

  /**
   * Rebuild glyph caches when the device pixel ratio changes.
   *
   * The atlas stores glyphs rasterised for the ratio in effect when they were
   * drawn; moving the window to a display with a different density, or zooming,
   * leaves it holding glyphs at the wrong scale, which renders as corrupt or
   * missing characters (xterm.js#2137). There is no event for this, so the
   * documented approach is a matchMedia query on the current ratio that is
   * re-armed each time it fires.
   */
  private watchPixelRatio(): void {
    if (typeof window.matchMedia !== 'function') return;
    const arm = () => {
      if (this.disposed) return;
      const mql = window.matchMedia(`(resolution: ${window.devicePixelRatio}dppx)`);
      const onChange = () => {
        mql.removeEventListener('change', onChange);
        if (this.pixelRatioWatch?.mql === mql) this.pixelRatioWatch = null;
        if (this.disposed) return;
        for (const entry of this.entries.values()) {
          clearGlyphCache(entry.terminalInstance.terminal);
        }
        arm(); // the ratio moved, so watch for the next change from here
      };
      this.pixelRatioWatch = { mql, onChange };
      mql.addEventListener('change', onChange);
    };
    try { arm(); } catch (e) { logPoolError('pool: watching pixel ratio failed', e); }
  }

  private makeKey(projectId: string, sessionId: string, windowIdx: number): string {
    // JSON avoids separator ambiguity and makes the project identity part of
    // cache ownership. Session IDs are not globally unique across projects.
    return JSON.stringify([projectId, sessionId, windowIdx]);
  }

  /** Ensure exactly one entry is visible (the activeKey), all others hidden */
  private applyVisibility(): void {
    for (const [key, entry] of this.entries) {
      const isActive = key === this.activeKey;
      if (isActive) {
        entry.containerEl.style.display = 'block';
        entry.containerEl.style.zIndex = '1';
        entry.containerEl.style.setProperty('content-visibility', 'visible');
      } else {
        entry.containerEl.style.display = 'none';
        entry.containerEl.style.zIndex = '0';
        // NOTE: we deliberately do NOT use `content-visibility: hidden` here.
        // It took hidden terminals out of the render tree for a perf win, but on
        // this WebKit toggling it back to visible sometimes failed to repaint —
        // the tab stayed permanently BLACK after switching to it. The perf
        // reason is moot now anyway: a hidden tab's PTY output is dropped at the
        // backend (see sendVisibility), so it does no rendering work regardless.
        // Plain display:none reliably repaints on show.
        entry.containerEl.style.setProperty('content-visibility', 'visible');
      }
      // Pair the DOM visibility with the xterm write gate — keeps hidden
      // tabs from spending CPU on off-screen canvas renders.
      const ti = entry.terminalInstance;
      const wasVisible = ti.visible;
      ti.visible = isActive;
      // Tell the BACKEND too, so a hidden tab's PTY output is dropped at the
      // source. Without this the backend keeps streaming a background agent's
      // flood over the WebSocket, and every frame is dispatched on the webview's
      // single main thread — starving the foreground tab's keystrokes (the
      // user-visible asymmetry: a busy background agent made typing in the
      // visible tab unbearably laggy). Only notify on an actual change.
      if (isActive !== wasVisible) {
        sendVisibility(ti, isActive);
      }
      if (isActive && !wasVisible) {
        // A tab returning from the background is a moment behind: the backend
        // held its output while it was away and sends it now. Mark it as
        // waiting so the view can say so; the first byte clears it.
        //
        // The screen is NOT cleared here, deliberately.
        //
        // It was, back when a hidden tab's output was dropped and recovery
        // meant asking the program to redraw itself: the leftovers of the old
        // frame had to go, because nothing else would overwrite them. Now the
        // bytes produced while hidden are held and replayed, and those are
        // differences against the screen as it was — clearing it first would
        // leave the replay writing into a blank pane, so most of the content
        // would simply be missing.
        // Reassigned on every switch, not only when starting a fresh wait: it
        // closes over `key`, so one left over from an earlier tab measures
        // activeKey against the wrong pane and could never fire again.
        ti.onAwaitingRedraw = (waiting) => {
          // Only the pane on screen; a background one finishing its wait
          // must not clear a spinner belonging to the visible tab.
          if (this.activeKey === key) this.onAwaitingRedraw?.(waiting);
        };
        if (!ti.awaitingRedraw) {
          ti.awaitingRedraw = true;
          this.onAwaitingRedraw?.(true);
        }
        // Give up after a moment. The wait ends on the first byte back, which
        // is the common case and the earliest signal — but a tab whose program
        // has nothing to say sends none, and its pane is already correct, so
        // the spinner sat over a screen that had finished. Long enough that a
        // real replay still clears it first.
        if (ti.awaitingRedrawTimer) clearTimeout(ti.awaitingRedrawTimer);
        ti.awaitingRedrawTimer = setTimeout(() => clearAwaitingRedraw(ti), 1200);
        const flush = (ti as any)._flushHidden as (() => void) | undefined;
        if (flush) flush();
        // Re-announce this tab's size on every switch to it.
        //
        // Needed because psmux sizes a whole SESSION, not a window: measured
        // with two clients on one session, a size sent by either applied to
        // BOTH windows, and window-size manual did not change that. So opening
        // or switching tabs leaves the other tab's client drawing at a size
        // that is no longer in force — the black pane that only came right
        // after resizing the window by hand, which was simply the next thing
        // to re-send a size.
        //
        // Deferred a frame so the container has been laid out; fitTerminal
        // measures the element and would otherwise read the hidden geometry.
        requestAnimationFrame(() => {
          if (this.activeKey === key) fitTerminal(ti, 'pool-attach');
        });
      }
    }
  }

  async getOrCreate(projectId: string, sessionId: string, windowIdx: number, themeCtx: { tabTheme?: string; agent?: string; fontSize?: number } = {}): Promise<PoolEntry> {
    if (this.disposed) throw new Error('terminal pool is disposed');
    const key = this.makeKey(projectId, sessionId, windowIdx);
    let entry = this.entries.get(key);
    if (entry) {
      const pending = this.connecting.get(key);
      if (pending) {
        await pending;
        entry = this.entries.get(key);
      }
      if (entry?.terminalInstance.ws?.readyState === WebSocket.OPEN) {
        return entry;
      }
      // A second split pane may have replaced this target's backend
      // connection. Its onclose leaves the cached xterm entry behind with a
      // null/closed socket; displaying that stale entry looks frozen until a
      // manual detach/attach. Evict it and create a fresh connection instead.
      if (entry) {
        this.entries.delete(key);
        if (this.activeKey === key) this.activeKey = null;
        this.teardownEntry(entry);
      }
    }

    // Create a new DOM container
    const containerEl = document.createElement('div');
    containerEl.className = 'terminal-pool-entry';
    containerEl.style.display = 'none';
    containerEl.style.width = '100%';
    containerEl.style.height = '100%';
    containerEl.style.position = 'absolute';
    containerEl.style.top = '0';
    containerEl.style.left = '0';
    containerEl.style.zIndex = '0';
    // Isolate this subtree's layout & paint so an xterm update repaints only
    // the terminal region instead of the whole 2560×1085 window (profiling
    // showed every keystroke echo doing a full-window Paint ~27ms). We use
    // `layout paint` (NOT `strict`, which also adds `size` containment and
    // would zero out the explicit 100% height). The translateZ promotes it
    // to its own compositor layer so the paint stays local.
    containerEl.style.contain = 'layout paint';
    containerEl.style.transform = 'translateZ(0)';
    this.parentEl.appendChild(containerEl);

    // Create xterm instance
    const terminalInstance = createTerminal(containerEl, this.terminalOptions, themeCtx);
    // Start hidden — applyVisibility() will flip this when show() runs.
    terminalInstance.visible = false;

    entry = { terminalInstance, containerEl, key, projectId, sessionId, windowIdx, themeCtx };
    this.entries.set(key, entry);

    // Attach WebSocket. On failure EVICT the entry: leaving it in the map
    // would poison the pool — every later show()/getOrCreate() would return
    // this dead, never-connected entry (permanently black terminal) until a
    // manual detach/attach happened to rebuild it.
    const attachPromise = attachToSession(terminalInstance, sessionId, windowIdx, projectId);
    this.connecting.set(key, attachPromise);
    try {
      await attachPromise;
    } catch (err) {
      this.entries.delete(key);
      if (this.activeKey === key) {
        this.activeKey = null;
      }
      // attachToSession can fail after the socket has opened (for example while
      // installing xterm handlers). Cleanup alone only disposes the renderer;
      // detach also closes that partially-owned socket and invalidates any
      // reconnect it managed to schedule.
      try { await detachFromSession(terminalInstance); } catch (e) { logPoolError('pool attach rollback: detach failed', e); }
      try { terminalInstance.cleanup(); } catch { /* already torn down */ }
      containerEl.remove();
      throw err;
    } finally {
      if (this.connecting.get(key) === attachPromise) {
        this.connecting.delete(key);
      }
    }
    if (this.disposed || this.entries.get(key) !== entry) {
      this.entries.delete(key);
      this.teardownEntry(entry);
      throw new Error('terminal pool connection was cancelled');
    }

    return entry;
  }

  async show(projectId: string, sessionId: string, windowIdx: number, shouldFocus: boolean | (() => boolean) = true, themeCtx: { tabTheme?: string; agent?: string; fontSize?: number } = {}): Promise<void> {
    if (this.disposed) return;
    const canFocus = () => typeof shouldFocus === 'function' ? shouldFocus() : shouldFocus;
    const key = this.makeKey(projectId, sessionId, windowIdx);

    // If already active and still connected, just fit. A split pane may have
    // replaced this target's backend connection while the cached entry stayed
    // active in this pool. In that case fall through so getOrCreate() can evict
    // and rebuild it instead of fitting a dead, black terminal.
    if (this.activeKey === key) {
      const entry = this.entries.get(key);
      if (entry?.terminalInstance.ws?.readyState === WebSocket.OPEN) {
        requestAnimationFrame(() => fitTerminal(entry.terminalInstance, 'pool-reuse'));
        if (canFocus()) entry.terminalInstance.terminal.focus();
        return;
      }
    }

    // Claim this generation so stale async calls won't override us
    const gen = ++this.showGeneration;

    // Remember which tab we are leaving, before activeKey is overwritten. If
    // the target has to be created, its attach resizes the whole session (psmux
    // sizes per session, not per window), leaving THIS tab drawing at a size
    // that is no longer in force — the one that went black when a new tab was
    // opened. It gets its size re-sent once the new tab is connected.
    const previousKey = this.activeKey;
    const wasCached = this.entries.has(key);

    // Set intended target immediately (before any async work)
    this.activeKey = key;

    // Hide all entries
    for (const entry of this.entries.values()) {
      entry.containerEl.style.display = 'none';
      entry.containerEl.style.zIndex = '0';
    }

    // Get or create the target entry (async for new entries - WebSocket connect)
    const entry = await this.getOrCreate(projectId, sessionId, windowIdx, themeCtx);

    // If another show() was called while we were awaiting, bail out
    if (this.showGeneration !== gen) return;

    // A newly created tab has just attached, and that attach resized the whole
    // session. Give the tab we came from its size back, so it is not left
    // rendering at the new tab's dimensions. Only on creation: switching
    // between existing tabs does not re-attach and so does not disturb them.
    if (!wasCached && previousKey && previousKey !== key) {
      const previous = this.entries.get(previousKey);
      if (previous) resendTerminalSize(previous.terminalInstance);
    }

    // getOrCreate() clears activeKey when it evicts a stale cached entry. That
    // is normally correct for standalone callers, but this show() still owns
    // the current generation and the freshly-created entry is its intended
    // target. Restore the key before applying visibility; otherwise every
    // entry remains display:none and the switched-to tab is permanently black.
    this.activeKey = key;

    // NOTE: we intentionally keep EVERY opened tab's WebSocket + tmux mirror
    // live (we do NOT tear down inactive tabs). An earlier experiment tore
    // them down to leave only the active tab connected, on the theory that the
    // number of parallel mirrors drove the stutter — but the real cause turned
    // out to be the frontend flush throttle, and the teardown only hurt UX
    // (a ~0.3s reconnect + tmux redraw on every tab switch, and background
    // tabs stopped reflecting live output). Hidden tabs are cheap: their
    // inbound bytes are buffered (not written to xterm) until the tab is shown
    // again — see the hiddenBuffer path in terminal.ts.

    // Apply visibility with the active key
    this.applyVisibility();

    // Fit + refresh after display. The newly-visible container goes from
    // display:none→block, so its real size may not be available for a frame or
    // two. The OLD code did a single check and, if the size wasn't ready yet,
    // skipped fit+refresh entirely — leaving the terminal BLACK until something
    // else (a resize) forced a redraw. That was the intermittent "black tab on
    // switch". Now we RETRY across several frames until the container has a real
    // size, then fit + force a full repaint. Focus happens immediately and
    // unconditionally (it doesn't depend on layout).
    if (canFocus()) entry.terminalInstance.terminal.focus();
    const term = entry.terminalInstance.terminal;
    let tries = 0;
    // Previous frame's container size, so settle() can tell a container that
    // has finished laying out from one still on its way there.
    let lastRect = '';
    const settle = () => {
      if (this.showGeneration !== gen) return; // a newer show() superseded us
      const rect = entry.containerEl.getBoundingClientRect();
      // Also wait for the socket: fitTerminal() drops the resize silently if it
      // is not open yet, and nothing sends it again afterwards — leaving the
      // pane at its previous size with every line wrapped in the wrong place.
      const wsOpen = entry.terminalInstance.ws?.readyState === WebSocket.OPEN;
      // Wait for the container to stop changing size, not merely to be
      // non-zero. Showing a pane while the window is being maximised finds a
      // container that is laid out but not yet at its final width: measured,
      // the pane announced 168x48 seven milliseconds after becoming visible,
      // and the real 221x60 only arrived 2.4 seconds later from the
      // ResizeObserver. The multiplexer had already reflowed for the smaller
      // size by then, and the TUI's frame stayed wrapped for a width the
      // window no longer had.
      //
      // Two consecutive equal frames is not enough on its own here. During a
      // maximise the CONTAINER can hold one intermediate size for several
      // frames while the WINDOW is still growing, so the check passes and the
      // intermediate size goes out anyway (measured: 168x48 from this path
      // after the same guard was already in place). Require the window's own
      // outer size to have settled too — that is the thing actually still in
      // motion, and it is what the container is waiting on.
      const sizeKey = `${Math.round(rect.width)}x${Math.round(rect.height)}`
        + `@${window.outerWidth}x${window.outerHeight}`;
      const stable = sizeKey === lastRect;
      lastRect = sizeKey;
      if (rect.width >= 2 && rect.height >= 2 && wsOpen && stable) {
        fitTerminal(entry.terminalInstance, 'pool-settle');
        // Force a full repaint of the viewport — without this the DOM/canvas
        // renderer can stay blank after display:none→block on some WebKit builds.
        // Drop the glyph cache first: refresh() redraws from the atlas, so on
        // its own it faithfully repaints whatever corruption is already in
        // there. Hiding and showing a pane is exactly when that shows up.
        clearGlyphCache(term);
        term.refresh(0, term.rows - 1);
        // Ask the multiplexer to repaint as well.
        //
        // The refresh above only repaints what this client already holds, and
        // the replay only carries what the pane produced while the tab was
        // away. Neither helps when the buffer itself is what went wrong — a TUI
        // that laid itself out for a size the tab no longer has, say — and the
        // result is the tab that occasionally comes back looking stale.
        //
        // RedrawWindow, not RefreshWindow: it re-announces the size and asks
        // for a repaint without sending the pane any input. RefreshWindow's
        // Ctrl-L would land in an agent's prompt as text, which is why that one
        // stays on the button where the user asks for it.
        requestRedraw(entry);
        // The pane has just been repainted, so there is nothing left to wait
        // for — whether or not the backend had anything to replay.
        clearAwaitingRedraw(entry.terminalInstance);
        if (canFocus()) term.focus();
        return;
      }
      if (++tries < 30) {
        requestAnimationFrame(settle); // ~0.5s of retries max
        return;
      }
      // Out of retries. If the hold-up was only the socket, still fit and
      // repaint: a correctly-sized terminal that has not told the backend yet
      // beats a blank pane, and attach() sends the size itself once its socket
      // opens. Re-measure rather than trusting the rect from the top of this
      // frame — half a second of retries is long enough for layout to change.
      const finalRect = entry.containerEl.getBoundingClientRect();
      if (finalRect.width >= 2 && finalRect.height >= 2) {
        fitTerminal(entry.terminalInstance, 'pool-settle-timeout');
        clearGlyphCache(term);
        term.refresh(0, term.rows - 1);
        requestRedraw(entry);
        clearAwaitingRedraw(entry.terminalInstance);
        if (canFocus()) term.focus();
      }
    };
    requestAnimationFrame(() => {
      if (this.showGeneration !== gen) return;
      requestAnimationFrame(settle);
    });
  }

  /** Focus the active terminal's input. Safe to call any time. */
  focusActive(): void {
    if (!this.activeKey) return;
    const entry = this.entries.get(this.activeKey);
    if (entry) entry.terminalInstance.terminal.focus();
  }

  /**
   * Destroy a single (sessionId, windowIdx) entry. Used when a tab is
   * deleted and another tab will later reuse the same window index — without
   * this the pool would hand back the cached WebSocket bound to the old
   * (now-killed) pane, leaving the user staring at a blank, unresponsive
   * terminal.
   */
  // IMPORTANT (all destroy* methods): the entry must leave `entries` (and
  // `activeKey`) SYNCHRONOUSLY, before the first await. destroyWindow used to
  // delete the map entry only after `await detachFromSession(...)` — a
  // concurrent show()/getOrCreate() running in that window (e.g. the
  // automatic re-show 300ms after a tab restart) would still FIND the dying
  // entry, return it as "alive", and then the tail of the destroy ripped its
  // DOM out from under the user: permanently black terminal.
  //
  // ALSO: every teardown step is isolated in try/catch. xterm's dispose()
  // can throw mid-teardown (the "_linkifier2" race, same one
  // recreateActiveForRenderer guards against). An uncaught throw here used
  // to abort the destroy BEFORE containerEl.remove(), leaving a dead,
  // visible, click-eating container in the DOM (terminal wouldn't focus),
  // and it rejected the caller's await so the automatic re-show never ran.
  private teardownEntry(entry: PoolEntry): void {
    // Cancels the give-up timer too, so a torn-down tab cannot fire a spinner
    // update at a pane that is no longer there.
    clearAwaitingRedraw(entry.terminalInstance);
    lastRedrawAt.delete(entry.key);
    detachFromSession(entry.terminalInstance).catch(e => logPoolError('pool teardown: detach failed', e));
    try { entry.terminalInstance.cleanup(); } catch (e) { logPoolError('pool teardown: cleanup failed (linkifier race?)', e); }
    try { entry.containerEl.remove(); } catch (e) { logPoolError('pool teardown: container remove failed', e); }
  }

  async destroyWindow(sessionId: string, windowIdx: number): Promise<void> {
    const found = [...this.entries].find(([, candidate]) =>
      candidate.sessionId === sessionId && candidate.windowIdx === windowIdx);
    if (!found) return;
    const [key, entry] = found;
    // An unrelated, hidden tab can be cleaned up while the visible one is
    // connecting. Invalidating the global show generation in that case makes
    // the visible show() return before applyVisibility(), leaving it black.
    if (this.activeKey === key) this.showGeneration++;
    this.entries.delete(key);
    if (this.activeKey === key) {
      this.activeKey = null;
    }
    this.teardownEntry(entry);
  }

  async destroy(sessionId: string): Promise<void> {
    // See destroyWindow: only a destroy that owns the intended/visible target
    // may cancel its in-flight show. A delayed cleanup for another session must
    // not cancel the tab the user switched to in the meantime.
    if (this.activeKey && this.entries.get(this.activeKey)?.sessionId === sessionId) this.showGeneration++;
    // Detach map state synchronously first (see note above), then tear down.
    const doomed: PoolEntry[] = [];
    for (const [key, entry] of this.entries) {
      if (entry.sessionId === sessionId) {
        doomed.push(entry);
        this.entries.delete(key);
        if (this.activeKey === key) {
          this.activeKey = null;
        }
      }
    }
    for (const entry of doomed) {
      this.teardownEntry(entry);
    }
  }

  /**
   * Repaint every pooled terminal, re-resolving each one's palette from its
   * own tab/agent context — a settings change can mean a different palette
   * per pane, not one theme for all.
   */
  applyTheme(): void {
    for (const entry of this.entries.values()) {
      try {
        const theme = themeFor(entry.themeCtx?.tabTheme, entry.themeCtx?.agent);
          entry.terminalInstance.terminal.options.theme = theme;
          // The viewport behind the rows is hard-coded to #000 by xterm, so it
          // has to be told the new colour too — otherwise a theme change leaves
          // a black band wherever the rows do not reach.
          if (theme?.background) {
            entry.containerEl?.style.setProperty('--xterm-background', theme.background);
          }
        // The canvas renderer caches rendered glyphs in a texture atlas keyed
        // by colour, and does not invalidate it when the theme changes
        // (xterm.js#3548). The stale entries show up as characters that are
        // simply missing from the pane — including plain ASCII, which is how
        // this is told apart from a font or character-width problem.
        clearGlyphCache(entry.terminalInstance.terminal);
      } catch (e) {
        logPoolError('pool: applying theme failed', e);
      }
    }
  }

  /**
   * Re-apply font sizes after a settings change. Each pane resolves its own
   * size, since a tab may override the global default. Changing the size
   * changes how many rows and columns fit, so every pane is refitted and the
   * new dimensions pushed to tmux — otherwise the pty keeps the old geometry
   * and output wraps at the wrong column.
   */
  /** Apply the current font stack to every open terminal. */
  applyFontFamily(): void {
    const stack = terminalFontStack();
    for (const entry of this.entries.values()) {
      try {
        if (entry.terminalInstance.terminal.options.fontFamily === stack) continue;
        entry.terminalInstance.terminal.options.fontFamily = stack;
        // Cached glyphs were drawn with the old font; keeping them mixes two
        // typefaces in one pane, or drops characters the atlas no longer has.
        clearGlyphCache(entry.terminalInstance.terminal);
        // A different font means a different cell size, so the pane no longer
        // matches its container until it is measured again.
        entry.terminalInstance.fitAddon?.fit();
      } catch (e) {
        logPoolError('pool: applying font family failed', e);
      }
    }
  }

  applyFontSize(): void {
    for (const entry of this.entries.values()) {
      try {
        const size = fontSizeFor(entry.themeCtx?.fontSize, entry.themeCtx?.agent);
        if (entry.terminalInstance.terminal.options.fontSize === size) continue;
        entry.terminalInstance.terminal.options.fontSize = size;
        // Same reason as the font family: the atlas holds glyphs at the old
        // size.
        clearGlyphCache(entry.terminalInstance.terminal);
        entry.terminalInstance.fitAddon?.fit();
      } catch (e) {
        logPoolError('pool: applying font size failed', e);
      }
    }
  }

  async destroyAll(): Promise<void> {
    this.showGeneration++;
    const doomed = [...this.entries.values()];
    this.entries.clear();
    this.activeKey = null;
    for (const entry of doomed) {
      this.teardownEntry(entry);
    }
  }

  /**
   * Recreate the pooled terminals so a renderer change (canvas/webgl/dom) from
   * Settings takes effect — an xterm's renderer is fixed at open(), so the only
   * way to apply a new one is to rebuild the instance. The tmux session keeps
   * running; the re-show re-attaches and tmux redraws (~0.3s).
   *
   * Implemented carefully: xterm.dispose() can throw from its internal linkifier
   * if anything touches the instance mid-teardown (the "_linkifier2" error). We
   * isolate each teardown in try/catch so one failure can't abort the whole
   * recreate and leave a half-built, BLACK terminal. We also remember the active
   * key, fully tear down, then re-show — bumping showGeneration first so any
   * in-flight settle()/rAF from the old terminals can't write to the new ones.
   */
  async recreateActiveForRenderer(
    shouldFocus: boolean | (() => boolean) = true,
    themeCtxOverride?: { tabTheme?: string; agent?: string; fontSize?: number }
  ): Promise<void> {
    const key = this.activeKey;
    if (!key) return;
    const activeEntry = this.entries.get(key);
    if (!activeEntry) return;
    const { projectId, sessionId, windowIdx: widx } = activeEntry;

    // Invalidate any pending async work tied to current entries.
    this.showGeneration++;

    // Keep the pane's palette across the rebuild. It lives on the entry, and
    // every entry is about to be thrown away; show() defaults it to {}, which
    // resolves to the global terminal theme — so without this, switching
    // renderer silently repaints every agent tab in the terminal colours, and
    // they stay that way because nothing re-applies the real one afterwards.
    // The caller's value is preferred — it is resolved from the session, so it
    // reflects a theme changed in this same visit to Settings. The stored one
    // is the fallback for callers that do not pass it.
    const themeCtx = themeCtxOverride ?? this.entries.get(key)?.themeCtx;

    for (const [, entry] of this.entries) {
      try { await detachFromSession(entry.terminalInstance); } catch { /* ignore */ }
      try { entry.terminalInstance.cleanup(); } catch { /* ignore (linkifier dispose race) */ }
      try { entry.containerEl.remove(); } catch { /* ignore */ }
    }
    this.entries.clear();
    this.activeKey = null;

    await this.show(projectId, sessionId, widx, shouldFocus, themeCtx);
  }

  hideAll(): void {
    this.showGeneration++;
    this.activeKey = null;
    this.applyVisibility();
  }

  async dispose(): Promise<void> {
    this.disposed = true;
    if (this.pixelRatioWatch) {
      this.pixelRatioWatch.mql.removeEventListener('change', this.pixelRatioWatch.onChange);
      this.pixelRatioWatch = null;
    }
    // A queued fit would otherwise run against a torn-down entry.
    if (this.fitFrame !== undefined) {
      cancelAnimationFrame(this.fitFrame);
      this.fitFrame = undefined;
    }
    await this.destroyAll();
  }

  fitActive(): void {
    if (!this.activeKey) return;
    const entry = this.entries.get(this.activeKey);
    if (entry) {
      // Skip fit when container is hidden (display: none) — prevents 0×0 resize
      const rect = entry.containerEl.getBoundingClientRect();
      if (rect.width === 0 || rect.height === 0) return;

      // Fit once the container has stopped changing size, not on the first
      // frame after a change.
      //
      // This runs from a ResizeObserver, which fires for every intermediate
      // size a maximise or a drag passes through. Fitting immediately sends
      // whichever one happened to be current: measured, this path announced
      // 168x48 while the window was on its way to 221x60, the multiplexer
      // reflowed for it, and the TUI's already-drawn frame stayed wrapped at
      // that width — the mis-aligned top of the pane that survived a Refresh.
      //
      // Frames rather than a delay, for the same reason as the resize settle
      // in terminal.ts: a fixed timeout encodes an assumed frame rate and
      // expires mid-burst on a slow machine.
      if (this.fitFrame !== undefined) cancelAnimationFrame(this.fitFrame);
      let stableFor = 0;
      // Includes the window's outer size: during a maximise the container can
      // sit at an intermediate size for several frames while the window is
      // still growing, and watching the container alone declares that settled.
      let lastSize = `${Math.round(rect.width)}x${Math.round(rect.height)}`
        + `@${window.outerWidth}x${window.outerHeight}`;
      const settleFit = () => {
        const r = entry.containerEl.getBoundingClientRect();
        if (r.width === 0 || r.height === 0) { this.fitFrame = undefined; return; }
        const size = `${Math.round(r.width)}x${Math.round(r.height)}`
          + `@${window.outerWidth}x${window.outerHeight}`;
        stableFor = size === lastSize ? stableFor + 1 : 0;
        lastSize = size;
        if (stableFor >= 2) {
          this.fitFrame = undefined;
          fitTerminal(entry.terminalInstance, 'pool-fitActive');
          return;
        }
        this.fitFrame = requestAnimationFrame(settleFit);
      };
      this.fitFrame = requestAnimationFrame(settleFit);
    }
  }

  getActive(): PoolEntry | null {
    if (!this.activeKey) return null;
    return this.entries.get(this.activeKey) || null;
  }

  hasEntry(projectId: string, sessionId: string, windowIdx: number): boolean {
    return this.entries.has(this.makeKey(projectId, sessionId, windowIdx));
  }

  get size(): number {
    return this.entries.size;
  }
}
