<script lang="ts">
  import { t } from '../../i18n';
  import { moveOnlyTabWithNotice, tabMovePrompt, type TabMovePrompt } from '../../utils/tabMove';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import MoveToNewSessionDialog from './MoveToNewSessionDialog.svelte';

  /**
   * The questions a tab move asks before it happens (see tabMovePrompt): the
   * name of the session a tab becomes, and whether to end a session by moving
   * its only tab. Kept in one place, since the move can come from a drop on a
   * sidebar row, on the "new session" target, or from the tab's menu.
   */

  let showName = false;
  let showConfirm = false;
  let prompt: TabMovePrompt | null = null;

  $: open($tabMovePrompt);

  function open(next: TabMovePrompt | null) {
    if (!next || next === prompt) return;
    prompt = next;
    showName = next.kind === 'newSession';
    showConfirm = next.kind === 'onlyTab';
  }

  function done() {
    prompt = null;
    showName = false;
    showConfirm = false;
    tabMovePrompt.set(null);
  }

  function confirmOnlyTab() {
    const asked = prompt;
    done();
    if (asked?.kind === 'onlyTab') void moveOnlyTabWithNotice(asked.sessionId, asked.targetId, asked.tabName);
  }

  $: newSession = prompt?.kind === 'newSession' ? prompt : null;
  $: onlyTab = prompt?.kind === 'onlyTab' ? prompt : null;
</script>

<MoveToNewSessionDialog
  bind:show={showName}
  sourceId={newSession?.sessionId ?? ''}
  windowIdx={newSession?.windowIdx ?? 0}
  tabName={newSession?.tabName ?? ''}
  on:close={done}
/>

<ConfirmDialog
  bind:show={showConfirm}
  title={$t('tabMove.onlyTabTitle')}
  message={$t('tabMove.onlyTabMessage', { name: onlyTab?.sessionName ?? '' })}
  confirmText={$t('tabMove.move')}
  cancelText={$t('common.cancel')}
  variant="warning"
  on:confirm={confirmOnlyTab}
  on:cancel={done}
/>
