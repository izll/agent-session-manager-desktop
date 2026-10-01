<script lang="ts">
  // The app's pieces around the project list, as App.svelte arranges them:
  // the header button, the window it opens, and the all-tasks view — so a task
  // can be followed from one to the other without mounting the whole app.
  import { onMount, tick } from 'svelte';
  import ProjectTasksButton from '../../src/lib/components/common/ProjectTasksButton.svelte';
  import ProjectTasksDialog from '../../src/lib/components/Dialogs/ProjectTasksDialog.svelte';
  import AllTasks from '../../src/lib/components/Dashboard/AllTasks.svelte';
  import ProjectWorkspace from '../../src/lib/components/Dashboard/ProjectWorkspace.svelte';
  import GlobalSearchDialog from '../../src/lib/components/Dialogs/GlobalSearchDialog.svelte';
  import { projectTasksOpen, watchProjectOpenCount } from '../../src/lib/stores/projectTasks';

  export let onFixtureReady: () => void = () => {};

  // ?dashboard adds the dashboard's copy of the project workspace, which is
  // on screen behind the project window in the app.
  const withDashboard = new URLSearchParams(location.search).has('dashboard');
  let showSearch = false;

  onMount(() => {
    (window as any).openGlobalSearch = () => { showSearch = true; };
    const stop = watchProjectOpenCount();
    void tick().then(() => requestAnimationFrame(onFixtureReady));
    return stop;
  });
</script>

<header class="fixture-header">
  <ProjectTasksButton />
</header>
<section class="fixture-all-tasks">
  <AllTasks />
</section>
{#if withDashboard}
  <section class="fixture-dashboard">
    <ProjectWorkspace />
  </section>
{/if}
<ProjectTasksDialog bind:show={$projectTasksOpen} />
<GlobalSearchDialog bind:show={showSearch} />

<style>
  .fixture-header { display: flex; justify-content: flex-end; padding: 8px; }
  .fixture-all-tasks { height: 700px; }
  .fixture-dashboard { height: 400px; }
</style>
