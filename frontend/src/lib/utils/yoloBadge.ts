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
 * It shows what a click changes. On a running Claude tab a click cycles the
 * pane's own mode, so the button follows the pane. Anywhere else a click
 * toggles the stored setting (and restarts), so the button follows that
 * setting. Following Codex's live reading instead made a tab whose YOLO the
 * background server ignored look off — and clicking it to switch YOLO on
 * switched it off, for the whole session.
 */
export function yoloButtonState(input: {
  running: boolean;
  tabAgent: string;
  sessionAutoYes: boolean;
  tabAutoYes: boolean;
  tab: { yolo?: boolean; yoloNotInEffect?: boolean } | undefined | null;
}): { active: boolean; notInEffect: boolean } {
  if (!input.running) return { active: input.sessionAutoYes, notInEffect: false };
  if (input.tabAgent === 'claude') return { active: !!input.tab?.yolo, notInEffect: false };
  return {
    active: input.sessionAutoYes || input.tabAutoYes || !!input.tab?.yolo,
    notInEffect: !!input.tab?.yoloNotInEffect,
  };
}

export function yoloBadge(tab: { yolo?: boolean; yoloNotInEffect?: boolean } | undefined | null): YoloBadge {
  if (tab?.yolo) return 'on';
  if (tab?.yoloNotInEffect) return 'notInEffect';
  return '';
}
