import { Terminal, type IDisposable } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { SearchAddon } from '@xterm/addon-search';
import { CanvasAddon } from '@xterm/addon-canvas';
import { LogFrontend } from '../../../wailsjs/go/main/App';
import { WebglAddon } from '@xterm/addon-webgl';
import { GetTerminalWSPort, GetTerminalWSToken } from '../../../wailsjs/go/main/App';
import { getTerminalTheme, resolveTerminalTheme, DEFAULT_TERMINAL_THEME, resolveFontSize,
         MIN_FONT_SIZE, MAX_FONT_SIZE, DEFAULT_FONT_SIZE } from './terminalThemes';

// The backend may bind a fallback port if 9753 is taken (e.g. a second
// instance running alongside). Resolve it from the backend, but ONLY cache
// a definitive success. If the Wails bridge isn't ready yet (early startup)
// the call can fail/return garbage; caching the 9753 fallback permanently
// would then break every terminal for the whole session when the backend
// actually bound a different port. So on any non-definitive result we drop
// the cache and let the next attach retry.
let cachedWSPort: number | null = null;
let wsPortInflight: Promise<number> | null = null;
async function getTerminalWSPort(): Promise<number> {
  if (cachedWSPort !== null) return cachedWSPort;
  if (wsPortInflight) return wsPortInflight;

  wsPortInflight = (async () => {
    try {
      const p = await GetTerminalWSPort();
      if (typeof p === 'number' && p > 0) {
        cachedWSPort = p; // definitive — safe to memoize
        return p;
      }
    } catch {
      // bridge not ready / transient — fall through
    }
    return 9753; // best-effort for THIS attempt; not cached
  })();

  try {
    return await wsPortInflight;
  } finally {
    wsPortInflight = null; // allow a fresh attempt next time if uncached
  }
}

// Per-launch terminal auth token. Same caching discipline as the port:
// only memoize a definitive non-empty value, so a transient early-startup
// failure doesn't permanently wedge every terminal with an empty token.
let cachedWSToken: string | null = null;
let wsTokenInflight: Promise<string> | null = null;
async function getTerminalWSToken(): Promise<string> {
  if (cachedWSToken) return cachedWSToken;
  if (wsTokenInflight) return wsTokenInflight;

  wsTokenInflight = (async () => {
    try {
      const t = await GetTerminalWSToken();
      if (typeof t === 'string' && t.length > 0) {
        cachedWSToken = t;
        return t;
      }
    } catch {
      // bridge not ready / transient — fall through, do not cache
    }
    return '';
  })();

  try {
    return await wsTokenInflight;
  } finally {
    wsTokenInflight = null;
  }
}

export interface TerminalInstance {
  terminal: Terminal;
  fitAddon: FitAddon;
  searchAddon: SearchAddon;
  sessionId: string | null;
  windowIdx: number;
  ws: WebSocket | null;
  cleanup: () => void;
  dataDisposable: IDisposable | null;
  resizeDisposable: IDisposable | null;
  // When false, inbound WS messages are buffered instead of written to xterm.
  // Flushed when the instance becomes visible again. Prevents hidden tabs
  // from burning WebKit render cycles on off-screen canvases.
  visible: boolean;
  hiddenBuffer: Uint8Array[];
}

// The terminal renderer is chosen in Settings (gear icon): 'canvas' (default),
// 'webgl' (fastest; can be flaky on some WebKitGTK — glyphs/repaint), or 'dom'
// (most compatible, but Paint-heavy). Set via setTerminalRenderer() from the
// settings store so a new terminal picks up the current choice.
//
// Background: the DOM renderer Paint-dominated WebKitGTK CPU on every Claude
// prompt redraw. Canvas renders to a 2D canvas WebKit repaints normally and is
// much cheaper. WebGL is fastest but on this stack sometimes only repainted
// after a manual resize — kept available so users can try it on their hardware.
let __terminalRenderer: 'canvas' | 'webgl' | 'dom' = 'canvas';
export function setTerminalRenderer(r: 'canvas' | 'webgl' | 'dom'): void {
  if (r === 'canvas' || r === 'webgl' || r === 'dom') __terminalRenderer = r;
}

