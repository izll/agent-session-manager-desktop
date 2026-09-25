/**
 * Which YOLO badge a tab shows.
 *
 * 'on': the agent reports YOLO in effect. 'notInEffect': YOLO was asked for,
 * but the agent reports it is not in effect — Codex's background server
 * ignores the flag — shown as a crossed-out badge, since no badge at all would
 * read as "not asked for". '': nothing to show.
 */
export type YoloBadge = 'on' | 'notInEffect' | '';

export function yoloBadge(tab: { yolo?: boolean; yoloNotInEffect?: boolean } | undefined | null): YoloBadge {
  if (tab?.yolo) return 'on';
  if (tab?.yoloNotInEffect) return 'notInEffect';
  return '';
}
