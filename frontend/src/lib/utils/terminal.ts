import { Terminal, type IDisposable } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';

export interface TerminalInstance {
  terminal: Terminal;
  fitAddon: FitAddon;
  sessionId: string | null;
  windowIdx: number;
  ws: WebSocket | null;
  cleanup: () => void;
  dataDisposable: IDisposable | null;
  resizeDisposable: IDisposable | null;
}

export function createTerminal(container: HTMLElement, options: Partial<Terminal['options']> = {}): TerminalInstance {
  const terminal = new Terminal({
    cursorBlink: true,
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

  // WebGL renderer disabled - causes lag in WebKit webview
  // Using canvas renderer instead (default)

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

    return true;
  });

  fitAddon.fit();

  return {
    terminal,
    fitAddon,
    sessionId: null,
    windowIdx: 0,
    ws: null,
    dataDisposable: null,
    resizeDisposable: null,
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
    // WebSocket port (hardcoded for now, matches terminal_ws.go)
    const port = 9753;
    const wsUrl = `ws://127.0.0.1:${port}/terminal?session=${sessionId}&window=${windowIdx}`;

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

    // Receive data from WebSocket
    ws.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) {
        const data = new Uint8Array(event.data);
        terminal.write(data);
      } else {
        terminal.write(event.data);
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

    // Handle resize
    terminalInstance.resizeDisposable = terminal.onResize(({ cols, rows }) => {
      sendResize(ws, cols, rows);
    });

    // Initial resize
    const { cols, rows } = terminal;
    sendResize(ws, cols, rows);

    // Clear and refocus
    terminal.clear();
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
    terminalInstance.ws.close();
    terminalInstance.ws = null;
    terminalInstance.sessionId = null;
  }
}

export function fitTerminal(terminalInstance: TerminalInstance): void {
  terminalInstance.fitAddon.fit();

  // Send resize via WebSocket if connected
  if (terminalInstance.ws && terminalInstance.ws.readyState === WebSocket.OPEN) {
    const { cols, rows } = terminalInstance.terminal;
    sendResize(terminalInstance.ws, cols, rows);
  }
}
