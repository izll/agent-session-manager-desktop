<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import Toast from '../../src/lib/components/common/Toast.svelte';
  import UndoToast from '../../src/lib/components/common/UndoToast.svelte';
  import { dismissUndo, offerUndo } from '../../src/lib/stores/undo';

  export let onFixtureReady: () => void = () => {};

  let showToast = false;
  let toastMessage = '';
  let toastRevision = 0;
  const undoResolvers: Array<() => void> = [];
  let undoCalls = 0;

  function notify(message: string) {
    toastMessage = message;
    toastRevision++;
    showToast = true;
  }

  function offer(message: string) {
    offerUndo({
      message,
      undo: () => new Promise<void>((resolve) => {
        undoCalls++;
        undoResolvers.push(resolve);
      }),
    });
  }

  onMount(async () => {
    await tick();
    requestAnimationFrame(onFixtureReady);
  });

  onDestroy(() => {
    dismissUndo();
    for (const resolve of undoResolvers) resolve();
  });
</script>

<button id="toast-first" on:click={() => notify('First notification')}>first notification</button>
<button id="toast-second" on:click={() => notify('Second notification')}>second notification</button>
<button id="undo-first" on:click={() => offer('Undo first action')}>offer first undo</button>
<button id="undo-second" on:click={() => offer('Undo second action')}>offer second undo</button>

<Toast bind:show={showToast} message={toastMessage} revision={toastRevision} variant="error" duration={800} />
<UndoToast />

<svelte:window
  on:feedback-resolve-undos={() => {
    for (const resolve of undoResolvers.splice(0)) resolve();
  }}
/>

<span id="undo-calls">{undoCalls}</span>
