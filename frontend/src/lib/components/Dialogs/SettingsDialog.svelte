<script lang="ts">
  import { autoFocusDialog } from '../../utils/dialogActions';
  import { createEventDispatcher } from 'svelte';
  import { settings, saveSettings } from '../../stores/settings';
  import * as DictationService from '../../../../wailsjs/go/main/DictationService';
  import type { main } from '../../../../wailsjs/go/models';
  import { EventsEmit } from '../../../../wailsjs/runtime/runtime';
  import Select from '../common/Select.svelte';
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

  async function changeLanguage(lang: string) {
    await loadTranslations(lang);
    saveSettings({ language: lang });
  }

  const rendererOptions = [
    { value: 'canvas', label: 'Canvas (ajánlott)' },
    { value: 'webgl', label: 'WebGL (leggyorsabb, kísérleti)' },
    { value: 'dom', label: 'DOM (legkompatibilisebb)' },
  ];

  function changeRenderer(r: string) {
    saveSettings({ terminalRenderer: r as 'canvas' | 'webgl' | 'dom' });
  }

  const dispatch = createEventDispatcher();

  // Tab state
  let activeTab: 'general' | 'dictation' = 'general';

  // Dictation settings state
  let dictationSettings: main.DictationSettings | null = null;
  let languages: Array<{code: string, name: string}> = [];
  let inputDevices: Array<{name: string, description: string, isDefault: boolean}> = [];
  let loading = true;
  let audioTestStatus: 'idle' | 'recording' | 'playing' | 'done' | 'error' = 'idle';
  let audioTestMessage = '';

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
  $: if (show && dictationSettings === null) {
    loadDictationSettings();
  }

  // Update commands when language changes
  $: if (dictationSettings?.language) {
    loadCommandsForLanguage(dictationSettings.language);
  }

  async function loadDictationSettings() {
    loading = true;
    try {
      const [settings, langs, devices] = await Promise.all([
        DictationService.GetDictationSettings(),
        DictationService.GetAvailableLanguages(),
        DictationService.GetInputDevices()
      ]);
      dictationSettings = settings;
      languages = langs.map((l: Record<string, string>) => ({ code: l.code, name: l.name }));
      inputDevices = devices || [];
    } catch (e) {
      console.error('Failed to load dictation settings:', e);
    }
    loading = false;
  }

  function loadCommandsForLanguage(lang: string) {
    punctuationCommands = defaultPunctuationCommands[lang] || defaultPunctuationCommands['en'] || {};
    deleteCommands = defaultDeleteCommands[lang] || defaultDeleteCommands['en'] || {};
  }

  async function runAudioTest() {
    if (audioTestStatus !== 'idle' && audioTestStatus !== 'done' && audioTestStatus !== 'error') return;

    audioTestStatus = 'recording';
    audioTestMessage = $t('settings.audioTestRecordingStart');

    try {
      // Countdown
      for (let i = 5; i > 0; i--) {
        audioTestMessage = $t('settings.audioTestRecording', { seconds: i });
        await new Promise(r => setTimeout(r, 1000));
      }

      audioTestStatus = 'playing';
      audioTestMessage = $t('settings.audioTestPlaying');

      await DictationService.AudioTest();

      audioTestStatus = 'done';
      audioTestMessage = $t('settings.audioTestDone');

      // Reset after 3 seconds
      setTimeout(() => {
        audioTestStatus = 'idle';
        audioTestMessage = '';
      }, 3000);
    } catch (e) {
      audioTestStatus = 'error';
      audioTestMessage = $t('settings.audioTestError', { error: String(e) });

      setTimeout(() => {
        audioTestStatus = 'idle';
        audioTestMessage = '';
      }, 5000);
    }
  }

  async function saveDictationSettings() {
    if (!dictationSettings) return;
    try {
      await DictationService.SetDictationSettings(JSON.stringify(dictationSettings));
    } catch (e) {
      console.error('Failed to save dictation settings:', e);
    }
  }

  function updateDictation<K extends keyof main.DictationSettings>(key: K, value: main.DictationSettings[K]) {
    if (!dictationSettings) return;
    (dictationSettings as any)[key] = value;
    dictationSettings = dictationSettings; // trigger reactivity
    saveDictationSettings();

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

  function toggle(key: 'hideStatusLines' | 'showAgentIcons' | 'compactList') {
    saveSettings({ [key]: !$settings[key] });
  }

  function formatCommandValue(value: string): string {
    if (value === '\n') return '[new line]';
    if (value === '\n\n') return '[new paragraph]';
    if (value === ' - ') return '[ - ]';
    return value;
  }

  function formatDeleteAction(action: string): string {
    switch (action) {
      case 'buffer': return $t('settings.deleteAction.buffer');
      case 'ctrl_backspace': return $t('settings.deleteAction.ctrlBackspace');
      case 'ctrl_alt_backspace': return $t('settings.deleteAction.ctrlAltBackspace');
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
          class:active={activeTab === 'dictation'}
          on:click={() => activeTab = 'dictation'}
        >
          {$t('settings.dictation')}
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

            <div class="setting-item input-item">
              <span class="setting-info">
                <span class="setting-label">Terminál renderer</span>
                <span class="setting-desc">Új terminálokra érvényes. Canvas = ajánlott; WebGL = leggyorsabb, de egyes gépeken hibás lehet; DOM = legkompatibilisebb, de lassabb.</span>
              </span>
              <Select
                value={$settings.terminalRenderer || 'canvas'}
                options={rendererOptions}
                on:change={(e) => changeRenderer(e.detail)}
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
          </div>
        {/if}

        <!-- Dictation Tab -->
        {#if activeTab === 'dictation'}
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
                    <input
                      type="password"
                      class="setting-input"
                      value={dictationSettings.googleApiKey}
                      placeholder={$t('settings.googleApiKeyPlaceholder')}
                      on:change={(e) => updateDictation('googleApiKey', e.currentTarget.value)}
                    />
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
                      <span class="command-value">{formatDeleteAction(action)}</span>
                    </div>
                  {/each}
                </div>
              </div>
            {/if}
          {/if}
        {/if}
      </div>

      <div class="dialog-footer">
        <span class="hint">{$t('settings.savedAutomatically')}</span>
      </div>
    </div>
  </div>
{/if}

<style>
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
    border: 1px solid rgba(139, 92, 246, 0.2);
    border-radius: 16px;
    box-shadow: 0 25px 50px rgba(0, 0, 0, 0.5), 0 0 100px rgba(139, 92, 246, 0.1);
    width: 100%;
    max-width: 520px;
    max-height: 85vh;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .dialog-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 20px 24px;
    background: linear-gradient(180deg, rgba(139, 92, 246, 0.1) 0%, transparent 100%);
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    flex-shrink: 0;
  }

  .dialog-header h2 {
    font-size: 18px;
    font-weight: 600;
    margin: 0;
    background: linear-gradient(135deg, #a78bfa 0%, #818cf8 100%);
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
  }

  .tab {
    padding: 8px 16px;
    font-size: 13px;
    font-weight: 500;
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
    color: #a78bfa;
    background: rgba(139, 92, 246, 0.15);
    border-color: rgba(139, 92, 246, 0.3);
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
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: #6b7280;
    margin: 0 0 8px 0;
  }

  .section-desc {
    font-size: 12px;
    color: #4b5563;
    margin: 0 0 12px 0;
  }

  .setting-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
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
  }

  .setting-label {
    font-size: 14px;
    font-weight: 500;
    color: #e4e4e7;
  }

  .setting-desc {
    font-size: 12px;
    color: #6b7280;
  }

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
    border-color: rgba(139, 92, 246, 0.5);
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
    font-size: 12px;
    color: #9ca3af;
    cursor: pointer;
  }

  .modifier-checkbox input {
    accent-color: #8b5cf6;
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
    background: rgba(139, 92, 246, 0.2);
    border: 1px solid rgba(139, 92, 246, 0.3);
    border-radius: 8px;
    color: #a78bfa;
    font-size: 14px;
    outline: none;
  }

  .hotkey-key:focus {
    border-color: #8b5cf6;
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
    background: #8b5cf6;
    border-radius: 50%;
    cursor: pointer;
    transition: all 0.15s ease;
  }

  .setting-slider::-webkit-slider-thumb:hover {
    background: #a78bfa;
    transform: scale(1.1);
  }

  .slider-value {
    min-width: 40px;
    text-align: right;
    font-size: 13px;
    color: #a78bfa;
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
    background: rgba(139, 92, 246, 0.6);
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
    background: #a78bfa;
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
    font-size: 12px;
  }

  .command-word {
    color: #a78bfa;
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
    font-size: 11px;
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
    background: rgba(139, 92, 246, 0.2);
    border: 1px solid rgba(139, 92, 246, 0.3);
    border-radius: 8px;
    color: #a78bfa;
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s ease;
    min-width: 100px;
    justify-content: center;
  }

  .audio-test-btn:hover:not(:disabled) {
    background: rgba(139, 92, 246, 0.3);
    border-color: rgba(139, 92, 246, 0.5);
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
