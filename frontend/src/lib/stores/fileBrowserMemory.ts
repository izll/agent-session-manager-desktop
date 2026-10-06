// What the Files view remembers per tab, kept outside the component.
//
// The full diff replaces the area the Files view lives in, so switching to it
// tears the view down and switching back builds a new one. Held inside, all of
// this went with it: the file that was open, where the caret was in it, and
// which folders were open — and coming back from the diff showed an empty pane.
//
// In memory only: it records where you are in this sitting, not a preference
// worth persisting. Keys carry the project, session and tab (`browseKey`).

/** The file that was open in each tab. */
export const lastFileByTab = new Map<string, string>();

/** The folders that were open in each tab's tree. */
export const expandedByTab = new Map<string, string[]>();

/**
 * Where the user was in each file, keyed by tab and path. Per file rather than
 * per tab: coming back lands where you were, but picking a different file
 * starts at its top — a file never opened has no entry here.
 */
export const placeByFile = new Map<string, number>();
