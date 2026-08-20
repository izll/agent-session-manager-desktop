/** A component-owned draft that must explicitly approve destructive navigation. */
export type UnsavedGuard = {
  isDirty: () => boolean;
  requestDiscard: (continueAfterDiscard: () => void) => void;
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
export function afterUnsavedChanges(action: () => void): void {
  const pending = [...guards].filter((guard) => guard.isDirty());
  const next = (index: number) => {
    if (index >= pending.length) {
      action();
      return;
    }
    // It may have been saved while an earlier prompt was open.
    if (!pending[index].isDirty()) {
      next(index + 1);
      return;
    }
    pending[index].requestDiscard(() => next(index + 1));
  };
  next(0);
}