// Colour palettes (see terminalThemes.ts). A terminal's palette resolves
// most-specific-first: the tab's own override, then the agent-type override,
// then this global base. Settings pushes the whole context here so both new
// and live terminals can be repainted.
let __themeCtx: {
  terminalDefault: string;
  agentDefault: string;
  agentThemes: Record<string, string>;
  customThemes: any[];
} = {
  terminalDefault: DEFAULT_TERMINAL_THEME,
  agentDefault: DEFAULT_TERMINAL_THEME,
  agentThemes: {},
  customThemes: [],
};

export function setTerminalThemeContext(ctx: {
  terminalDefault?: string;
  agentDefault?: string;
  agentThemes?: Record<string, string> | null;
  customThemes?: any[] | null;
  fontSize?: number;
  agentFontSize?: number;
}): void {
  __themeCtx = {
    terminalDefault: ctx.terminalDefault || DEFAULT_TERMINAL_THEME,
    agentDefault: ctx.agentDefault || DEFAULT_TERMINAL_THEME,
    agentThemes: ctx.agentThemes || {},
    customThemes: ctx.customThemes || [],
  };
  __terminalFontSize = ctx.fontSize || 0;
  __agentFontSize = ctx.agentFontSize || 0;
}

/** Separate defaults for terminal and agent tabs; 0 means the built-in one. */
let __terminalFontSize = 0;
let __agentFontSize = 0;

/** The size a tab should render at, given its own override (0 = inherit). */
export function fontSizeFor(tabSize?: number, agent?: string): number {
  return resolveFontSize(tabSize, __terminalFontSize, __agentFontSize, agent);
}

/** Palette for one terminal, given its tab override and agent type. */
export function themeFor(tabTheme?: string, agent?: string) {
  return resolveTerminalTheme({
    tabTheme,
    agent,
    agentThemes: __themeCtx.agentThemes,
    terminalDefault: __themeCtx.terminalDefault,
    agentDefault: __themeCtx.agentDefault,
    customThemes: __themeCtx.customThemes,
  });
}

// Load the configured renderer addon, defensively. xterm measures glyph metrics
// synchronously when the renderer initialises; if the monospace font isn't
// loaded yet those metrics are wrong and glyphs can blur/mis-align — so we wait
// for document.fonts.ready first. Any failure falls back to the DOM renderer.
function loadRenderer(terminal: Terminal): void {
  const mode = __terminalRenderer;
  if (mode === 'dom') return; // no addon = xterm's built-in DOM renderer

  const attach = () => {
    // Terminal may have been disposed while we awaited fonts.
    if (!(terminal as any).element) return;
    try {
      if (mode === 'webgl') {
        const addon = new WebglAddon();
        addon.onContextLoss(() => {
          console.warn('[terminal] WebGL context lost, falling back to DOM');
          try { addon.dispose(); } catch { /* ignore */ }
        });
        terminal.loadAddon(addon);
      } else {
        terminal.loadAddon(new CanvasAddon());
      }
    } catch (e) {
      console.warn(`[terminal] ${mode} addon failed, using DOM renderer:`, e);
    }
  };

  // Wait for fonts so glyph metrics are correct, then attach. Guard the API.
  const fonts: FontFaceSet | undefined = (document as any).fonts;
  if (fonts && fonts.ready && typeof fonts.ready.then === 'function') {
    fonts.ready.then(attach).catch(attach);
  } else {
    attach();
  }
}

