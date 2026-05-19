import { Terminal, type IDisposable } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { GetTerminalWSPort, GetTerminalWSToken } from '../../../wailsjs/go/main/App';

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

export function createTerminal(container: HTMLElement, options: Partial<Terminal['options']> = {}): TerminalInstance {
  const terminal = new Terminal({
    // cursorBlink triggers a continuous render tick on the xterm canvas every
    // ~500ms even when the terminal is idle — disabled to keep the WebKit
    // renderer quiet when nothing is happening.
    cursorBlink: false,
    fontSize: 14,
    scrollback: 1000,
    fontFamily: 'JetBrains Mono, Menlo, Monaco, Consolas, monospace',
    theme: {
      background: '#1a1a2e',
      foreground: '#eee',
      cursor: '#7d56f4',
      selection: 'rgba(125, 86, 244, 0.3)',
      black: '#000000',
      red: '#ff5555',
      green: '#50fa7b',
      yellow: '#f1fa8c',
      blue: '#bd93f9',
      magenta: '#ff79c6',
      cyan: '#8be9fd',
      white: '#f8f8f2',
    },
    ...options
  });

  const fitAddon = new FitAddon();
  terminal.loadAddon(fitAddon);

  terminal.open(container);

  // Using the default DOM renderer — the canvas addon produced blurry glyphs
  // on HiDPI displays, and the WebGL addon lags in WebKit. The real CPU work
  // is throttled on the backend side (see terminal_ws.go) and by suppressing
  // writes to hidden tabs (see terminalPool.ts).

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

    // Shift+Enter: send newline (\n) instead of carriage return (\r)
    // Most agents (Claude CLI, etc.) interpret \n as "new line" vs \r as "submit"
    if (event.shiftKey && event.key === 'Enter' && event.type === 'keydown') {
      (terminal as any)._core.coreService.triggerDataEvent('\n', true);
      return false;
    }

    return true;
  });

  // Auto-copy selection to clipboard when user selects text
  terminal.onSelectionChange(() => {
    const selection = terminal.getSelection();
    if (selection) {
      navigator.clipboard.writeText(selection).catch(() => {
        // Clipboard write may fail if not focused or no permission
      });
    }
  });

  return {
    terminal,
    fitAddon,
    sessionId: null,
    windowIdx: 0,
    ws: null,
    dataDisposable: null,
    resizeDisposable: null,
    visible: true,
    hiddenBuffer: [],
    cleanup: () => {
      terminal.dispose();
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

    // Timed batching for visible writes. Every WS chunk goes into a queue
    // and gets flushed at a capped rate (see FLUSH_INTERVAL_MS). Using a
    // plain setTimeout rather than requestAnimationFrame on purpose:
    // - rAF runs at 60Hz and is still aggressive enough to keep WebKit's
    //   main thread pegged when an agent paints continuously.
    // - Terminals don't need 60fps; 20-25fps feels instant to humans and
    //   halves the DOM-mutation storm that the xterm DOM renderer causes.
    const FLUSH_INTERVAL_MS = 40; // ~25fps
    let visibleQueue: Uint8Array[] = [];
    let timerHandle: ReturnType<typeof setTimeout> | null = null;
    const flushVisible = () => {
      timerHandle = null;
      if (visibleQueue.length === 0) return;
      // Concat and write in one call so xterm only runs one parse/layout cycle.
      let total = 0;
      for (const c of visibleQueue) total += c.byteLength;
      const merged = new Uint8Array(total);
      let offset = 0;
      for (const c of visibleQueue) {
        merged.set(c, offset);
        offset += c.byteLength;
      }
      visibleQueue = [];
      terminal.write(merged);
    };

    ws.onmessage = (event) => {
      const chunk = event.data instanceof ArrayBuffer
        ? new Uint8Array(event.data)
        : new TextEncoder().encode(event.data as string);

      if (terminalInstance.visible) {
        visibleQueue.push(chunk);
        if (timerHandle === null) {
          timerHandle = setTimeout(flushVisible, FLUSH_INTERVAL_MS);
        }
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
      if (timerHandle === null) {
        timerHandle = setTimeout(flushVisible, FLUSH_INTERVAL_MS);
      }
    };

    ws.onclose = () => {
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

    // Focus terminal
    terminal.focus();

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
