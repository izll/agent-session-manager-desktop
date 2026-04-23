import {
  createTerminal,
  attachToSession,
  detachFromSession,
  fitTerminal,
  type TerminalInstance
} from './terminal';
import type { Terminal } from '@xterm/xterm';

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
      } else {
        entry.containerEl.style.display = 'none';
        entry.containerEl.style.zIndex = '0';
      }
      // Pair the DOM visibility with the xterm write gate — keeps hidden
      // tabs from spending CPU on off-screen canvas renders.
      const ti = entry.terminalInstance;
      const wasVisible = ti.visible;
      ti.visible = isActive;
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
    this.parentEl.appendChild(containerEl);

    // Create xterm instance
    const terminalInstance = createTerminal(containerEl, this.terminalOptions);
    // Start hidden — applyVisibility() will flip this when show() runs.
    terminalInstance.visible = false;

    entry = { terminalInstance, containerEl, key };
    this.entries.set(key, entry);

    // Attach WebSocket
    await attachToSession(terminalInstance, sessionId, windowIdx);

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

    // Apply visibility with the active key
    this.applyVisibility();

    // Fit + refresh after display. We need two rAFs: the first lets the browser
    // compute layout for the newly-visible container (display:none→block), the
    // second runs once the dimensions are real so fitAddon measures correctly.
    requestAnimationFrame(() => {
      if (this.showGeneration !== gen) return;
      requestAnimationFrame(() => {
        if (this.showGeneration !== gen) return;
        // Bail out if the container somehow still has no size; we'll retry
        // on the next ResizeObserver callback.
        const rect = entry.containerEl.getBoundingClientRect();
        if (rect.width < 2 || rect.height < 2) return;

        fitTerminal(entry.terminalInstance);
        const term = entry.terminalInstance.terminal;
        term.refresh(0, term.rows - 1);
        term.focus();
      });
    });
  }

  async destroy(sessionId: string): Promise<void> {
    const keysToDelete: string[] = [];
    for (const [key, entry] of this.entries) {
      if (key.startsWith(sessionId + ':')) {
        keysToDelete.push(key);
        await detachFromSession(entry.terminalInstance);
        entry.terminalInstance.cleanup();
        entry.containerEl.remove();
        if (this.activeKey === key) {
          this.activeKey = null;
        }
      }
    }
    for (const key of keysToDelete) {
      this.entries.delete(key);
    }
  }

  async destroyAll(): Promise<void> {
    for (const entry of this.entries.values()) {
      await detachFromSession(entry.terminalInstance);
      entry.terminalInstance.cleanup();
      entry.containerEl.remove();
    }
    this.entries.clear();
    this.activeKey = null;
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