export function createTerminal(
  container: HTMLElement,
  options: Partial<Terminal['options']> = {},
  themeCtx: { tabTheme?: string; agent?: string; fontSize?: number } = {},
): TerminalInstance {
  const terminal = new Terminal({
    // cursorBlink triggers a continuous render tick on the xterm canvas every
    // ~500ms even when the terminal is idle — disabled to keep the WebKit
    // renderer quiet when nothing is happening.
    cursorBlink: false,
    fontSize: fontSizeFor(themeCtx.fontSize, themeCtx.agent),
    scrollback: 1000,
    // Low-risk render-cost trims for the DOM renderer on WebKitGTK (no renderer
    // change). Each one removes work from the per-update style/layout/paint
    // pipeline that profiling pinned as the real cost (WebKitWebProcess high
    // CPU while bytes/JS time were ~0):
    //  - smoothScrollDuration 0: a scroll repaints once, not over N animation
    //    frames (an agent that scrolls its panel every redraw would otherwise
    //    multiply repaints).
    //  - allowTransparency false: lets WebKit use the opaque fast path and skip
    //    per-cell alpha compositing (must be set before open()).
    //  - minimumContrastRatio 1: the default, but pinned explicitly — any value
    //    >1 forces a per-fg/bg contrast recompute on every cell render.
    smoothScrollDuration: 0,
    allowTransparency: false,
    minimumContrastRatio: 1,
    fontFamily: 'JetBrains Mono, Menlo, Monaco, Consolas, monospace',
    theme: themeFor(themeCtx.tabTheme, themeCtx.agent),
    ...options
  });

  const fitAddon = new FitAddon();
  terminal.loadAddon(fitAddon);

  // Scrollback search (Ctrl+Shift+L overlay in Terminal.svelte)
  const searchAddon = new SearchAddon();
  terminal.loadAddon(searchAddon);

  terminal.open(container);

  // Renderer chosen in Settings (canvas | webgl | dom). See loadRenderer().
  loadRenderer(terminal);

  // NOTE: fit() is called later by the pool once the container is visible.
  // Calling it here while the container may be display:none produces a
  // 0/tiny size that leaks into the initial WebSocket resize, leading to
  // tmux rendering at ~5 cols wide until another resize happens.

  // Intercept keyboard shortcuts
  terminal.attachCustomKeyEventHandler((event) => {
    // Alt+Up/Down for session navigation
    if (event.altKey && (event.key === 'ArrowUp' || event.key === 'ArrowDown')) {
      window.dispatchEvent(new CustomEvent('terminal-nav', {
        detail: { direction: event.key === 'ArrowUp' ? 'up' : 'down' }
      }));
      return false;
    }

    // Alt+F for search focus
    if (event.altKey && event.key === 'f') {
      if (event.type === 'keydown') {
        const searchInput = document.querySelector('.search-input') as HTMLInputElement;
        searchInput?.focus();
      }
      return false;
    }

    // Shift+Enter: insert a newline instead of submitting.
    // Send the CSI-u (kitty keyboard protocol) encoding of Shift+Enter.
    // Verified end-to-end on this exact path (xterm → WS → tmux attach
    // client PTY → pane running Claude Code): the pane receives it intact
    // and Claude inserts a newline without submitting. The alternatives all
    // failed: bare \n and ESC+CR submit (ink normalizes them to "return"),
    // backslash+CR was chunk-timing dependent (flaky).
    //
    // IMPORTANT: this handler is invoked for keydown, keypress AND keyup of
    // the same physical keystroke. Swallow ALL of them — returning true for
    // the keypress lets xterm ALSO emit its default "\r", so the pane got
    // "newline, then submit", which looked exactly like a plain submit.
    if (event.shiftKey && event.key === 'Enter') {
      if (event.type === 'keydown') {
        (terminal as any)._core.coreService.triggerDataEvent('\x1b[13;2u', true);
      }
      return false;
    }

    return true;
  });

  // Shift+selection → copy to clipboard, the SAFE way.
  //
  // The old approach used terminal.onSelectionChange, which fired on EVERY
  // buffer shift — so a chatty pane (e.g. a task-master MCP dumping JSON) made
  // it call navigator.clipboard.writeText() continuously, and WebKit's
  // document-wide clipboard machinery froze the whole UI. Instead we copy only
  // on an explicit user gesture: a mouseup that ENDED a Shift-held drag. Output
  // can never trigger it, so no freeze. Normal (non-Shift) selection does not
  // auto-copy (use the OS/context menu), matching the requested behaviour.
  let shiftSelecting = false;
  const onMouseDown = (e: MouseEvent) => { shiftSelecting = e.shiftKey; };
  const onMouseUp = () => {
    if (!shiftSelecting) return;
    shiftSelecting = false;
    const sel = terminal.getSelection();
    if (sel && sel.length > 0 && navigator.clipboard) {
      navigator.clipboard.writeText(sel).catch(() => { /* ignore */ });
    }
  };
  container.addEventListener('mousedown', onMouseDown, true);
  container.addEventListener('mouseup', onMouseUp, true);

  // Ctrl+wheel resizes the text, as in a browser or a terminal emulator. The
  // change is reported so the caller can persist it — this handler only knows
  // about one pane, not which session or tab it belongs to.
  const onWheel = (e: WheelEvent) => {
    if (!e.ctrlKey) return;
    // Without this the pane scrolls as well as zooming.
    e.preventDefault();
    const current = terminal.options.fontSize || DEFAULT_FONT_SIZE;
    const next = Math.min(MAX_FONT_SIZE, Math.max(MIN_FONT_SIZE,
      current + (e.deltaY < 0 ? 1 : -1)));
    if (next === current) return; // already at a limit
    terminal.options.fontSize = next;
    // A different size means a different row/column count; without refitting,
    // the pty keeps the old geometry and output wraps at the wrong column.
    try { fitAddon.fit(); } catch { /* not attached yet */ }
    container.dispatchEvent(new CustomEvent('terminal:fontsize', {
      detail: { size: next },
      bubbles: true,
    }));
  };
  // Capture phase, like the mouse handlers above: xterm registers its own
  // non-passive wheel listener on the element it creates inside this
  // container, so a bubble-phase handler here would run after it — or not at
  // all. Capturing on the outer container means we see the event on its way
  // down, before xterm can act on it. Not passive either: the handler calls
  // preventDefault to stop the pane scrolling while zooming.
  container.addEventListener('wheel', onWheel, { capture: true, passive: false });

  return {
    terminal,
    fitAddon,
    searchAddon,
    sessionId: null,
    windowIdx: 0,
    ws: null,
    dataDisposable: null,
    resizeDisposable: null,
    visible: true,
    hiddenBuffer: [],
    cleanup: () => {
      container.removeEventListener('mousedown', onMouseDown, true);
      container.removeEventListener('mouseup', onMouseUp, true);
      container.removeEventListener('wheel', onWheel, true);
      // xterm's dispose() intermittently throws from its internal linkifier
      // ("this._linkifier2.onShowLinkUnderline" is undefined) when a tab is
      // torn down right after an abrupt WebSocket close (1005). The throw
      // aborts dispose() midway, leaving the instance half-disposed so the
      // next attach reuses a broken renderer — the "black tab until manual
      // detach/attach" symptom. Neutralize the linkifier's callbacks first so
      // the risky path can't fire, then dispose defensively.
      try {
        const core = (terminal as any)._core;
        const linkifier = core?._linkifier2 || core?.linkifier2 || core?._linkifier;
        if (linkifier) {
          linkifier.onShowLinkUnderline = () => {};
          linkifier.onHideLinkUnderline = () => {};
        }
      } catch { /* internals shifted between xterm versions — ignore */ }
      try {
        terminal.dispose();
      } catch (e) {
        console.warn('[terminal] dispose threw during cleanup:', e);
      }
    }
  };
}

