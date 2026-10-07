<script lang="ts">
  /**
   * One tab's view bar order, from the tab's menu.
   *
   * Changes apply as they are made, so the bar above shows the result at
   * once; "use the global order" drops the tab's own and follows Settings
   * again.
   */
  import { claimKeyForDialog } from '../../utils/dialogKeys';
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { portal } from '../../utils/portal';
  import { get } from 'svelte/store';
  import * as App from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';
  import { sessions, loadSessions } from '../../stores/sessions';
  import { settings } from '../../stores/settings';
  import { activeProjectId } from '../../stores/projects';
  import { normalizeViewOrder, resolveViewOrder, type ViewId } from '../../utils/viewOrder';
  import DialogCloseButton from '../common/DialogCloseButton.svelte';
  import ViewOrderEditor from '../common/ViewOrderEditor.svelte';

  export let show = false;
  export let sessionId = '';
  export let windowIdx = 0;
  /** The tab's name, for the title. */
  export let tabName = '';

  let error = '';
  let saving = false;

  $: session = $sessions.find((s) => s.id === sessionId) ?? null;
  $: ownOrder = (() => {
    if (!session) return null;
    if (windowIdx === (session.mainWindowIndex ?? 0)) return session.viewOrder ?? null;
    const fw = (session.followedWindows || []).find((f: any) => f.index === windowIdx);
    return (fw?.view_order as string[] | undefined) ?? null;
  })();
  $: hasOwn = !!ownOrder?.length;
  $: shown = resolveViewOrder(ownOrder, $settings.viewOrder);

  async function store(order: ViewId[] | null) {
    if (saving) return;
    saving = true;
    error = '';
    try {
      await App.SetTabViewOrder(sessionId, windowIdx, order ?? [], get(activeProjectId));
      await loadSessions();
    } catch (e) {
      error = String(e);
    } finally {
      saving = false;
    }
  }

  function close() {
    show = false;
    error = '';
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      claimKeyForDialog();
      e.stopPropagation();
      close();
    }
  }
</script>

{#if show}
  <div
    class="dialog-overlay"
    use:portal
    use:autoFocusDialog
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
    tabindex="-1"
    data-dialog="tab-view-order"
  >
    <div class="dialog-content view-order-dialog">
      <div class="dialog-header">
        <h2>{$t('viewOrder.tabTitle', { name: tabName })}</h2>
        <DialogCloseButton on:click={close} />
      </div>
      <div class="dialog-body">
        <p class="source" data-source={hasOwn ? 'tab' : 'global'}>
          {hasOwn ? $t('viewOrder.usesOwn') : $t('viewOrder.usesGlobal')}
        </p>
        <ViewOrderEditor order={shown} on:change={(e) => store(normalizeViewOrder(e.detail))} />
        {#if error}<div class="error-line">{error}</div>{/if}
      </div>
      <div class="dialog-footer">
        <button
          class="btn-secondary"
          data-action="use-global"
          disabled={!hasOwn || saving}
          on:click={() => store(null)}
        >{$t('viewOrder.useGlobal')}</button>
        <button class="btn-primary" on:click={close}>{$t('common.close')}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .view-order-dialog {
    width: min(420px, 94vw);
    max-width: min(420px, 94vw);
  }
  .source {
    margin: 0 0 12px;
    font-size: 12px;
    color: #9ca3af;
  }
  .source[data-source='tab'] { color: rgb(var(--accent-rgb)); }
  .error-line { margin-top: 10px; font-size: 12px; color: #f87171; }
</style>
