<script lang="ts">
  import { chatStore } from '#lib/chatStore.js';
  import { ArrowLeft, Trash2, Monitor, Sun, Moon } from 'lucide-svelte';
  import { goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { themePreference } from '#lib/themeStore.js';
  import type { ThemePreference } from '#lib/themeStore.js';
  import { localePreference, t, type LocalePreference } from '#lib/i18n.js';
  import {
    models as modelsStore,
    modelsError,
    modelsLoaded,
    modelsLoading,
  } from '#lib/clientStore.js';
  import {
    getAvailableTranscriptionModels,
    transcriptionModelConfig,
    transcriptionModel,
  } from '#lib/transcriptionStore.js';
  import type { TranscriptionModelID } from '#lib/transcriptionStore.js';
  import { onMount } from 'svelte';

  let showDeleteConfirm = $state(false);
  let currentTheme = $state<ThemePreference>('system');
  let currentLocalePreference = $state<LocalePreference>('system');
  let currentTranscriptionModel =
    $state<TranscriptionModelID>('whisper-large-v3');
  let availableTranscriptionModels = $derived(
    getAvailableTranscriptionModels($modelsStore),
  );

  onMount(() => {
    const unsubscribeTheme = themePreference.subscribe((v) => {
      currentTheme = v;
    });
    const unsubscribeLocale = localePreference.subscribe((v) => {
      currentLocalePreference = v;
    });
    const unsubscribeTranscriptionModel = transcriptionModel.subscribe((v) => {
      currentTranscriptionModel = v;
    });
    return () => {
      unsubscribeTheme();
      unsubscribeLocale();
      unsubscribeTranscriptionModel();
    };
  });

  function setTheme(value: ThemePreference) {
    themePreference.set(value);
  }

  function setLocalePreference(value: LocalePreference) {
    localePreference.set(value);
  }

  function setTranscriptionModel(value: TranscriptionModelID) {
    transcriptionModel.set(value);
  }

  const themeOptions: {
    value: ThemePreference;
    icon: typeof Monitor;
  }[] = [
    { value: 'system', icon: Monitor },
    { value: 'light', icon: Sun },
    { value: 'dark', icon: Moon },
  ];

  const languageOptions: LocalePreference[] = ['system', 'en', 'de'];

  function themeLabel(value: ThemePreference): string {
    return $t(
      value === 'system'
        ? 'settings.themeSystem'
        : value === 'light'
          ? 'settings.themeLight'
          : 'settings.themeDark',
    );
  }

  function languageLabel(value: LocalePreference): string {
    return $t(
      value === 'system'
        ? 'settings.languageSystem'
        : value === 'en'
          ? 'settings.languageEnglish'
          : 'settings.languageGerman',
    );
  }

  function handleDeleteAllChats() {
    chatStore.clear();
    showDeleteConfirm = false;
    goto(resolve('/'));
  }

  function handleBack() {
    goto(resolve('/'));
  }
</script>

<div class="settings-container">
  <div class="settings-header">
    <button
      onclick={handleBack}
      class="back-btn"
    >
      <ArrowLeft size={20} />
      {$t('common.back')}
    </button>
    <h1>{$t('settings.title')}</h1>
  </div>

  <div class="settings-content">
    <section class="setting-section">
      <h2>{$t('settings.appearance')}</h2>
      <p class="setting-description">
        {$t('settings.appearanceDescription')}
      </p>
      <div class="theme-picker">
        {#each themeOptions as option (option.value)}
          <button
            class="theme-option"
            class:active={currentTheme === option.value}
            aria-pressed={currentTheme === option.value}
            onclick={() => setTheme(option.value)}
          >
            <option.icon size={18} />
            {themeLabel(option.value)}
          </button>
        {/each}
      </div>
    </section>

    <section class="setting-section">
      <h2>{$t('settings.language')}</h2>
      <p class="setting-description">
        {$t('settings.languageDescription')}
      </p>
      <div class="theme-picker">
        {#each languageOptions as option (option)}
          <button
            class="theme-option"
            class:active={currentLocalePreference === option}
            aria-pressed={currentLocalePreference === option}
            onclick={() => setLocalePreference(option)}
          >
            {languageLabel(option)}
          </button>
        {/each}
      </div>
    </section>

    <section class="setting-section">
      <h2>{$t('settings.audioTranscription')}</h2>
      <p class="setting-description">
        {$t('settings.audioDescription')}
      </p>
      {#if !$modelsLoaded && $modelsLoading}
        <p class="empty-model-state">
          {$t('settings.transcriptionLoading')}
        </p>
      {:else if !$modelsLoaded && $modelsError}
        <p class="empty-model-state">
          {$t('settings.transcriptionLoadFailed', { error: $modelsError })}
        </p>
      {:else if availableTranscriptionModels.length === 0}
        <p class="empty-model-state">
          {$t('settings.noTranscriptionModel')}
        </p>
      {:else}
        <div class="transcription-model-picker">
          {#each availableTranscriptionModels as model (model.id)}
            <button
              class="transcription-model-option"
              class:active={currentTranscriptionModel === model.id}
              aria-pressed={currentTranscriptionModel === model.id}
              onclick={() => setTranscriptionModel(model.id)}
            >
              <span class="model-label"
                >{transcriptionModelConfig[model.id].displayName}</span
              >
              <span class="model-description"
                >{transcriptionModelConfig[model.id].description}</span
              >
            </button>
          {/each}
        </div>
      {/if}
    </section>

    <section class="setting-section danger-section">
      <h2>{$t('settings.dangerZone')}</h2>
      <p class="setting-description">{$t('settings.dangerDescription')}</p>

      {#if !showDeleteConfirm}
        <button
          onclick={() => (showDeleteConfirm = true)}
          class="danger-btn"
        >
          <Trash2 size={20} />
          {$t('settings.deleteAllChats')}
        </button>
      {:else}
        <div class="confirm-delete">
          <p class="confirm-text">
            {$t('settings.deleteAllConfirm')}
          </p>
          <div class="confirm-actions">
            <button
              onclick={handleDeleteAllChats}
              class="confirm-danger-btn"
            >
              {$t('settings.yesDeleteAll')}
            </button>
            <button
              onclick={() => (showDeleteConfirm = false)}
              class="cancel-btn"
            >
              {$t('common.cancel')}
            </button>
          </div>
        </div>
      {/if}
    </section>
  </div>
</div>

<style>
  .settings-container {
    margin: 0 auto;
    max-width: 800px;
    padding: 20px;
  }

  .settings-header {
    margin-bottom: 40px;
  }

  .back-btn {
    display: flex;
    align-items: center;
    gap: 8px;
    background: none;
    border: none;
    color: var(--color-text-muted);
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    padding: 8px 0;
    margin-bottom: 16px;
    font-family: 'Inter Variable', sans-serif;
    transition: color 0.2s;
  }

  .back-btn:hover {
    color: var(--color-text-heading);
  }

  .settings-header h1 {
    font-size: 32px;
    font-weight: 600;
    color: var(--color-text-heading);
    margin: 0;
  }

  .settings-content {
    display: flex;
    flex-direction: column;
    gap: 32px;
  }

  .setting-section {
    background: var(--color-bg-surface);
    border-radius: 12px;
    padding: 24px;
    box-shadow: var(--shadow-sm);
  }

  .setting-section h2 {
    font-size: 20px;
    font-weight: 600;
    color: var(--color-text-heading);
    margin: 0 0 8px 0;
  }

  .setting-description {
    color: var(--color-text-muted);
    font-size: 14px;
    margin: 0 0 20px 0;
  }

  .theme-picker {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
    gap: 8px;
  }

  .theme-option {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 10px 20px;
    width: 100%;
    min-width: 0;
    background: var(--color-bg-surface);
    color: var(--color-text-muted);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s;
    font-family: 'Inter Variable', sans-serif;
  }

  .theme-option:hover {
    background: var(--color-bg-hover);
    border-color: var(--color-border-secondary);
  }

  .theme-option.active {
    background: var(--color-bg-active);
    border-color: var(--color-accent);
    color: var(--color-accent);
  }

  .transcription-model-picker {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 8px;
  }

  .transcription-model-option {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    padding: 12px;
    min-width: 0;
    background: var(--color-bg-surface);
    color: var(--color-text-muted);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.2s;
    font-family: 'Inter Variable', sans-serif;
    text-align: left;
  }

  .transcription-model-option:hover {
    background: var(--color-bg-hover);
    border-color: var(--color-border-secondary);
  }

  .transcription-model-option.active {
    background: var(--color-bg-active);
    border-color: var(--color-accent);
  }

  .model-label {
    color: var(--color-text-heading);
    font-size: 14px;
    font-weight: 600;
  }

  .transcription-model-option.active .model-label {
    color: var(--color-accent);
  }

  .model-description {
    color: var(--color-text-muted);
    font-size: 13px;
    line-height: 1.4;
  }

  .empty-model-state {
    margin: 0;
    color: var(--color-text-muted);
    font-size: 14px;
  }

  .danger-section {
    border: 1px solid var(--color-danger-border);
  }

  .danger-btn {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px 24px;
    background: var(--color-bg-surface);
    color: var(--color-danger);
    border: 1px solid var(--color-danger);
    border-radius: 8px;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s;
    font-family: 'Inter Variable', sans-serif;
  }

  .danger-btn:hover {
    background: var(--color-danger-bg);
  }

  .confirm-delete {
    background: var(--color-danger-bg);
    padding: 16px;
    border-radius: 8px;
    border: 1px solid var(--color-danger-border);
  }

  .confirm-text {
    margin: 0 0 16px 0;
    color: var(--color-danger);
    font-weight: 500;
    font-size: 14px;
  }

  .confirm-actions {
    display: flex;
    gap: 12px;
  }

  .confirm-danger-btn {
    padding: 10px 20px;
    background: var(--color-danger);
    color: var(--color-text-inverse);
    border: none;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    transition: background-color 0.2s;
    font-family: 'Inter Variable', sans-serif;
  }

  .confirm-danger-btn:hover {
    background: var(--color-danger-dark);
  }

  .cancel-btn {
    padding: 10px 20px;
    background: var(--color-bg-surface);
    color: var(--color-text-muted);
    border: 1px solid var(--color-border-light);
    border-radius: 8px;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s;
    font-family: 'Inter Variable', sans-serif;
  }

  .cancel-btn:hover {
    background: var(--color-bg-surface-tertiary);
  }

  @media (max-width: 768px) {
    .back-btn {
      display: none;
    }
  }

  @media (max-width: 480px) {
    .theme-picker {
      grid-template-columns: 1fr;
    }

    .theme-option {
      padding: 10px 16px;
    }
  }
</style>
