<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { activeProjectId, selectProject } from '../../src/lib/stores/projects';
  import { saveSettings, settings } from '../../src/lib/stores/settings';
  import { loadTranslations, locale, t } from '../../src/lib/i18n';

  export let onFixtureReady: () => void = () => {};

  onMount(async () => {
    activeProjectId.set('project-a');
    settings.update((value) => ({ ...value, uiTheme: 'old-project-theme' }));
    await loadTranslations('en');
    await tick();
    requestAnimationFrame(onFixtureReady);
  });
</script>

<button id="switch-language-project" on:click={() => void selectProject('project-b')}>switch language project</button>
<button id="switch-failing-settings-project" on:click={() => void selectProject('project-c')}>switch failing settings project</button>
<button id="save-after-settings-failure" on:click={() => void saveSettings({ compactList: true })}>save after settings failure</button>

<span id="settings-language">{$settings.language}</span>
<span id="settings-theme">{$settings.uiTheme}</span>
<span id="runtime-locale">{$locale}</span>
<span id="translated-save">{$t('common.save')}</span>
