/**
 * Which YOLO badge a tab shows.
 *
 * 'on': the agent reports YOLO in effect. 'notInEffect': YOLO was asked for,
 * but the agent reports it is not in effect — Codex's background server
 * ignores the flag — shown as a crossed-out badge, since no badge at all would
 * read as "not asked for". '': nothing to show.
 */
export type YoloBadge = 'on' | 'notInEffect' | '';

/**
 * The YOLO button above the terminal: whether it shows as on, and whether to
 * warn that YOLO is not in effect.
 *
 * It shows what a click changes. On a running Claude window a click cycles the
 * pane's own mode, so the button follows the pane. Anywhere else a click
 * toggles the stored setting (and restarts), so the button follows that
 * setting. Following Codex's live reading instead made a tab whose YOLO the
 * background server ignored look off — and clicking it to switch YOLO on
 * switched it off, for the whole session.
 *
 * The stored setting of a tab is its own flag OR the session's — a tab starts
 * with YOLO when either is set — and a click on a tab turns off whichever is
 * set, or turns on the tab's own (CycleYoloMode). The main window has only the
 * session's flag: pass tabAutoYes false for it. A stopped tab has no pane to
 * cycle, so even a Claude one follows its stored setting.
 */
export function yoloButtonState(input: {
  running: boolean;
  tabAgent: string;
  sessionAutoYes: boolean;
  tabAutoYes: boolean;
  tabStopped?: boolean;
  tab: { yolo?: boolean; yoloNotInEffect?: boolean } | undefined | null;
}): { active: boolean; notInEffect: boolean } {
  const stored = input.sessionAutoYes || input.tabAutoYes;
  if (!input.running || input.tabStopped) return { active: stored, notInEffect: false };
  if (input.tabAgent === 'claude') return { active: !!input.tab?.yolo, notInEffect: false };
  return { active: stored, notInEffect: !!input.tab?.yoloNotInEffect };
}

export function yoloBadge(tab: { yolo?: boolean; yoloNotInEffect?: boolean } | undefined | null): YoloBadge {
  if (tab?.yolo) return 'on';
  if (tab?.yoloNotInEffect) return 'notInEffect';
  return '';
}
