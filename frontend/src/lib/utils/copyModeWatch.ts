/**
 * Watches whether the tab a terminal shows is in tmux copy mode. In it tmux shows a
 * still picture, so a working agent looks frozen — and the app hides tmux's own
 * indicator. Shown again, a tab is settled once (copy mode left at the bottom ends);
 * after that it is read on every tick, to say so while it lasts.
 */

export type PaneScroll = { inMode: boolean; scrolled: boolean };

export type CopyModeApi = {
  read(sessionId: string, windowIdx: number): Promise<PaneScroll>;
  settle(sessionId: string, windowIdx: number): Promise<PaneScroll>;
};

export type Schedule = {
  every(fn: () => void, ms: number): unknown;
  stop(handle: unknown): void;
};

const NONE: PaneScroll = { inMode: false, scrolled: false };

const browserSchedule: Schedule = {
  every: (fn, ms) => setInterval(fn, ms),
  stop: (handle) => clearInterval(handle as ReturnType<typeof setInterval>),
};

export function createCopyModeWatch(
  api: CopyModeApi,
  onChange: (state: PaneScroll) => void,
  schedule: Schedule = browserSchedule,
  intervalMs = 1000,
) {
  let shown: { sessionId: string; windowIdx: number } | null = null;
  // Bumped on every change of tab, so an answer about the one before is dropped.
  let generation = 0;
  let timer: unknown = null;
  let asking = false;

  function ask(call: Promise<PaneScroll>) {
    const asked = generation;
    asking = true;
    call
      .then((state) => {
        if (asked === generation) onChange({ inMode: !!state?.inMode, scrolled: !!state?.scrolled });
      })
      .catch(() => {
        // Not knowing is no news: the badge stays as it was.
      })
      .finally(() => {
        if (asked === generation) asking = false;
      });
  }

  function stopTimer() {
    if (timer !== null) schedule.stop(timer);
    timer = null;
  }

  return {
    show(sessionId: string, windowIdx: number) {
      if (shown && shown.sessionId === sessionId && shown.windowIdx === windowIdx) return;
      generation += 1;
      asking = false;
      shown = { sessionId, windowIdx };
      stopTimer();
      onChange(NONE);
      ask(api.settle(sessionId, windowIdx));
      timer = schedule.every(() => {
        if (shown && !asking) ask(api.read(shown.sessionId, shown.windowIdx));
      }, intervalMs);
    },
    hide() {
      generation += 1;
      asking = false;
      stopTimer();
      if (shown) onChange(NONE);
      shown = null;
    },
  };
}
