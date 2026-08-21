<script lang="ts">
  import { onMount, tick } from 'svelte';
  import TaskPanel from '../../src/lib/components/MainPanel/TaskPanel.svelte';

  export let onFixtureReady: () => void = () => {};

  onMount(() => {
    // A cold parallel Vite run can spend several seconds transforming the
    // TaskPanel graph. Signal only from the real Svelte lifecycle, after the
    // child has mounted, its immediate async task load has flushed, and the
    // browser has reached a paint frame.
    void tick().then(() => {
      requestAnimationFrame(onFixtureReady);
    });
  });
</script>

<TaskPanel active={true} />
