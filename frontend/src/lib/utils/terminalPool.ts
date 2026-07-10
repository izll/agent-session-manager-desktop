import {
  createTerminal,
  attachToSession,
  detachFromSession,
  fitTerminal,
  sendVisibility,
  type TerminalInstance
} from './terminal';
import type { Terminal } from '@xterm/xterm';
import { LogFrontend } from '../../../wailsjs/go/main/App';

// Surface pool errors in the backend log file too — the packaged build has
// no devtools console, so console.error alone is invisible.
function logPoolError(msg: string, e: unknown): void {
  console.error(msg, e);
  try { LogFrontend(`${msg}: ${e}`); } catch { /* bridge not ready */ }
}

export interface PoolEntry {
  terminalInstance: TerminalInstance;
  containerEl: HTMLDivElement;
  key: string;
}

export class TerminalPool {
  private entries = new Map<string, PoolEntry>();
  private parentEl: HTMLElement;
  private activeKey: string | null = null;
  private showGeneration = 0;
  private terminalOptions: Partial<Terminal['options']>;

  constructor(parentEl: HTMLElement, terminalOptions: Partial<Terminal['options']> = {}) {
    this.parentEl = parentEl;
    this.terminalOptions = terminalOptions;
  }

  private makeKey(sessionId: string, windowIdx: number): string {
    return `${sessionId}:${windowIdx}`;
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
        const flush = (ti as any)._flushHidden as (() => void) | undefined;
        if (flush) flush();
      }
    }
  }

  async getOrCreate(sessionId: string, windowIdx: number): Promise<PoolEntry> {
    const key = this.makeKey(sessionId, windowIdx);
    let entry = this.entries.get(key);
    if (entry) return entry;

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
    const terminalInstance = createTerminal(containerEl, this.terminalOptions);
    // Start hidden — applyVisibility() will flip this when show() runs.
    terminalInstance.visible = false;

    entry = { terminalInstance, containerEl, key };
    this.entries.set(key, entry);

    // Attach WebSocket. On failure EVICT the entry: leaving it in the map
    // would poison the pool — every later show()/getOrCreate() would return
    // this dead, never-connected entry (permanently black terminal) until a
    // manual detach/attach happened to rebuild it.
    try {
      await attachToSession(terminalInstance, sessionId, windowIdx);
    } catch (err) {
      this.entries.delete(key);
      if (this.activeKey === key) {
        this.activeKey = null;
      }
      try { terminalInstance.cleanup(); } catch { /* already torn down */ }
      containerEl.remove();
      throw err;
    }

    return entry;
  }

  async show(sessionId: string, windowIdx: number): Promise<void> {
    const key = this.makeKey(sessionId, windowIdx);

    // If already active, just fit
    if (this.activeKey === key) {
      const entry = this.entries.get(key);
      if (entry) {
        requestAnimationFrame(() => fitTerminal(entry.terminalInstance));
      }
      return;
    }

    // Claim this generation so stale async calls won't override us
    const gen = ++this.showGeneration;

    // Set intended target immediately (before any async work)
    this.activeKey = key;

    // Hide all entries
    for (const entry of this.entries.values()) {
      entry.containerEl.style.display = 'none';
      entry.containerEl.style.zIndex = '0';
    }

    // Get or create the target entry (async for new entries - WebSocket connect)
    const entry = await this.getOrCreate(sessionId, windowIdx);

    // If another show() was called while we were awaiting, bail out
    if (this.showGeneration !== gen) return;

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
    entry.terminalInstance.terminal.focus();
    const term = entry.terminalInstance.terminal;
    let tries = 0;
    const settle = () => {
      if (this.showGeneration !== gen) return; // a newer show() superseded us
      const rect = entry.containerEl.getBoundingClientRect();
      if (rect.width >= 2 && rect.height >= 2) {
        fitTerminal(entry.terminalInstance);
        // Force a full repaint of the viewport — without this the DOM/canvas
        // renderer can stay blank after display:none→block on some WebKit builds.
        term.refresh(0, term.rows - 1);
        term.focus();
        return;
      }
      if (++tries < 30) requestAnimationFrame(settle); // ~0.5s of retries max
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
    detachFromSession(entry.terminalInstance).catch(e => logPoolError('pool teardown: detach failed', e));
    try { entry.terminalInstance.cleanup(); } catch (e) { logPoolError('pool teardown: cleanup failed (linkifier race?)', e); }
    try { entry.containerEl.remove(); } catch (e) { logPoolError('pool teardown: container remove failed', e); }
  }

  async destroyWindow(sessionId: string, windowIdx: number): Promise<void> {
    const key = this.makeKey(sessionId, windowIdx);
    const entry = this.entries.get(key);
    if (!entry) return;
    this.entries.delete(key);
    if (this.activeKey === key) {
      this.activeKey = null;
    }
    this.teardownEntry(entry);
  }

  async destroy(sessionId: string): Promise<void> {
    // Detach map state synchronously first (see note above), then tear down.
    const doomed: PoolEntry[] = [];
    for (const [key, entry] of this.entries) {
      if (key.startsWith(sessionId + ':')) {
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

  async destroyAll(): Promise<void> {
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
  async recreateActiveForRenderer(): Promise<void> {
    const key = this.activeKey;
    if (!key) return;
    const sessionId = key.slice(0, key.lastIndexOf(':'));
    const widx = parseInt(key.slice(key.lastIndexOf(':') + 1), 10);

    // Invalidate any pending async work tied to current entries.
    this.showGeneration++;

    for (const [, entry] of this.entries) {
      try { await detachFromSession(entry.terminalInstance); } catch { /* ignore */ }
      try { entry.terminalInstance.cleanup(); } catch { /* ignore (linkifier dispose race) */ }
      try { entry.containerEl.remove(); } catch { /* ignore */ }
    }
    this.entries.clear();
    this.activeKey = null;

    await this.show(sessionId, widx);
  }

  hideAll(): void {
    this.activeKey = null;
    this.applyVisibility();
  }

  fitActive(): void {
    if (!this.activeKey) return;
    const entry = this.entries.get(this.activeKey);
    if (entry) {
      // Skip fit when container is hidden (display: none) — prevents 0×0 resize
      const rect = entry.containerEl.getBoundingClientRect();
      if (rect.width === 0 || rect.height === 0) return;
      fitTerminal(entry.terminalInstance);
    }
  }

  getActive(): PoolEntry | null {
    if (!this.activeKey) return null;
    return this.entries.get(this.activeKey) || null;
  }

  hasEntry(sessionId: string, windowIdx: number): boolean {
    return this.entries.has(this.makeKey(sessionId, windowIdx));
  }

  get size(): number {
    return this.entries.size;
  }
}
