<script lang="ts">
  /**
   * Says that a Codex conversation was continued through Codex's background
   * server, and offers to stop that server.
   *
   * The server still held the conversation, so starting it without the server
   * would only have shown Codex's "open in another app" screen. Through the
   * server it opens, but the server ignores the YOLO flag — which is worth
   * saying, since the session asked for YOLO.
   *
   * Once the server is stopped it offers to restart the tab, which then opens
   * the conversation without the server, with YOLO in effect. One step at a
   * time: stop, then restart.
   */
  import { onDestroy, onMount } from 'svelte';
  import Toast from './Toast.svelte';
  import { EventsOff, EventsOn } from '../../../../wailsjs/runtime/runtime';
  import { StopCodexDaemon } from '../../../../wailsjs/go/main/App';
  import { t } from '../../i18n';
  import { sessions, restartTab } from '../../stores/sessions';
  import { activeProjectId } from '../../stores/projects';
  import {
    codexDaemonNoticeText,
    codexDaemonNoticeWindow,
    type CodexDaemonHeldNotice,
    type CodexDaemonNoticeState,
  } from './codexDaemonNotice';

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

  function act() {
    if (state === 'held') void stopDaemon();
    else if (state === 'stopped') void restartHeldTab();
  }

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

  // The tab the conversation runs in, while it can still be restarted safely.
  $: restartWindow = codexDaemonNoticeWindow(notice, $sessions, $activeProjectId);

  async function restartHeldTab() {
    if (!notice || busy || restartWindow === null) return;
    const restarting = notice;
    busy = true;
    try {
      await restartTab(restarting.sessionId, restartWindow);
      // A server still holding the conversation sends a fresh notice from
      // this very restart; that one is what the user needs to see.
      if (notice === restarting) state = 'restarted';
    } catch (e) {
      if (notice === restarting) {
        state = 'restartFailed';
        failure = String(e);
      }
    } finally {
      busy = false;
      revision++;
    }
  }

  $: text = codexDaemonNoticeText($t, state, notice, failure, restartWindow !== null);
</script>

<Toast
  bind:show
  message={text.message}
  variant={text.variant}
  actionLabel={text.action}
  actionBusy={busy}
  on:action={act}
  {revision}
  duration={text.action ? 0 : 9000}
/>
