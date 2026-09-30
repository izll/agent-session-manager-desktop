<script lang="ts">
  // The app's pieces around the project list, as App.svelte arranges them:
  // the header button, the window it opens, and the all-tasks view — so a task
  // can be followed from one to the other without mounting the whole app.
  import { onMount, tick } from 'svelte';
  import ProjectTasksButton from '../../src/lib/components/common/ProjectTasksButton.svelte';
  import ProjectTasksDialog from '../../src/lib/components/Dialogs/ProjectTasksDialog.svelte';
  import AllTasks from '../../src/lib/components/Dashboard/AllTasks.svelte';
  import { projectTasksOpen, watchProjectOpenCount } from '../../src/lib/stores/projectTasks';

  export let onFixtureReady: () => void = () => {};

  onMount(() => {
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
<ProjectTasksDialog bind:show={$projectTasksOpen} />

<style>
  .fixture-header { display: flex; justify-content: flex-end; padding: 8px; }
  .fixture-all-tasks { height: 700px; }
</style>