// Send resize command via WebSocket
function sendResize(ws: WebSocket, cols: number, rows: number) {
  if (ws.readyState === WebSocket.OPEN) {
    // Resize message format: 0x01 + cols (2 bytes big-endian) + rows (2 bytes big-endian)
    const buf = new Uint8Array(5);
    buf[0] = 0x01; // Resize command
    buf[1] = (cols >> 8) & 0xff;
    buf[2] = cols & 0xff;
    buf[3] = (rows >> 8) & 0xff;
    buf[4] = rows & 0xff;
    ws.send(buf);
  }
}

// Tell the backend whether this tab is visible. A hidden tab has its PTY
// output DROPPED at the backend so a background agent's flood can't starve the
// foreground tab's keystrokes on the webview's single main thread. Message
// format: 0x02 + (1 visible | 0 hidden).
export function sendVisibility(terminalInstance: TerminalInstance, visible: boolean): void {
  const ws = terminalInstance.ws;
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(new Uint8Array([0x02, visible ? 1 : 0]));
  }
}

export async function attachToSession(
  terminalInstance: TerminalInstance,
  sessionId: string,
  windowIdx: number
): Promise<void> {
  const { terminal } = terminalInstance;

  // Detach from previous session if any
  if (terminalInstance.ws) {
    await detachFromSession(terminalInstance);
  }

  // Dispose previous handlers
  if (terminalInstance.dataDisposable) {
    terminalInstance.dataDisposable.dispose();
    terminalInstance.dataDisposable = null;
  }
  if (terminalInstance.resizeDisposable) {
    terminalInstance.resizeDisposable.dispose();
    terminalInstance.resizeDisposable = null;
  }

  try {
    // Ask the backend which port it actually bound (may differ from 9753
    // if a fallback was used because the preferred port was busy).
    const port = await getTerminalWSPort();
    const token = await getTerminalWSToken();
    const wsUrl = `ws://127.0.0.1:${port}/terminal?session=${encodeURIComponent(sessionId)}` +
      `&window=${windowIdx}&token=${encodeURIComponent(token)}`;

    const ws = new WebSocket(wsUrl);
    ws.binaryType = 'arraybuffer';

    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(() => {
        ws.close();
        reject(new Error('WebSocket connection timeout'));
      }, 5000);

      ws.onopen = () => {
        clearTimeout(timeout);
        resolve();
      };

      ws.onerror = (e) => {
        clearTimeout(timeout);
        reject(new Error('WebSocket connection failed'));
      };
    });

    terminalInstance.ws = ws;
    terminalInstance.sessionId = sessionId;
    terminalInstance.windowIdx = windowIdx;

    // Sync the backend's hidden/visible state to this instance's current value
    // as soon as the socket is open. A background tab restored at startup may
    // attach while already hidden; without this the backend would default to
    // "visible" and stream its agent's output, starving the foreground tab.
    sendVisibility(terminalInstance, terminalInstance.visible);

    // Clear terminal BEFORE setting onmessage to avoid old content mixing with new
    terminal.clear();

    // Receive data from WebSocket.
    // When this terminal is hidden (another tab is active) we avoid calling
    // terminal.write() on every chunk — that triggers an xterm canvas render
    // even though nothing is on screen, which drives WebKit CPU through the
    // roof when several agents are producing output. Instead we buffer the
    // raw bytes and flush them in one shot when the tab becomes visible.
    // The buffer is capped so a very chatty hidden session can't balloon
    // memory forever; when we overflow we drop everything and ask tmux to
    // redraw on show.
    const HIDDEN_BUFFER_CAP = 512 * 1024; // 512 KB
    let hiddenBytes = 0;
    let hiddenOverflow = false;

    let visibleQueue: Uint8Array[] = [];
    // Low-latency leading-edge flush (the version that tested best): if we
    // haven't written for >= MIN_INTERVAL, flush on the next microtask so an
    // isolated keystroke echo lands essentially immediately; otherwise coalesce
    // a burst with one short timer capped at MIN_INTERVAL (~60fps). No rAF (it
    // added a one-frame input delay) and no burst ramp (it bunched echoes).
    const MIN_INTERVAL_MS = 16;
    let timerHandle: ReturnType<typeof setTimeout> | null = null;
    let microQueued = false;
    let lastFlush = 0;
    const doWrite = () => {
      if (visibleQueue.length === 0) return;
      // Concat and write in one call so xterm only runs one parse/layout cycle.
      let total = 0;
      for (const c of visibleQueue) total += c.byteLength;
      if (total === 0) { visibleQueue = []; return; } // nothing real → no Paint
      const merged = new Uint8Array(total);
      let offset = 0;
      for (const c of visibleQueue) {
        merged.set(c, offset);
        offset += c.byteLength;
      }
      visibleQueue = [];
      lastFlush = performance.now();
      terminal.write(merged);
    };
    const flushVisible = () => {
      timerHandle = null;
      microQueued = false;
      doWrite();
    };
    const scheduleFlush = () => {
      if (timerHandle !== null || microQueued) return;
      const since = performance.now() - lastFlush;
      if (since >= MIN_INTERVAL_MS) {
        microQueued = true;
        queueMicrotask(flushVisible);
      } else {
        timerHandle = setTimeout(flushVisible, MIN_INTERVAL_MS - since);
      }
    };

    ws.onmessage = (event) => {
      const chunk = event.data instanceof ArrayBuffer
        ? new Uint8Array(event.data)
        : new TextEncoder().encode(event.data as string);

      if (terminalInstance.visible) {
        visibleQueue.push(chunk);
        scheduleFlush();
        return;
      }

      if (hiddenOverflow) return;
      hiddenBytes += chunk.byteLength;
      if (hiddenBytes > HIDDEN_BUFFER_CAP) {
        terminalInstance.hiddenBuffer = [];
        hiddenBytes = 0;
        hiddenOverflow = true;
        return;
      }
      terminalInstance.hiddenBuffer.push(chunk);
    };

    // Expose a "become visible" hook via the instance so the pool can call it.
    (terminalInstance as any)._flushHidden = () => {
      if (hiddenOverflow) {
        // Scrollback may have drifted — ask the server side to redraw.
        hiddenOverflow = false;
        hiddenBytes = 0;
        terminalInstance.hiddenBuffer = [];
        // A tmux refresh-client is cheaper than replaying the dropped bytes;
        // sending a resize (0x01) with current size nudges tmux.
        const { cols, rows } = terminal;
        if (cols > 1 && rows > 1 && ws.readyState === WebSocket.OPEN) {
          sendResize(ws, cols, rows);
        }
        return;
      }
      if (terminalInstance.hiddenBuffer.length === 0) return;
      // Fold buffered bytes into the visible queue and schedule a flush.
      for (const c of terminalInstance.hiddenBuffer) {
        visibleQueue.push(c);
      }
      terminalInstance.hiddenBuffer = [];
      hiddenBytes = 0;
      scheduleFlush();
    };

    ws.onclose = (ev) => {
      void LogFrontend(`[term] ws closed code=${ev.code} reason=${ev.reason || '(none)'} clean=${ev.wasClean}`);
      terminalInstance.ws = null;
      terminalInstance.sessionId = null;
    };

    ws.onerror = (e) => {
      console.error('WebSocket error:', e);
    };

    // Send terminal input directly via WebSocket
    terminalInstance.dataDisposable = terminal.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(data);
      }
    });

    // Handle resize - only forward sane sizes to avoid tmux rendering at
    // 0/1/tiny widths if the xterm reports its default 80×24 while still
    // hidden. The real resize lands via fitTerminal once the container is
    // visible.
    terminalInstance.resizeDisposable = terminal.onResize(({ cols, rows }) => {
      if (cols > 1 && rows > 1) {
        sendResize(ws, cols, rows);
      }
    });

    // Send the current size once, now that the socket is open.
    //
    // Nothing else guarantees this. onResize above only fires when the size
    // CHANGES, so a terminal that already measures what it measured last time
    // never announces itself; and fitTerminal() only sends if the socket was
    // already open when the container became visible, which is a race it
    // sometimes loses. The multiplexer is then left at whatever size it had —
    // the size before the window was last closed, or on Windows psmux's 120x30
    // default — and every line wraps in the wrong place until the user resizes
    // the window by hand.
    //
    // Deferred a frame so the container has been laid out: measuring a
    // still-hidden element yields the 80x24 default, which would be worse than
    // sending nothing.
    requestAnimationFrame(() => {
      const { cols, rows } = terminal;
      if (cols > 1 && rows > 1 && ws.readyState === WebSocket.OPEN) {
        sendResize(ws, cols, rows);
      }
    });

  } catch (e) {
    console.error('Failed to attach session:', e);
    throw e;
  }
}

