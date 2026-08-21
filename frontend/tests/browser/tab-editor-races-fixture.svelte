<script lang="ts">
  import { onMount, tick } from 'svelte';
  import TabBar from '../../src/lib/components/MainPanel/TabBar.svelte';

  export let onFixtureReady: () => void = () => {};

  onMount(() => {
    void tick().then(() => requestAnimationFrame(onFixtureReady));
  });
</script>

<TabBar visible={true} />
