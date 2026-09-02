<script lang="ts">
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { loadActivities } from '../../stores/activities';
  import { setDictationHotkey } from '../../utils/dictationHotkey';
  import { createEventDispatcher, onDestroy } from 'svelte';
  import { get } from 'svelte/store';
  import { settings, saveSettings } from '../../stores/settings';
  import { activeProjectId } from '../../stores/projects';
  import * as DictationService from '../../../../wailsjs/go/main/DictationService';
  import * as App from '../../../../wailsjs/go/main/App';
  import type { main } from '../../../../wailsjs/go/models';
  import { EventsEmit } from '../../../../wailsjs/runtime/runtime';
  import Select from '../common/Select.svelte';
  import { TERMINAL_THEMES, CUSTOM_THEME_KEYS, getTerminalTheme, allPalettes, nextCustomId,
           MIN_FONT_SIZE, MAX_FONT_SIZE, DEFAULT_FONT_SIZE, type CustomPalette } from '../../utils/terminalThemes';
  import PalettePicker from '../common/PalettePicker.svelte';
  import PaletteManager from '../common/PaletteManager.svelte';
  import { UI_THEMES, DEFAULT_UI_THEME, CUSTOM_UI_THEME,
           accentContrastOnBackground, MIN_ACCENT_CONTRAST } from '../../utils/uiThemes';
  import { agents } from '../../stores/agents';
  import ShortcutEditor from '../Settings/ShortcutEditor.svelte';
  import { t, loadTranslations } from '../../i18n';

  export let show = false;

  const languageOptions = [
    { value: 'en', label: 'English' },
    { value: 'hu', label: 'Magyar' },
    { value: 'de', label: 'Deutsch' },
    { value: 'es', label: 'Español' },
    { value: 'fr', label: 'Français' },
    { value: 'pt-br', label: 'Português (Brasil)' },
    { value: 'it', label: 'Italiano' },
    { value: 'ru', label: 'Русский' },
    { value: 'zh-cn', label: '简体中文' },
    { value: 'ja', label: '日本語' },
    { value: 'ko', label: '한국어' },
    { value: 'tr', label: 'Türkçe' },
    { value: 'pl', label: 'Polski' },
    { value: 'nl', label: 'Nederlands' },
    { value: 'cs', label: 'Čeština' },
    { value: 'uk', label: 'Українська' },
    { value: 'ar', label: 'العربية' },
    { value: 'th', label: 'ภาษาไทย' },
    { value: 'vi', label: 'Tiếng Việt' },
    { value: 'sv', label: 'Svenska' },
  ];

  let languageChangeGeneration = 0;
  async function changeLanguage(lang: string) {
    const projectId = get(activeProjectId);
    const generation = ++languageChangeGeneration;
    await loadTranslations(lang);
    // A locale chunk may finish after the dialog was closed and another
    // project selected. Never persist A's choice into B's settings.
    if (generation !== languageChangeGeneration || projectId !== get(activeProjectId)) return;
    void saveSettings({ language: lang }, projectId);
  }

  // --- Terminal palettes -------------------------------------------------
  $: customPalettes = ((($settings as any).customTerminalThemes || []) as CustomPalette[]);
  $: pickablePalettes = allPalettes(customPalettes);

  // 0 means "unset" in storage, so the slider shows the effective default.
  $: currentFontSize = $settings.terminalFontSize || DEFAULT_FONT_SIZE;

  function changeFontSize(size: number) {
    saveSettings({ terminalFontSize: size });
  }

  $: currentAgentFontSize = $settings.agentFontSize || DEFAULT_FONT_SIZE;

  function changeAgentFontSize(size: number) {
    saveSettings({ agentFontSize: size });
  }

  function changeTheme(id: string) {
    saveSettings({ terminalTheme: id });
  }

  // The agent side has its own default, independent of the terminal one.
  $: agentDefaultTheme = (($settings as any).agentDefaultTheme || 'asmgr') as string;
  function changeAgentDefault(id: string) {
    saveSettings({ agentDefaultTheme: id } as any);
  }

  $: agentThemeMap = (($settings as any).agentTerminalThemes || {}) as Record<string, string>;
  function agentThemeValue(type: string): string {
    return agentThemeMap[type] || '';
  }
  function changeAgentTheme(agentType: string, id: string) {
    const map = { ...agentThemeMap };
    if (id) map[agentType] = id; else delete map[agentType];
    saveSettings({ agentTerminalThemes: map } as any);
  }


  const rendererOptions = [
    { value: 'canvas', label: 'Canvas (ajánlott)' },
    { value: 'webgl', label: 'WebGL (leggyorsabb, kísérleti)' },
    { value: 'dom', label: 'DOM (legkompatibilisebb)' },
  ];

  function changeRenderer(r: string) {
    saveSettings({ terminalRenderer: r as 'canvas' | 'webgl' | 'dom' });
  }

  // Stacks that exist without installing anything, plus the app's own default.
  // Named families are quoted where they contain a space; the terminal appends
  // a generic if the user's own entry lacks one.
  $: fontOptions = [
    { value: '', label: $t('settings.fontDefault') },
    { value: 'Menlo, monospace', label: 'Menlo' },
    { value: '"DejaVu Sans Mono", monospace', label: 'DejaVu Sans Mono' },
    { value: '"Liberation Mono", monospace', label: 'Liberation Mono' },
    { value: 'Consolas, monospace', label: 'Consolas' },
    { value: '"Courier New", monospace', label: 'Courier New' },
    { value: 'monospace', label: $t('settings.fontSystemMono') },
  ];

  function changeFontFamily(v: string) {
    saveSettings({ terminalFontFamily: v });
  }

  $: copyModeOptions = [
    { value: 'shift', label: $t('settings.copyModeShift') },
    { value: 'select', label: $t('settings.copyModeSelect') },
  ];

  function changeCopyMode(v: string) {
    saveSettings({ terminalCopyMode: v as 'shift' | 'select' });
  }

  // The backend supplies the list, because which shells make sense is a
  // property of the platform and the frontend cannot tell which it is on.
  // A single entry means there is nothing to choose between — Unix, where
  // $SHELL already answers the question — so the row is hidden entirely
  // rather than offered as a menu of one.
  $: shellOptions = ($settings.shellChoices ?? []).map((choice) => ({
    value: choice.command,
    label: choice.label || $t('settings.shellSystemDefault'),
  }));
  $: showShellChoice = shellOptions.length > 1;

  function changeShell(v: string) {
    saveSettings({ terminalShell: v });
  }

  // Rebuilt on locale change so the labels follow the chosen language.
  $: gitBranchOptions = [
    { value: 'header', label: $t('settings.gitBranchHeader') },
    { value: 'statusbar', label: $t('settings.gitBranchStatusBar') },
    { value: 'off', label: $t('settings.gitBranchOff') },
  ];

  function changeGitBranchDisplay(v: string) {
    saveSettings({ gitBranchDisplay: v as 'header' | 'statusbar' | 'off' });
  }

  // How long deleted sessions and tabs stay recoverable. "Keep everything" is
  // -1 rather than 0: 0 is what every config predating the setting reports, so
  // it has to keep meaning "the default".
  const KEEP_ALL_RETENTION = '-1';
  const DEFAULT_RETENTION_DAYS = 30;

  $: trashRetentionOptions = [
    { value: '7', label: $t('settings.trashRetentionDays', { days: 7 }) },
    { value: '30', label: $t('settings.trashRetentionDays', { days: 30 }) },
    { value: '90', label: $t('settings.trashRetentionDays', { days: 90 }) },
    { value: KEEP_ALL_RETENTION, label: $t('settings.trashRetentionForever') },
  ];

  // 0 means unset, which resolves to the default, so show that option selected.
  $: trashRetentionValue = String($settings.trashRetentionDays || DEFAULT_RETENTION_DAYS);

  function changeTrashRetention(v: string) {
    saveSettings({ trashRetentionDays: parseInt(v, 10) });
  }

  $: currentUITheme = $settings.uiTheme || DEFAULT_UI_THEME;
  $: customAccent = $settings.uiAccent || '#8b5cf6';
  // A very dark accent is nearly invisible on the dark background; say so
  // rather than leave the user wondering why nothing changed.
  $: customAccentTooDark =
    accentContrastOnBackground(customAccent) < MIN_ACCENT_CONTRAST;

  // Picking a colour also selects the custom theme: choosing a shade and then
  // having to select it separately would be a step with no purpose.
  function pickCustomAccent(hex: string) {
    saveSettings({ uiTheme: CUSTOM_UI_THEME, uiAccent: hex });
  }

  const dispatch = createEventDispatcher();

  // Tab state
  let activeTab: 'general' | 'terminal' | 'shortcuts' | 'agents' | 'dictation' | 'maintenance' = 'general';
  /** Whether the API key is currently readable. */
  let showApiKey = false;
  // App keeps this component mounted and only toggles its `show` prop. Cover
  // the secret whenever the dialog is hidden, including parent-driven closes.
  let previousShow = false;
  let dictationProblems: string[] = [];
  let dictationLoadGeneration = 0;

  // Asked for once when settings open: the answer is fixed at startup, so
  // polling it would be re-reading a constant.
  async function loadDictationProblems(generation: number) {
    try {
      const next = (await DictationService.GetDictationProblems()) ?? [];
      if (!show || generation !== dictationLoadGeneration) return;
      dictationProblems = next;
    } catch {
      if (!show || generation !== dictationLoadGeneration) return;
      dictationProblems = [];
    }
  }


  // Activity-detection patterns: which version is in force, and the result of a
  // manual check. Shown because "already up to date" and a check that quietly
  // failed are otherwise indistinguishable.
  let patternsVersion = 0;
  let refreshingPatterns = false;
  let patternsMessage = '';
  let patternLoadGeneration = 0;

  async function loadPatternsVersion() {
    const generation = ++patternLoadGeneration;
    try {
      const next = await App.DetectionPatternsVersion();
      if (!show || generation !== patternLoadGeneration) return;
      patternsVersion = next;
    } catch {
      if (!show || generation !== patternLoadGeneration) return;
      patternsVersion = 0;
    }
  }

  async function refreshPatterns() {
    refreshingPatterns = true;
    patternsMessage = '';
    try {
      const result = await App.RefreshDetectionPatterns();
      patternsVersion = result?.version ?? patternsVersion;
      patternsMessage = result?.updated
        ? $t('settings.refreshPatternsUpdated').replace('{version}', String(patternsVersion))
        : $t('settings.refreshPatternsCurrent');
      if (result?.updated) {
        // New patterns apply to the next detection rather than needing a
        // restart, and that runs every couple of seconds anyway — but the
        // point of pressing the button is to see the effect, so ask for it
        // now instead of waiting out the poll.
        void loadActivities();
      }
    } catch (e) {
      // The backend returns translation keys, so an unknown one still shows
      // something readable rather than an empty line.
      const key = String(e);
      const translated = $t(key);
      patternsMessage = translated === key ? String(e) : translated;
    }
    refreshingPatterns = false;
  }

  // Dictation settings state
  let dictationSettings: main.DictationSettings | null = null;
  let languages: Array<{code: string, name: string}> = [];
  let inputDevices: Array<{name: string, description: string, isDefault: boolean}> = [];
  let loading = true;
  let audioTestStatus: 'idle' | 'recording' | 'playing' | 'done' | 'error' = 'idle';
  let audioTestMessage = '';
  let audioTestGeneration = 0;
  let audioResetTimer: ReturnType<typeof setTimeout> | null = null;
  let dictationSaveQueue: Promise<void> = Promise.resolve();

  $: if (show && !previousShow) {
    previousShow = true;
  } else if (!show && previousShow) {
    previousShow = false;
    showApiKey = false;
    dictationLoadGeneration++;
    patternLoadGeneration++;
    cancelAudioTest();
  }

  // Punctuation commands for current language
  let punctuationCommands: Record<string, string> = {};
  let deleteCommands: Record<string, string> = {};

  // Default commands (from JSON files)
  const defaultPunctuationCommands: Record<string, Record<string, string>> = {
    hu: {
      "pont": ".",
      "vessző": ",",
      "kérdőjel": "?",
      "felkiáltójel": "!",
      "kettőspont": ":",
      "pontosvessző": ";",
      "kötőjel": "-",
      "gondolatjel": " - ",
      "nyitó zárójel": "(",
      "csukó zárójel": ")",
      "új sor": "\n",
      "új bekezdés": "\n\n"
    },
    en: {
      "period": ".",
      "dot": ".",
      "comma": ",",
      "question mark": "?",
      "exclamation mark": "!",
      "colon": ":",
      "semicolon": ";",
      "hyphen": "-",
      "dash": " - ",
      "open parenthesis": "(",
      "close parenthesis": ")",
      "new line": "\n",
      "new paragraph": "\n\n"
    }
  };

  const defaultDeleteCommands: Record<string, Record<string, string>> = {
    hu: {
      "szusi": "buffer",
      "vegeta": "ctrl_backspace",
      "goku": "ctrl_alt_backspace"
    },
    en: {
      "sushi": "buffer",
      "vegeta": "ctrl_backspace",
      "goku": "ctrl_alt_backspace"
    }
  };

  // Load dictation settings when dialog opens
  // Read the pattern version when the dialog opens, so the maintenance tab can
  // show it without a round trip on every render.
  $: if (show && patternsVersion === 0) void loadPatternsVersion();

  $: if (show && dictationSettings === null) {
    const generation = ++dictationLoadGeneration;
    void loadDictationSettings(generation);
    void loadDictationProblems(generation);
  }

  // Update commands when language changes
  $: if (dictationSettings?.language) {
    loadCommandsForLanguage(dictationSettings.language);
  }

  async function loadDictationSettings(generation: number) {
    loading = true;
    try {
      const [settings, langs, devices] = await Promise.all([
        DictationService.GetDictationSettings(),
        DictationService.GetAvailableLanguages(),
        DictationService.GetInputDevices()
      ]);
      if (!show || generation !== dictationLoadGeneration) return;
      dictationSettings = settings;
      languages = langs.map((l: Record<string, string>) => ({ code: l.code, name: l.name }));
      inputDevices = devices || [];
    } catch (e) {
      if (!show || generation !== dictationLoadGeneration) return;
      console.error('Failed to load dictation settings:', e);
    }
    if (show && generation === dictationLoadGeneration) loading = false;
  }

  function loadCommandsForLanguage(lang: string) {
    punctuationCommands = defaultPunctuationCommands[lang] || defaultPunctuationCommands['en'] || {};
    deleteCommands = defaultDeleteCommands[lang] || defaultDeleteCommands['en'] || {};
  }

  async function runAudioTest() {
    if (audioTestStatus !== 'idle' && audioTestStatus !== 'done' && audioTestStatus !== 'error') return;

    const generation = ++audioTestGeneration;
    if (audioResetTimer) { clearTimeout(audioResetTimer); audioResetTimer = null; }
    audioTestStatus = 'recording';
    audioTestMessage = $t('settings.audioTestRecordingStart');

    try {
      // Countdown
      for (let i = 5; i > 0; i--) {
        audioTestMessage = $t('settings.audioTestRecording', { seconds: i });
        await new Promise(r => setTimeout(r, 1000));
        if (!show || generation !== audioTestGeneration) return;
      }

      audioTestStatus = 'playing';
      audioTestMessage = $t('settings.audioTestPlaying');

      await DictationService.AudioTest();
      if (!show || generation !== audioTestGeneration) return;

      audioTestStatus = 'done';
      audioTestMessage = $t('settings.audioTestDone');

      // Reset after 3 seconds
      audioResetTimer = setTimeout(() => {
        if (generation !== audioTestGeneration) return;
        audioResetTimer = null;
        audioTestStatus = 'idle';
        audioTestMessage = '';
      }, 3000);
    } catch (e) {
      if (!show || generation !== audioTestGeneration) return;
      audioTestStatus = 'error';
      audioTestMessage = $t('settings.audioTestError', { error: String(e) });

      audioResetTimer = setTimeout(() => {
        if (generation !== audioTestGeneration) return;
        audioResetTimer = null;
        audioTestStatus = 'idle';
        audioTestMessage = '';
      }, 5000);
    }
  }

  function cancelAudioTest() {
    audioTestGeneration++;
    if (audioResetTimer) { clearTimeout(audioResetTimer); audioResetTimer = null; }
    audioTestStatus = 'idle';
    audioTestMessage = '';
  }

  onDestroy(() => {
    dictationLoadGeneration++;
    patternLoadGeneration++;
    cancelAudioTest();
  });

  async function saveDictationSettings() {
    if (!dictationSettings) return;
    const snapshot = JSON.stringify(dictationSettings);
    // DictationService writes the whole settings object. Serialize snapshots
    // so an older, slower write cannot land after a newer toggle and restore
    // the previous values.
    const save = dictationSaveQueue
      .catch(() => {})
      .then(() => DictationService.SetDictationSettings(snapshot));
    dictationSaveQueue = save;
    try {
      await save;
    } catch (e) {
      console.error('Failed to save dictation settings:', e);
    }
  }

  let forgettingWindow = false;
  let forgotWindow = false;

  /**
   * Discard the remembered window position, so the next launch centres it.
   *
   * The confirmation sits on the button rather than in a toast: the effect is
   * invisible until the app restarts, so without it nothing shows the click
   * did anything at all.
   */
  async function forgetWindowGeometry() {
    if (forgettingWindow) return;
    forgettingWindow = true;
    try {
      await App.ForgetWindowGeometry();
      forgotWindow = true;
      setTimeout(() => { forgotWindow = false; }, 3000);
    } catch (e) {
      console.error('Failed to forget the window geometry:', e);
    } finally {
      forgettingWindow = false;
    }
  }

  function updateDictation<K extends keyof main.DictationSettings>(key: K, value: main.DictationSettings[K]) {
    if (!dictationSettings) return;
    (dictationSettings as any)[key] = value;
    dictationSettings = dictationSettings; // trigger reactivity
    saveDictationSettings();
    // Rebinding takes effect in the panes immediately. Without this the
    // terminal would keep declining the old combination and pass the new one
    // straight through to the agent.
    setDictationHotkey({
      ctrl: dictationSettings.hotkeyCtrl,
      alt: dictationSettings.hotkeyAlt,
      shift: dictationSettings.hotkeyShift,
      key: dictationSettings.hotkeyKey,
    });

    // Notify parent when enabled state changes
    if (key === 'enabled') {
      dispatch('dictationEnabledChange', value as boolean);
    }

    // Notify TabBar when buffer mode, close-on-send, or mode changes
    if (key === 'bufferMode' || key === 'mode' || key === 'bufferCloseOnSend') {
      EventsEmit('dictation:settingsChanged');
    }
  }

  function close() {
    show = false;
    dispatch('close');
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      close();
    }
  }

  function toggle(key: 'hideStatusLines' | 'showAgentIcons' | 'compactList' | 'hideViewBar' | 'agentHideViewBar' | 'hideStatusBar' | 'agentHideStatusBar' | 'notifyOnWaiting' | 'notifyDesktop' | 'notifyNtfy' | 'taskMasterEnabled' | 'restoreLastSession') {
    saveSettings({ [key]: !$settings[key] });
  }

  function formatCommandValue(value: string): string {
    if (value === '\n') return '[new line]';
    if (value === '\n\n') return '[new paragraph]';
    if (value === ' - ') return '[ - ]';
    return value;
  }

  // Takes the translate function rather than reading $t inside: called from
  // the markup, Svelte re-runs this only when its arguments change, so a
  // language switch would leave the old wording on screen.
  function formatDeleteAction(action: string, tr: typeof $t): string {
    switch (action) {
      case 'buffer': return tr('settings.deleteAction.buffer');
      case 'ctrl_backspace': return tr('settings.deleteAction.ctrlBackspace');
      case 'ctrl_alt_backspace': return tr('settings.deleteAction.ctrlAltBackspace');
      default: return action;
    }
  }
