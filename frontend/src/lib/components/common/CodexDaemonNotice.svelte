<script lang="ts">
  /**
   * Says that a Codex conversation was continued through Codex's background
   * server, and offers to stop that server.
   *
   * The server still held the conversation, so starting it without the server
   * would only have shown Codex's "open in another app" screen. Through the
   * server it opens, but the server ignores the YOLO flag — which is worth
   * saying, since the session asked for YOLO.
   */
  import { onDestroy, onMount } from 'svelte';
  import Toast from './Toast.svelte';
  import { EventsOff, EventsOn } from '../../../../wailsjs/runtime/runtime';
  import { StopCodexDaemon } from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';
  import { codexDaemonNoticeText, type CodexDaemonHeldNotice, type CodexDaemonNoticeState } from './codexDaemonNotice';

  const EVENT = 'codex:daemonHeld';

  let notice: CodexDaemonHeldNotice | null = null;
  let state: CodexDaemonNoticeState = 'held';
  let failure = '';
  let show = false;
  let revision = 0;
  let busy = false;

  onMount(() => {
    EventsOn(EVENT, (next: CodexDaemonHeldNotice) => {
      notice = next;
      state = 'held';
      failure = '';
      busy = false;
      revision++;
      show = true;
    });
  });
  onDestroy(() => EventsOff(EVENT));

  async function stopDaemon() {
    if (!notice || busy) return;
    busy = true;
    try {
      await StopCodexDaemon(notice.sessionId, notice.serverId);
      state = 'stopped';
    } catch (e) {
      state = 'failed';
      failure = String(e);
    } finally {
      busy = false;
      revision++;
    }
  }

  $: text = codexDaemonNoticeText($t, state, notice, failure);
</script>

<Toast
  bind:show
  message={text.message}
  variant={text.variant}
  actionLabel={text.action}
  actionBusy={busy}
  on:action={stopDaemon}
  {revision}
  duration={state === 'held' ? 0 : 9000}
/>
