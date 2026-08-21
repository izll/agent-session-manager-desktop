/** A component-owned draft that must explicitly approve destructive navigation. */
export type UnsavedGuard = {
  isDirty: () => boolean;
  requestDiscard: (continueAfterDiscard: () => void, cancelDiscard?: () => void) => void;
};

const guards = new Set<UnsavedGuard>();

export function registerUnsavedGuard(guard: UnsavedGuard): () => void {
  guards.add(guard);
  return () => guards.delete(guard);
}

/**
 * Continue only after every currently dirty component has confirmed discard.
 * Guards are component-owned because only the editor can present the right
 * recovery choice and clear its buffer without leaving stale UI behind.
 */
export function afterUnsavedChanges(action: () => void, onCancel: () => void = () => {}): void {
  const approved = new Set<UnsavedGuard>();
  const next = () => {
    // Re-scan the live registry after every approval. A second editor may
    // become dirty while an earlier confirmation is open.
    const guard = [...guards].find((candidate) => !approved.has(candidate) && candidate.isDirty());
    if (!guard) {
      action();
      return;
    }
    guard.requestDiscard(() => {
      // Editors may remain dirty until their modal is torn down. Remembering
      // the approval avoids prompting the same guard forever in that case.
      approved.add(guard);
      next();
    }, onCancel);
  };
  next();
}
