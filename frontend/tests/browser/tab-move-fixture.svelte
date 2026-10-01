<script lang="ts">
  import { onMount, tick } from 'svelte';
  import SessionTree from '../../src/lib/components/Sidebar/SessionTree.svelte';
  import TabBar from '../../src/lib/components/MainPanel/TabBar.svelte';
  import Notes from '../../src/lib/components/MainPanel/Notes.svelte';

  export let onFixtureReady: () => void = () => {};

  onMount(async () => {
    await tick();
    requestAnimationFrame(() => requestAnimationFrame(onFixtureReady));
  });
</script>

<!-- The sidebar and the tab bar side by side, as in the app: a tab is dragged
     from one onto a session row in the other. -->
<div style="display: flex; height: 100vh">
  <div style="width: 280px; height: 600px; display: flex; flex-direction: column">
    <SessionTree onNewSession={() => {}} onNewGroup={() => {}} onCollapse={() => {}} />
  </div>
  <div style="flex: 1; min-width: 0">
    <TabBar visible={true} />
    <div style="height: 300px"><Notes active={true} /></div>
  </div>
</div>
