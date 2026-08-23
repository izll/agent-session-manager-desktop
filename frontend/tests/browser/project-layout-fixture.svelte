<script lang="ts">
  import { onMount, tick } from 'svelte';
  import MainPanel from '../../src/lib/components/MainPanel/MainPanel.svelte';

  export let onFixtureReady: () => void = () => {};

  onMount(async () => {
    await tick();
    requestAnimationFrame(() => requestAnimationFrame(onFixtureReady));
  });
</script>

<MainPanel visible={true} terminalFocusAllowed={false} />