</script>

{#if show}
  <div
    class="dialog-overlay" use:autoFocusDialog
    on:click|self={close}
    on:keydown={handleKeydown}
    role="dialog"
    aria-modal="true"
  >
    <div class="dialog-content">
      <div class="dialog-header">
        <h2>{$t('settings.title')}</h2>
        <button class="close-btn" on:click={close}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"/>
            <line x1="6" y1="6" x2="18" y2="18"/>
          </svg>
        </button>
      </div>

      <!-- Tabs -->
      <div class="tabs">
        <button
          class="tab"
          class:active={activeTab === 'general'}
          on:click={() => activeTab = 'general'}
        >
          {$t('settings.general')}
        </button>
        <button
          class="tab"
          class:active={activeTab === 'terminal'}
          on:click={() => activeTab = 'terminal'}
        >
          {$t('settings.terminal')}
        </button>
        <button
          class="tab"
          class:active={activeTab === 'agents'}
          on:click={() => activeTab = 'agents'}
        >
          {$t('settings.agents')}
        </button>
        <button
          class="tab"
          class:active={activeTab === 'shortcuts'}
          on:click={() => activeTab = 'shortcuts'}
        >
          {$t('settings.shortcuts')}
        </button>
        <button
          class="tab"
          class:active={activeTab === 'dictation'}
          on:click={() => activeTab = 'dictation'}
        >
          {$t('settings.dictation')}
        </button>
        <button
          class="tab"
          class:active={activeTab === 'maintenance'}
          on:click={() => activeTab = 'maintenance'}
        >
          {$t('settings.maintenance')}
        </button>
      </div>

      <div class="settings-list">
        <!-- General Tab -->
        {#if activeTab === 'general'}
          <div class="settings-section">
            <h3>{$t('settings.language')}</h3>

            <div class="setting-item input-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.language')}</span>
                <span class="setting-desc">{$t('settings.languageDesc')}</span>
              </span>
              <Select
                value={$settings.language || 'en'}
                options={languageOptions}
                on:change={(e) => changeLanguage(e.detail)}
              />
            </div>

          </div>

          <div class="settings-section">
            <h3>{$t('settings.appearance')}</h3>

            <div class="setting-item input-item column-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.uiTheme')}</span>
                <span class="setting-desc">{$t('settings.uiThemeDesc')}</span>
              </span>
              <div class="theme-grid">
                {#each UI_THEMES as th (th.id)}
                  <button
                    class="theme-swatch"
                    class:selected={currentUITheme === th.id}
                    title={th.name}
                    style="--sw: {th.accent}; --sw-light: {th.light}"
                    on:click={() => saveSettings({ uiTheme: th.id })}
                  >
                    <span class="theme-dot"></span>
                    <span class="theme-name">{th.name}</span>
                  </button>
                {/each}

                <!-- The custom entry doubles as its own colour picker: the
                     swatch shows the current choice, clicking opens the
                     native picker. Only one colour is asked for — the lighter
                     and darker steps, and the readable text colour, are
                     derived from it. -->
                <label
                  class="theme-swatch custom"
                  class:selected={currentUITheme === CUSTOM_UI_THEME}
                  title={$t('settings.uiThemeCustom')}
                  style="--sw: {customAccent}; --sw-light: {customAccent}"
                >
                  <span class="theme-dot custom-dot"></span>
                  <span class="theme-name">{$t('settings.uiThemeCustom')}</span>
                  <input
                    type="color"
                    value={customAccent}
                    on:input={(e) => pickCustomAccent(e.currentTarget.value)}
                  />
                </label>
              </div>
              {#if currentUITheme === CUSTOM_UI_THEME && customAccentTooDark}
                <p class="accent-warning">{$t('settings.uiAccentTooDark')}</p>
              {/if}
            </div>
          </div>

          <div class="settings-section">
            <h3>{$t('settings.windowSection')}</h3>

            <!-- Not input-item: that aligns to flex-start for tall fields,
                 which leaves a button hanging below its own label. -->
            <div class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.forgetWindowGeometry')}</span>
                <span class="setting-desc">{$t('settings.forgetWindowGeometryDesc')}</span>
              </span>
              <button class="action-btn" on:click={forgetWindowGeometry} disabled={forgettingWindow}>
                {forgotWindow ? $t('settings.forgetWindowGeometryDone') : $t('settings.forgetWindowGeometryAction')}
              </button>
            </div>
          </div>

          <!-- Renderer lives here, not on the Terminal tab: it applies to
               every terminal view, agent sessions included. -->
          <div class="settings-section">
            <h3>{$t('settings.terminalRendering')}</h3>

            <div class="setting-item input-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.terminalRenderer')}</span>
                <span class="setting-desc">{$t('settings.terminalRendererDesc')}</span>
              </span>
              <Select
                value={$settings.terminalRenderer || 'canvas'}
                options={rendererOptions}
                on:change={(e) => changeRenderer(e.detail)}
              />
            </div>

            <div class="setting-item input-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.terminalFont')}</span>
                <span class="setting-desc">{$t('settings.terminalFontDesc')}</span>
              </span>
              <Select
                value={$settings.terminalFontFamily || ''}
                options={fontOptions}
                on:change={(e) => changeFontFamily(e.detail)}
              />
            </div>

            <div class="setting-item input-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.terminalCopyMode')}</span>
                <span class="setting-desc">{$t('settings.terminalCopyModeDesc')}</span>
              </span>
              <Select
                value={$settings.terminalCopyMode || 'shift'}
                options={copyModeOptions}
                on:change={(e) => changeCopyMode(e.detail)}
              />
            </div>
          </div>

          <div class="settings-section">
            <h3>{$t('settings.gitSection')}</h3>

            <div class="setting-item input-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.gitBranchDisplay')}</span>
                <span class="setting-desc">{$t('settings.gitBranchDisplayDesc')}</span>
              </span>
              <Select
                value={$settings.gitBranchDisplay || 'header'}
                options={gitBranchOptions}
                on:change={(e) => changeGitBranchDisplay(e.detail)}
              />
            </div>
          </div>

          <div class="settings-section">
            <h3>{$t('settings.sessionList')}</h3>

            <label class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.showStatusLines')}</span>
                <span class="setting-desc">{$t('settings.showStatusLinesDesc')}</span>
              </span>
              <button
                class="toggle-btn"
                class:active={!$settings.hideStatusLines}
                on:click={() => toggle('hideStatusLines')}
              >
                <span class="toggle-track">
                  <span class="toggle-thumb"></span>
                </span>
              </button>
            </label>

            <label class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.showAgentIcons')}</span>
                <span class="setting-desc">{$t('settings.showAgentIconsDesc')}</span>
              </span>
              <button
                class="toggle-btn"
                class:active={$settings.showAgentIcons}
                on:click={() => toggle('showAgentIcons')}
              >
                <span class="toggle-track">
                  <span class="toggle-thumb"></span>
                </span>
              </button>
            </label>

            <!-- YOLO is stored inverted ("hide") so existing settings keep
                 showing it; the resume marker is opt-in. Both are presented
                 as "show" toggles to match the others in this section. -->
            <label class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.showYoloBadge')}</span>
                <span class="setting-desc">{$t('settings.showYoloBadgeDesc')}</span>
              </span>
              <button
                class="toggle-btn"
                class:active={!$settings.hideYoloBadge}
                on:click={() => saveSettings({ hideYoloBadge: !$settings.hideYoloBadge })}
              >
                <span class="toggle-track">
                  <span class="toggle-thumb"></span>
                </span>
              </button>
            </label>

            <label class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.showResumeBadge')}</span>
                <span class="setting-desc">{$t('settings.showResumeBadgeDesc')}</span>
              </span>
              <button
                class="toggle-btn"
                class:active={$settings.showResumeBadge}
                on:click={() => saveSettings({ showResumeBadge: !$settings.showResumeBadge })}
              >
                <span class="toggle-track">
                  <span class="toggle-thumb"></span>
                </span>
              </button>
            </label>

            <label class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.compactList')}</span>
                <span class="setting-desc">{$t('settings.compactListDesc')}</span>
              </span>
              <button
                class="toggle-btn"
                class:active={$settings.compactList}
                on:click={() => toggle('compactList')}
              >
                <span class="toggle-track">
                  <span class="toggle-thumb"></span>
                </span>
              </button>
            </label>

            <label class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.restoreLastSession')}</span>
                <span class="setting-desc">{$t('settings.restoreLastSessionDesc')}</span>
              </span>
              <button
                class="toggle-btn"
                class:active={$settings.restoreLastSession}
                on:click={() => toggle('restoreLastSession')}
              >
                <span class="toggle-track">
                  <span class="toggle-thumb"></span>
                </span>
              </button>
            </label>
          </div>

          <div class="settings-section">
            <h3>{$t('settings.notifications')}</h3>

            <label class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.notifyOnWaiting')}</span>
                <span class="setting-desc">{$t('settings.notifyOnWaitingDesc')}</span>
              </span>
              <button
                class="toggle-btn"
                class:active={$settings.notifyOnWaiting}
                on:click={() => toggle('notifyOnWaiting')}
              >
                <span class="toggle-track">
                  <span class="toggle-thumb"></span>
                </span>
              </button>
            </label>

            {#if $settings.notifyOnWaiting}
              <label class="setting-item">
                <span class="setting-info">
                  <span class="setting-label">{$t('settings.notifyDesktop')}</span>
                  <span class="setting-desc">{$t('settings.notifyDesktopDesc')}</span>
                </span>
                <button
                  class="toggle-btn"
                  class:active={$settings.notifyDesktop}
                  on:click={() => toggle('notifyDesktop')}
                >
                  <span class="toggle-track">
                    <span class="toggle-thumb"></span>
                  </span>
                </button>
              </label>

              <label class="setting-item">
                <span class="setting-info">
                  <span class="setting-label">{$t('settings.notifyNtfy')}</span>
                  <span class="setting-desc">{$t('settings.notifyNtfyDesc')}</span>
                </span>
                <button
                  class="toggle-btn"
                  class:active={$settings.notifyNtfy}
                  on:click={() => toggle('notifyNtfy')}
                >
                  <span class="toggle-track">
                    <span class="toggle-thumb"></span>
                  </span>
                </button>
              </label>

              {#if $settings.notifyNtfy}
                <div class="setting-item input-item">
                  <span class="setting-info">
                    <span class="setting-label">{$t('settings.ntfyUrl')}</span>
                    <span class="setting-desc">{$t('settings.ntfyUrlDesc')}</span>
                  </span>
                  <input
                    type="text"
                    class="setting-input"
                    placeholder="https://ntfy.sh/my-topic"
                    value={$settings.ntfyUrl}
                    on:input={(e) => saveSettings({ ntfyUrl: e.currentTarget.value })}
                  />
                </div>
              {/if}
            {/if}
          </div>

          <!-- General rather than Maintenance: this switches a view on and off,
               like the other toggles on this tab. Maintenance is for one-off
               actions (export, update check), not for preferences. -->
          <div class="settings-section">
            <h3>{$t('settings.experimental')}</h3>

            <label class="setting-item">
              <span class="setting-info">
                <span class="setting-label">
                  {$t('settings.taskMaster')}
                  <span class="experimental-badge">{$t('settings.experimentalBadge')}</span>
                </span>
                <span class="setting-desc">{$t('settings.taskMasterDesc')}</span>
              </span>
              <button
                class="toggle-btn"
                class:active={$settings.taskMasterEnabled}
                on:click={() => toggle('taskMasterEnabled')}
              >
                <span class="toggle-track">
                  <span class="toggle-thumb"></span>
                </span>
              </button>
            </label>
          </div>

        {/if}

        <!-- Terminal Tab -->
        {#if activeTab === 'terminal'}
          {#if showShellChoice}
            <div class="settings-section">
              <h3>{$t('settings.shellSection')}</h3>

              <div class="setting-item input-item">
                <span class="setting-info">
                  <span class="setting-label">{$t('settings.terminalShell')}</span>
                  <span class="setting-desc">{$t('settings.terminalShellDesc')}</span>
                </span>
                <Select
                  value={$settings.terminalShell || ''}
                  options={shellOptions}
                  on:change={(e) => changeShell(e.detail)}
                />
              </div>
            </div>
          {/if}

          <div class="settings-section">
            <h3>{$t('settings.paletteTerminalDefault')}</h3>

            <div class="setting-item input-item column-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.terminalTheme')}</span>
                <span class="setting-desc">{$t('settings.terminalThemeDesc')}</span>
              </span>
              <PalettePicker
                palettes={pickablePalettes}
                value={$settings.terminalTheme || 'asmgr'}
                on:change={(e) => changeTheme(e.detail)}
              />
            </div>

          </div>

          <div class="settings-section">
            <h3>{$t('settings.fontSize')}</h3>

            <div class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.fontSize')}</span>
                <span class="setting-desc">{$t('settings.fontSizeDesc')}</span>
              </span>
              <div class="font-size-control">
                <input
                  type="range"
                  min={MIN_FONT_SIZE}
                  max={MAX_FONT_SIZE}
                  value={currentFontSize}
                  on:input={(e) => changeFontSize(+e.currentTarget.value)}
                />
                <span class="font-size-value">{currentFontSize}px</span>
              </div>
            </div>

            <label class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.hideViewBar')}</span>
                <span class="setting-desc">{$t('settings.hideViewBarDesc')}</span>
              </span>
              <button
                class="toggle-btn"
                class:active={$settings.hideViewBar}
                on:click={() => toggle('hideViewBar')}
              >
                <span class="toggle-track">
                  <span class="toggle-thumb"></span>
                </span>
              </button>
            </label>

            <label class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.hideStatusBar')}</span>
                <span class="setting-desc">{$t('settings.hideStatusBarDesc')}</span>
              </span>
              <button
                class="toggle-btn"
                class:active={$settings.hideStatusBar}
                on:click={() => toggle('hideStatusBar')}
              >
                <span class="toggle-track">
                  <span class="toggle-thumb"></span>
                </span>
              </button>
            </label>
          </div>

          <div class="settings-section">
            <h3>{$t('settings.paletteCustom')}</h3>

            <!-- User-defined palettes (shared manager component) -->
            <div class="setting-item input-item column-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.customPalette')}</span>
                <span class="setting-desc">{$t('settings.customPaletteDesc')}</span>
              </span>
              <PaletteManager />
            </div>

          </div>

        {/if}

        <!-- Shortcuts Tab -->
        {#if activeTab === 'shortcuts'}
          <div class="settings-section">
            <ShortcutEditor />
          </div>
        {/if}

        <!-- Agents Tab -->
        {#if activeTab === 'agents'}
          <!-- Agent tabs keep their own defaults, deliberately unaware of the
               terminal ones — the same split the palettes use. -->
          <div class="settings-section">
            <h3>{$t('settings.fontSize')}</h3>

            <div class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.fontSize')}</span>
                <span class="setting-desc">{$t('settings.agentFontSizeDesc')}</span>
              </span>
              <div class="font-size-control">
                <input
                  type="range"
                  min={MIN_FONT_SIZE}
                  max={MAX_FONT_SIZE}
                  value={currentAgentFontSize}
                  on:input={(e) => changeAgentFontSize(+e.currentTarget.value)}
                />
                <span class="font-size-value">{currentAgentFontSize}px</span>
              </div>
            </div>

            <label class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.hideViewBar')}</span>
                <span class="setting-desc">{$t('settings.agentHideViewBarDesc')}</span>
              </span>
              <button
                class="toggle-btn"
                class:active={$settings.agentHideViewBar}
                on:click={() => toggle('agentHideViewBar')}
              >
                <span class="toggle-track">
                  <span class="toggle-thumb"></span>
                </span>
              </button>
            </label>

            <label class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.hideStatusBar')}</span>
                <span class="setting-desc">{$t('settings.agentHideStatusBarDesc')}</span>
              </span>
              <button
                class="toggle-btn"
                class:active={$settings.agentHideStatusBar}
                on:click={() => toggle('agentHideStatusBar')}
              >
                <span class="toggle-track">
                  <span class="toggle-thumb"></span>
                </span>
              </button>
            </label>
          </div>

          <div class="settings-section">
            <h3>{$t('settings.paletteAgentDefault')}</h3>

            <div class="setting-item input-item column-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.agentDefaultTheme')}</span>
                <span class="setting-desc">{$t('settings.agentDefaultThemeDesc')}</span>
              </span>
              <PalettePicker
                palettes={pickablePalettes}
                value={agentDefaultTheme}
                on:change={(e) => changeAgentDefault(e.detail)}
              />
            </div>
          </div>

          <div class="settings-section">
            <h3>{$t('settings.palettePerAgent')}</h3>

            <!-- Per-agent overrides: a Claude pane can look different from a
                 plain shell, like separate Konsole profiles. -->
            <div class="setting-item input-item column-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.agentThemes')}</span>
                <span class="setting-desc">{$t('settings.agentThemesDesc')}</span>
              </span>
              {#each $agents.filter(a => a.type !== 'terminal') as agent (agent.type)}
                <details class="agent-theme-block" open>
                  <summary>
                    <span class="agent-theme-name">{agent.name}</span>
                    <span class="agent-theme-current">
                      {pickablePalettes.find(p => p.id === agentThemeValue(agent.type))?.name || $t('settings.themeInherit')}
                    </span>
                  </summary>
                  <PalettePicker
                    compact
                    palettes={pickablePalettes}
                    value={agentThemeValue(agent.type)}
                    inheritLabel={$t('settings.themeInherit')}
                    on:change={(e) => changeAgentTheme(agent.type, e.detail)}
                  />
                </details>
              {/each}

              <!-- Create/edit palettes without leaving the Agents tab. -->
              <PaletteManager collapsible />
            </div>

          </div>

        {/if}

        <!-- Dictation Tab -->
        {#if activeTab === 'dictation'}
          <!-- Anything that stopped dictation setting itself up. Shown at the
               top of its own tab because the failure it reports — no xdotool or
               ydotool — makes speech recognise perfectly and type nothing,
               which looks like a broken microphone rather than a missing
               package. -->
          {#if dictationProblems?.length}
            <div class="dictation-problem">
              <strong>{$t('settings.dictationProblem')}</strong>
              {#each dictationProblems as problem}
                <pre>{problem}</pre>
              {/each}
            </div>
          {/if}
          {#if loading}
            <div class="loading">{$t('settings.loading')}</div>
          {:else if dictationSettings}
            <div class="settings-section">
              <h3>{$t('settings.configuration')}</h3>

              <label class="setting-item">
                <span class="setting-info">
                  <span class="setting-label">{$t('settings.enableDictation')}</span>
                  <span class="setting-desc">{$t('settings.enableDictationDesc')}</span>
                </span>
                <button
                  class="toggle-btn"
                  class:active={dictationSettings.enabled}
                  on:click={() => updateDictation('enabled', !dictationSettings?.enabled)}
                >
                  <span class="toggle-track">
                    <span class="toggle-thumb"></span>
                  </span>
                </button>
              </label>

              {#if dictationSettings.enabled}
                <div class="setting-item input-item">
                  <span class="setting-info">
                    <span class="setting-label">{$t('settings.mode')}</span>
                    <span class="setting-desc">{$t('settings.modeDesc')}</span>
                  </span>
                  <Select
                    value={dictationSettings.mode}
                    options={[
                      { value: 'free', label: $t('settings.modeFree') },
                      { value: 'api', label: $t('settings.modeApi') },
                      { value: 'streaming', label: $t('settings.modeStreaming') }
                    ]}
                    on:change={(e) => updateDictation('mode', e.detail)}
                  />
                </div>

                {#if dictationSettings.mode !== 'free'}
                  <div class="setting-item input-item">
                    <span class="setting-info">
                      <span class="setting-label">{$t('settings.googleApiKey')}</span>
                      <span class="setting-desc">{$t('settings.googleApiKeyDesc')}</span>
                    </span>
                    <!-- Revealable, because a key is pasted in and typos are
                         invisible behind dots: the usual failure is a stray
                         character or a truncated paste, and the only way to see
                         one is to look. Hidden by default, and never persisted
                         as shown — the next visit starts covered. -->
                    <span class="key-field">
                      <input
                        type={showApiKey ? 'text' : 'password'}
                        class="setting-input"
                        value={dictationSettings.googleApiKey}
                        placeholder={$t('settings.googleApiKeyPlaceholder')}
                        on:change={(e) => updateDictation('googleApiKey', e.currentTarget.value)}
                      />
                      <button
                        class="key-reveal"
                        type="button"
                        title={showApiKey ? $t('settings.hideApiKey') : $t('settings.showApiKey')}
                        aria-label={showApiKey ? $t('settings.hideApiKey') : $t('settings.showApiKey')}
                        on:click={() => showApiKey = !showApiKey}
                      >
                        {#if showApiKey}
                          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/>
                            <line x1="1" y1="1" x2="23" y2="23"/>
                          </svg>
                        {:else}
                          <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
                            <circle cx="12" cy="12" r="3"/>
                          </svg>
                        {/if}
                      </button>
                    </span>
                  </div>
                {/if}

                {#if dictationSettings.mode === 'streaming'}
                  <label class="setting-item">
                    <span class="setting-info">
                      <span class="setting-label">{$t('settings.bufferMode')}</span>
                      <span class="setting-desc">{$t('settings.bufferModeDesc')}</span>
                    </span>
                    <button
                      class="toggle-btn"
                      class:active={dictationSettings.bufferMode}
                      on:click={() => updateDictation('bufferMode', !dictationSettings?.bufferMode)}
                    >
                      <span class="toggle-track">
                        <span class="toggle-thumb"></span>
                      </span>
                    </button>
                  </label>

                  {#if dictationSettings.bufferMode}
                    <label class="setting-item">
                      <span class="setting-info">
                        <span class="setting-label">{$t('settings.closeAfterSend')}</span>
                        <span class="setting-desc">{$t('settings.closeAfterSendDesc')}</span>
                      </span>
                      <button
                        class="toggle-btn"
                        class:active={dictationSettings.bufferCloseOnSend}
                        on:click={() => updateDictation('bufferCloseOnSend', !dictationSettings?.bufferCloseOnSend)}
                      >
                        <span class="toggle-track">
                          <span class="toggle-thumb"></span>
                        </span>
                      </button>
                    </label>
                  {/if}
                {/if}

                <div class="setting-item input-item">
                  <span class="setting-info">
                    <span class="setting-label">{$t('settings.dictLanguage')}</span>
                    <span class="setting-desc">{$t('settings.dictLanguageDesc')}</span>
                  </span>
                  <Select
                    value={dictationSettings.language}
                    options={languages.map(l => ({ value: l.code, label: l.name }))}
                    on:change={(e) => updateDictation('language', e.detail)}
                  />
                </div>

                <div class="setting-item input-item">
                  <span class="setting-info">
                    <span class="setting-label">{$t('settings.inputDevice')}</span>
                    <span class="setting-desc">{$t('settings.inputDeviceDesc')}</span>
                  </span>
                  <Select
                    value={dictationSettings.inputDevice || ''}
                    options={inputDevices.map(d => ({ value: d.name, label: d.description + (d.isDefault ? ' (Default)' : '') }))}
                    on:change={(e) => updateDictation('inputDevice', e.detail)}
                  />
                </div>

                <div class="setting-item hotkey-item">
                  <span class="setting-info">
                    <span class="setting-label">{$t('settings.hotkey')}</span>
                    <span class="setting-desc">{$t('settings.hotkeyDesc')}</span>
                  </span>
                  <div class="hotkey-config">
                    <label class="modifier-checkbox">
                      <input
                        type="checkbox"
                        checked={dictationSettings.hotkeyCtrl}
                        on:change={(e) => updateDictation('hotkeyCtrl', e.currentTarget.checked)}
                      />
                      Ctrl
                    </label>
                    <label class="modifier-checkbox">
                      <input
                        type="checkbox"
                        checked={dictationSettings.hotkeyAlt}
                        on:change={(e) => updateDictation('hotkeyAlt', e.currentTarget.checked)}
                      />
                      Alt
                    </label>
                    <label class="modifier-checkbox">
                      <input
                        type="checkbox"
                        checked={dictationSettings.hotkeyShift}
                        on:change={(e) => updateDictation('hotkeyShift', e.currentTarget.checked)}
                      />
                      Shift
                    </label>
                    <span class="plus">+</span>
                    <input
                      type="text"
                      class="hotkey-key"
                      maxlength="1"
                      value={dictationSettings.hotkeyKey}
                      on:change={(e) => updateDictation('hotkeyKey', e.currentTarget.value.toLowerCase())}
                    />
                  </div>
                </div>

                <label class="setting-item">
                  <span class="setting-info">
                    <span class="setting-label">{$t('settings.muteOutput')}</span>
                    <span class="setting-desc">{$t('settings.muteOutputDesc')}</span>
                  </span>
                  <button
                    class="toggle-btn"
                    class:active={dictationSettings.muteOutputDuringRecording}
                    on:click={() => updateDictation('muteOutputDuringRecording', !dictationSettings?.muteOutputDuringRecording)}
                  >
                    <span class="toggle-track">
                      <span class="toggle-thumb"></span>
                    </span>
                  </button>
                </label>

                <label class="setting-item">
                  <span class="setting-info">
                    <span class="setting-label">{$t('settings.autoStop')}</span>
                    <span class="setting-desc">{$t('settings.autoStopDesc')}</span>
                  </span>
                  <button
                    class="toggle-btn"
                    class:active={dictationSettings.autoStopOnSilence}
                    on:click={() => updateDictation('autoStopOnSilence', !dictationSettings?.autoStopOnSilence)}
                  >
                    <span class="toggle-track">
                      <span class="toggle-thumb"></span>
                    </span>
                  </button>
                </label>

                {#if dictationSettings.autoStopOnSilence}
                  <div class="setting-item input-item">
                    <span class="setting-info">
                      <span class="setting-label">{$t('settings.silenceDuration')}</span>
                      <span class="setting-desc">{$t('settings.silenceDurationDesc')}</span>
                    </span>
                    <input
                      type="number"
                      class="setting-input number-input"
                      min="0.1"
                      max="5"
                      step="0.1"
                      value={dictationSettings.silenceDuration}
                      on:change={(e) => updateDictation('silenceDuration', parseFloat(e.currentTarget.value))}
                    />
                  </div>

                  <div class="setting-item input-item">
                    <span class="setting-info">
                      <span class="setting-label">{$t('settings.noiseThreshold')}</span>
                      <span class="setting-desc">{$t('settings.noiseThresholdDesc')}</span>
                    </span>
                    <div class="slider-row">
                      <input
                        type="range"
                        class="setting-slider"
                        min="0"
                        max="100"
                        step="1"
                        value={dictationSettings.silenceThreshold}
                        on:input={(e) => updateDictation('silenceThreshold', parseFloat(e.currentTarget.value))}
                      />
                      <span class="slider-value">{dictationSettings.silenceThreshold}%</span>
                    </div>
                  </div>
                {/if}

                <label class="setting-item">
                  <span class="setting-info">
                    <span class="setting-label">{$t('settings.enableLogging')}</span>
                    <span class="setting-desc">{$t('settings.enableLoggingDesc')}</span>
                  </span>
                  <button
                    class="toggle-btn"
                    class:active={dictationSettings.enableLogging}
                    on:click={() => updateDictation('enableLogging', !dictationSettings?.enableLogging)}
                  >
                    <span class="toggle-track">
                      <span class="toggle-thumb"></span>
                    </span>
                  </button>
                </label>

                {#if dictationSettings.enableLogging}
                  <label class="setting-item">
                    <span class="setting-info">
                      <span class="setting-label">{$t('settings.debugLogging')}</span>
                      <span class="setting-desc">{$t('settings.debugLoggingDesc')}</span>
                    </span>
                    <button
                      class="toggle-btn"
                      class:active={dictationSettings.enableDebugLogging}
                      on:click={() => updateDictation('enableDebugLogging', !dictationSettings?.enableDebugLogging)}
                    >
                      <span class="toggle-track">
                        <span class="toggle-thumb"></span>
                      </span>
                    </button>
                  </label>
                {/if}

                <div class="setting-item audio-test-item">
                  <span class="setting-info">
                    <span class="setting-label">{$t('settings.audioTest')}</span>
                    <span class="setting-desc">{$t('settings.audioTestDesc')}</span>
                  </span>
                  <button
                    class="audio-test-btn"
                    class:recording={audioTestStatus === 'recording'}
                    class:playing={audioTestStatus === 'playing'}
                    disabled={audioTestStatus === 'recording' || audioTestStatus === 'playing'}
                    on:click={runAudioTest}
                  >
                    {#if audioTestStatus === 'idle'}
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/>
                        <path d="M19 10v2a7 7 0 0 1-14 0v-2"/>
                      </svg>
                      {$t('settings.audioTestBtn')}
                    {:else if audioTestStatus === 'recording'}
                      <span class="recording-dot"></span>
                      {audioTestMessage}
                    {:else if audioTestStatus === 'playing'}
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor">
                        <polygon points="5 3 19 12 5 21 5 3"/>
                      </svg>
                      {audioTestMessage}
                    {:else if audioTestStatus === 'done'}
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <polyline points="20 6 9 17 4 12"/>
                      </svg>
                      {audioTestMessage}
                    {:else}
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <circle cx="12" cy="12" r="10"/>
                        <line x1="15" y1="9" x2="9" y2="15"/>
                        <line x1="9" y1="9" x2="15" y2="15"/>
                      </svg>
                      {audioTestMessage}
                    {/if}
                  </button>
                </div>
              {/if}
            </div>

            {#if dictationSettings.enabled}
              <div class="settings-section">
                <h3>{$t('settings.punctuationCommands')}</h3>
                <p class="section-desc">{$t('settings.punctuationCommandsDesc')}</p>
                <div class="commands-list">
                  {#each Object.entries(punctuationCommands) as [command, value]}
                    <div class="command-item">
                      <span class="command-word">"{command}"</span>
                      <span class="command-arrow">&rarr;</span>
                      <span class="command-value">{formatCommandValue(value)}</span>
                    </div>
                  {/each}
                </div>
              </div>

              <div class="settings-section">
                <h3>{$t('settings.deleteCommands')}</h3>
                <p class="section-desc">{$t('settings.deleteCommandsDesc')}</p>
                <div class="commands-list">
                  {#each Object.entries(deleteCommands) as [command, action]}
                    <div class="command-item">
                      <span class="command-word">"{command}"</span>
                      <span class="command-arrow">&rarr;</span>
                      <span class="command-value">{formatDeleteAction(action, $t)}</span>
                    </div>
                  {/each}
                </div>
              </div>
            {/if}
          {/if}
        {/if}

        <!-- Maintenance: occasional, one-off actions. They lived in the header
             toolbar, where they competed with everyday controls and their icons
             were easy to confuse with each other. -->
        {#if activeTab === 'maintenance'}
          <div class="settings-section">
            <h3>{$t('settings.recoverySection')}</h3>

            <div class="setting-item input-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.trashRetention')}</span>
                <span class="setting-desc">{$t('settings.trashRetentionDesc')}</span>
              </span>
              <Select
                value={trashRetentionValue}
                options={trashRetentionOptions}
                on:change={(e) => changeTrashRetention(e.detail)}
              />
            </div>
          </div>

          <div class="settings-section">
            <h3>{$t('settings.logSection')}</h3>

            <div class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.logSection')}</span>
                <span class="setting-desc">{$t('settings.logDesc')}</span>
              </span>
              <button class="action-btn" on:click={() => dispatch('openLogs')}>
                {$t('settings.logView')}
              </button>
            </div>
          </div>

          <div class="settings-section">
            <h3>{$t('settings.checkUpdates')}</h3>

            <div class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.checkUpdates')}</span>
                <span class="setting-desc">{$t('settings.checkUpdatesDesc')}</span>
              </span>
              <button class="action-btn" on:click={() => dispatch('openUpdate')}>
                {$t('settings.checkUpdatesAction')}
              </button>
            </div>

            <div class="setting-item">
              <span class="setting-info">
                <span class="setting-label">
                  {$t('settings.refreshPatterns')}
                  {#if patternsVersion > 0}
                    <span class="patterns-version">v{patternsVersion}</span>
                  {/if}
                </span>
                <span class="setting-desc">
                  {patternsMessage || $t('settings.refreshPatternsDesc')}
                </span>
              </span>
              <button
                class="action-btn"
                disabled={refreshingPatterns}
                on:click={refreshPatterns}
              >
                {refreshingPatterns
                  ? $t('settings.refreshPatternsBusy')
                  : $t('settings.refreshPatternsAction')}
              </button>
            </div>
          </div>

          <div class="settings-section">
            <h3>{$t('settings.sessionTransfer')}</h3>

            <div class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.exportSessions')}</span>
                <span class="setting-desc">{$t('settings.exportSessionsDesc')}</span>
              </span>
              <button class="action-btn" on:click={() => dispatch('exportSessions')}>
                {$t('settings.exportSessionsAction')}
              </button>
            </div>

            <div class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.importFromFile')}</span>
                <span class="setting-desc">{$t('settings.importFromFileDesc')}</span>
              </span>
              <button class="action-btn" on:click={() => dispatch('openFileImport')}>
                {$t('settings.importFromFileAction')}
              </button>
            </div>

            <div class="setting-item">
              <span class="setting-info">
                <span class="setting-label">{$t('settings.importSessions')}</span>
                <span class="setting-desc">{$t('settings.importSessionsDesc')}</span>
              </span>
              <button class="action-btn" on:click={() => dispatch('openImport')}>
                {$t('settings.importSessionsAction')}
              </button>
            </div>
          </div>
        {/if}
      </div>

      <div class="dialog-footer">
        <span class="hint">{$t('settings.savedAutomatically')}</span>
      </div>
    </div>
  </div>
{/if}

<style>
  /* Must beat .input-item's align-items:flex-start, which would shrink a
     grid child to its content width and collapse it to a single column. */
  .setting-item.column-item { align-items: stretch; }
  .column-item { flex-direction: column; gap: 10px; }
  .column-item > :global(*) { width: 100%; }
  .agent-theme-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(230px, 1fr)); gap: 8px; }
  .agent-theme-row { display: flex; align-items: center; gap: 8px; }
  .agent-theme-name { min-width: 80px; font-size: 13px; color: #d4d4d8; }
  .palette-toggle {
    align-self: flex-start; padding: 6px 12px; border-radius: 7px; font-size: 13px; cursor: pointer;
    border: 1px solid rgba(255,255,255,.12); background: rgba(255,255,255,.05); color: #d4d4d8;
  }
  .palette-toggle:hover { border-color: rgba(var(--accent-rgb), .5); color: var(--accent-pale); }
  .agent-theme-block {
    border: 1px solid rgba(255,255,255,.07); border-radius: 8px; padding: 6px 10px;
    background: rgba(0,0,0,.15);
  }
  .agent-theme-block summary {
    display: flex; align-items: center; justify-content: space-between; gap: 8px;
    cursor: pointer; font-size: 13px; color: #d4d4d8; list-style: none;
  }
  .agent-theme-block summary::-webkit-details-marker { display: none; }
  .agent-theme-block[open] summary { margin-bottom: 8px; }
  .agent-theme-current { color: #71717a; font-size: 12px; }
  .custom-list { display: flex; flex-direction: column; gap: 6px; }
  .custom-row { display: flex; align-items: center; gap: 6px; }
  .custom-row.editing .custom-name { border-color: rgba(var(--accent-rgb), .5); }
  .custom-name {
    flex: 1; min-width: 0; padding: 6px 9px; border-radius: 6px; font-size: 13px;
    border: 1px solid rgba(255,255,255,.12); background: rgba(0,0,0,.25); color: #e4e4e7;
  }
  .palette-delete {
    border: 1px solid rgba(255,255,255,.12); background: rgba(255,255,255,.04);
    color: #a1a1aa; border-radius: 6px; width: 28px; height: 28px; cursor: pointer; font-size: 15px;
  }
  .palette-delete:hover { color: #fb7185; border-color: rgba(251,113,133,.5); }
  .palette-add {
    align-self: flex-start; padding: 6px 12px; border-radius: 7px; font-size: 13px; cursor: pointer;
    border: 1px dashed rgba(var(--accent-rgb), .4); background: rgba(var(--accent-rgb), .08); color: var(--accent-lighter);
  }
  .palette-add:hover { background: rgba(var(--accent-rgb), .16); }

  .palette-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 8px; }
  .palette-swatch { display: flex; align-items: center; gap: 8px; font-size: 12px; color: #a1a1aa; }
  .palette-swatch input[type="color"] {
    width: 34px; height: 24px; padding: 0; border: 1px solid rgba(255,255,255,.15);
    border-radius: 5px; background: transparent; cursor: pointer;
  }

  .dialog-overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.7);
    /* no backdrop-filter — WebKit repaints the whole blurred region on any
       change beneath it (same reason it was removed from the header) */
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 50;
  }

  .dialog-content {
    background: linear-gradient(180deg, #1a1a2e 0%, #0f0f1a 100%);
    border: 1px solid rgba(var(--accent-rgb), 0.2);
    border-radius: 16px;
    box-shadow: 0 25px 50px rgba(0, 0, 0, 0.5), 0 0 100px rgba(var(--accent-rgb), 0.1);
    width: 100%;
    /* Wide enough that a setting's label and its control share a line
       comfortably; the 90vw keeps it sane on a small window. */
    max-width: min(760px, 90vw);
    /* A fixed height, not just a maximum: the tabs differ in content length,
       and sizing to each one made the whole dialog jump as you switched
       between them. The list inside scrolls instead. Capped in px as well so
       a tall screen doesn't stretch it into mostly empty space.
       Both figures grew with the app-wide type bump — at the old size the
       longer settings tabs scrolled where they used to fit. */
    height: min(88vh, 780px);
    max-height: 88vh;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .dialog-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 20px 24px;
    background: linear-gradient(180deg, rgba(var(--accent-rgb), 0.1) 0%, transparent 100%);
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    flex-shrink: 0;
  }

  .dialog-header h2 {
    font-size: 18px;
    font-weight: 600;
    margin: 0;
    background: linear-gradient(135deg, var(--accent-light) 0%, var(--accent) 100%);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }

  .close-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    background: rgba(255, 255, 255, 0.05);
    border: none;
    border-radius: 8px;
    color: #6b7280;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .close-btn:hover {
    background: rgba(255, 255, 255, 0.1);
    color: white;
  }

  .tabs {
    display: flex;
    gap: 4px;
    padding: 12px 24px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    flex-shrink: 0;
    /* Six tabs whose labels vary a lot by language — Hungarian's
       "Karbantartás" is twice the width of English's "Update". Rather than let
       them overflow the dialog, the strip scrolls; the scrollbar is hidden
       because a horizontal bar under the tabs reads as a divider. */
    overflow-x: auto;
    scrollbar-width: none;
  }

  .tabs::-webkit-scrollbar {
    display: none;
  }

  .tab {
    padding: 8px 16px;
    font-size: 13px;
    font-weight: 500;
    /* A scrolling strip must not compress or wrap its labels. */
    white-space: nowrap;
    flex-shrink: 0;
    color: #6b7280;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.2s ease;
  }

  .tab:hover {
    color: #9ca3af;
    background: rgba(255, 255, 255, 0.05);
  }

  .tab.active {
    color: var(--accent-light);
    background: rgba(var(--accent-rgb), 0.15);
    border-color: rgba(var(--accent-rgb), 0.3);
  }

  .settings-list {
    padding: 16px 24px;
    overflow-y: auto;
    flex: 1;
  }

  .settings-section {
    margin-bottom: 24px;
  }

  .settings-section:last-child {
    margin-bottom: 0;
  }

  .settings-section h3 {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
    margin: 0 0 8px 0;
  }

  .section-desc {
    font-size: 13px;
    color: #4b5563;
    margin: 0 0 12px 0;
  }

  .setting-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    /* Keeps the label and its control reading as one row now the dialog is
       wide: space-between alone would push them to opposite edges with a lake
       of empty space between, and the eye stops connecting the two. */
    gap: 24px;
    padding: 12px 0;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    cursor: pointer;
  }

  .setting-item:last-child {
    border-bottom: none;
  }

  .input-item {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
    cursor: default;
  }

  .hotkey-item {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
    cursor: default;
  }

  .setting-info {
    display: flex;
    flex-direction: column;
    gap: 2px;
    /* Takes the row's slack so a long description wraps into the space rather
       than the label and its control drifting to opposite edges. */
    flex: 1;
    min-width: 0;
  }

  .setting-label {
    font-size: 14px;
    font-weight: 500;
    color: #e4e4e7;
  }

  /* Marks a setting as not-yet-dependable, sitting beside its label rather
     than in the description so it is read before the setting is turned on.
     Amber, matching the DEV badge in the header — the app's existing signal
     for "this build is not the ordinary one". */
  .experimental-badge {
    display: inline-block;
    margin-left: 6px;
    padding: 1px 6px;
    border-radius: 4px;
    border: 1px solid rgba(255, 200, 0, 0.35);
    background: rgba(255, 200, 0, 0.12);
    color: #ffc800;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    vertical-align: middle;
  }

  .setting-desc {
    font-size: 13px;
    color: #6b7280;
  }

  /* One-off actions in the Maintenance section: these open a dialog rather
     than change a setting, so they read as buttons, not toggles. */
  .theme-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
    gap: 8px;
  }
  .theme-swatch {
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 7px 9px;
    border-radius: 8px;
    border: 1px solid rgba(255, 255, 255, 0.1);
    background: rgba(255, 255, 255, 0.03);
    color: #a1a1aa;
    font-size: 12px;
    cursor: pointer;
    transition: border-color 0.15s ease, color 0.15s ease;
  }
  .theme-swatch:hover { border-color: rgba(255, 255, 255, 0.25); color: #e4e4e7; }
  /* Bordered in its OWN colour, not the active accent: the selected swatch
     has to be identifiable before the theme is applied. */
  .theme-swatch.selected {
    border-color: var(--sw);
    background: color-mix(in srgb, var(--sw) 14%, transparent);
    color: #e4e4e7;
  }
  .theme-dot {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    flex-shrink: 0;
    background: linear-gradient(135deg, var(--sw), var(--sw-light));
  }
  /* The colour input covers the swatch so the whole tile is the target,
     rather than a separate small square beside the label. */
  .accent-warning {
    margin: 8px 0 0;
    font-size: 12px;
    color: #fbbf24;
  }

  .theme-swatch.custom { position: relative; }
  .theme-swatch.custom input[type="color"] {
    position: absolute;
    inset: 0;
    opacity: 0;
    cursor: pointer;
    border: 0;
    padding: 0;
  }
  .custom-dot {
    background: var(--sw);
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.25);
  }

  .theme-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  /* A quiet marker beside the label: useful when comparing against what the
     repository holds, but not something to lead with. */
  .patterns-version {
    margin-left: 6px;
    padding: 1px 5px;
    border-radius: 4px;
    background: rgba(255, 255, 255, 0.06);
    color: #9ca3af;
    font-size: 10px;
    font-weight: 500;
  }

  .action-btn {
    flex-shrink: 0;
    padding: 7px 14px;
    border-radius: 8px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: rgba(255, 255, 255, 0.05);
    color: #d4d4d8;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
    transition: border-color 0.15s ease, color 0.15s ease;
  }
  .action-btn:hover:not(:disabled) {
    border-color: rgba(var(--accent-rgb), 0.5);
    color: var(--accent-pale);
  }
  /* The first action button that can be disabled — while its request is in
     flight — so the muted state is defined here rather than left to the browser
     default, which greys only the label and keeps the hover effect. */
  .action-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .font-size-control {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-shrink: 0;
  }
  .font-size-control input[type="range"] {
    width: 150px;
    accent-color: var(--accent);
  }
  .font-size-value {
    min-width: 42px;
    text-align: right;
    font-family: 'JetBrains Mono', monospace;
    font-size: 13px;
    color: #a1a1aa;
  }

  /* Full width, like the bare input it replaced: .setting-input is width:100%,
     so it sizes to its parent — and an inline-flex wrapper shrinks to its
     contents, which took the field down with it. */
  /* Prominent rather than a note: this is why nothing gets typed, and the
     settings around it all look fine. */
  .dictation-problem {
    margin: 0 0 16px;
    padding: 12px 14px;
    border-radius: 8px;
    background: rgba(239, 68, 68, 0.12);
    border: 1px solid rgba(239, 68, 68, 0.3);
    color: #fca5a5;
    font-size: 13px;
  }
  .dictation-problem pre {
    margin: 8px 0 0;
    white-space: pre-wrap;
    font-family: inherit;
    font-size: 12px;
    opacity: 0.9;
  }

  .key-field {
    position: relative;
    display: flex;
    align-items: center;
    width: 100%;
  }
  .key-field .setting-input {
    padding-right: 34px;
  }
  .key-reveal {
    position: absolute;
    right: 6px;
    display: flex;
    align-items: center;
    padding: 2px;
    border: 0;
    background: transparent;
    color: #9ca3af;
    cursor: pointer;
  }
  .key-reveal:hover { color: #e4e4e7; }

  .setting-input {
    width: 100%;
    padding: 8px 12px;
    background: rgba(255, 255, 255, 0.05);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 8px;
    color: #e4e4e7;
    font-size: 13px;
    outline: none;
    transition: all 0.2s ease;
  }

  .setting-input:focus {
    border-color: rgba(var(--accent-rgb), 0.5);
    background: rgba(255, 255, 255, 0.08);
  }

  .setting-input::placeholder {
    color: #4b5563;
  }

  .number-input {
    width: 80px;
  }

  .hotkey-config {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .modifier-checkbox {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 13px;
    color: #9ca3af;
    cursor: pointer;
  }

  .modifier-checkbox input {
    accent-color: var(--accent);
  }

  .plus {
    color: #6b7280;
    font-weight: bold;
  }

  .hotkey-key {
    width: 36px;
    height: 36px;
    text-align: center;
    text-transform: uppercase;
    font-weight: 600;
    background: rgba(var(--accent-rgb), 0.2);
    border: 1px solid rgba(var(--accent-rgb), 0.3);
    border-radius: 8px;
    color: var(--accent-light);
    font-size: 14px;
    outline: none;
  }

  .hotkey-key:focus {
    border-color: var(--accent);
  }

  .loading {
    padding: 20px;
    text-align: center;
    color: #6b7280;
    font-size: 13px;
  }

  .slider-row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
  }

  .setting-slider {
    flex: 1;
    height: 6px;
    -webkit-appearance: none;
    appearance: none;
    background: rgba(255, 255, 255, 0.1);
    border-radius: 3px;
    outline: none;
  }

  .setting-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 16px;
    height: 16px;
    background: var(--accent);
    border-radius: 50%;
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .setting-slider::-webkit-slider-thumb:hover {
    background: var(--accent-light);
    transform: scale(1.1);
  }

  .slider-value {
    min-width: 40px;
    text-align: right;
    font-size: 13px;
    color: var(--accent-light);
    font-weight: 500;
  }

  .toggle-btn {
    background: none;
    border: none;
    cursor: pointer;
    padding: 0;
    flex-shrink: 0;
  }

  .toggle-track {
    display: block;
    width: 44px;
    height: 24px;
    background: rgba(255, 255, 255, 0.1);
    border-radius: 12px;
    position: relative;
    transition: background 0.2s ease;
  }

  .toggle-btn.active .toggle-track {
    background: rgba(var(--accent-rgb), 0.6);
  }

  .toggle-thumb {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 20px;
    height: 20px;
    background: #4b5563;
    border-radius: 50%;
    transition: all 0.2s ease;
  }

  .toggle-btn.active .toggle-thumb {
    left: 22px;
    background: var(--accent-light);
  }

  .commands-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 200px;
    overflow-y: auto;
    background: rgba(0, 0, 0, 0.2);
    border-radius: 8px;
    padding: 8px;
  }

  .command-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    background: rgba(255, 255, 255, 0.03);
    border-radius: 6px;
    font-size: 13px;
  }

  .command-word {
    color: var(--accent-light);
    font-weight: 500;
    min-width: 120px;
  }

  .command-arrow {
    color: #4b5563;
  }

  .command-value {
    color: #9ca3af;
    font-family: monospace;
  }

  .dialog-footer {
    padding: 12px 24px;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
    flex-shrink: 0;
  }

  .hint {
    font-size: 12px;
    color: #4b5563;
  }

  .audio-test-item {
    flex-direction: row;
    align-items: center;
  }

  .audio-test-btn {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 14px;
    background: rgba(var(--accent-rgb), 0.2);
    border: 1px solid rgba(var(--accent-rgb), 0.3);
    border-radius: 8px;
    color: var(--accent-light);
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s ease;
    min-width: 100px;
    justify-content: center;
  }

  .audio-test-btn:hover:not(:disabled) {
    background: rgba(var(--accent-rgb), 0.3);
    border-color: rgba(var(--accent-rgb), 0.5);
  }

  .audio-test-btn:disabled {
    cursor: not-allowed;
    opacity: 0.8;
  }

  .audio-test-btn.recording {
    background: rgba(239, 68, 68, 0.2);
    border-color: rgba(239, 68, 68, 0.4);
    color: #f87171;
  }

  .audio-test-btn.playing {
    background: rgba(34, 197, 94, 0.2);
    border-color: rgba(34, 197, 94, 0.4);
    color: #4ade80;
  }

  .recording-dot {
    width: 8px;
    height: 8px;
    background: #ef4444;
    border-radius: 50%;
    animation: pulse-recording 1s ease-in-out infinite;
  }

  @keyframes pulse-recording {
    0%, 100% {
      opacity: 1;
      transform: scale(1);
    }
    50% {
      opacity: 0.5;
      transform: scale(0.8);
    }
  }
</style>