export async function detachFromSession(terminalInstance: TerminalInstance): Promise<void> {
  // Dispose handlers
  if (terminalInstance.dataDisposable) {
    terminalInstance.dataDisposable.dispose();
    terminalInstance.dataDisposable = null;
  }
  if (terminalInstance.resizeDisposable) {
    terminalInstance.resizeDisposable.dispose();
    terminalInstance.resizeDisposable = null;
  }

  if (terminalInstance.ws) {
    // Null out handlers BEFORE close to prevent buffered messages from old session
    // leaking into the terminal during session switch
    void LogFrontend('[term] detach requested by the app');
    terminalInstance.ws.onmessage = null;
    terminalInstance.ws.onclose = null;
    terminalInstance.ws.onerror = null;
    terminalInstance.ws.close();
    terminalInstance.ws = null;
    terminalInstance.sessionId = null;
  }
}

export function fitTerminal(terminalInstance: TerminalInstance): void {
  // Guard against fitting a detached/zero-sized container (would send bogus
  // 1×1 or similar resize to tmux, which then renders the pane that way).
  const el = (terminalInstance.terminal as any).element as HTMLElement | undefined;
  if (el) {
    const rect = el.getBoundingClientRect();
    if (rect.width < 2 || rect.height < 2) return;
  }

  terminalInstance.fitAddon.fit();

  // Send resize via WebSocket if connected
  if (terminalInstance.ws && terminalInstance.ws.readyState === WebSocket.OPEN) {
    const { cols, rows } = terminalInstance.terminal;
    // Need realistic terminal dimensions; anything below 2 is almost certainly
    // the result of measuring a hidden container.
    if (cols > 1 && rows > 1) {
      sendResize(terminalInstance.ws, cols, rows);
    }
  }
}
