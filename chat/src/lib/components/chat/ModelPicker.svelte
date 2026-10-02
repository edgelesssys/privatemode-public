<script lang="ts">
  import { onMount } from 'svelte';
  import { ChevronUp, ChevronDown } from 'lucide-svelte';
  import Tooltip from '../common/Tooltip.svelte';
  import type { Model, PrivatemodeClient } from '$lib/clientStore';
  import { modelConfig, DEFAULT_MODEL_ID } from '$lib/models';
  import {
    loadModels as loadModelsFromClient,
    models as modelsStore,
  } from '$lib/clientStore';
  import { t } from '$lib/i18n';

  export let privatemodeAIClient: PrivatemodeClient | null = null;
  export let selectedModel: string = '';
  export let disabled: boolean = false;
  export let lockedModel: string | undefined = undefined;
  export let onModelChange: (modelId: string) => void = () => {};

  const SELECTED_MODEL_KEY = 'privatemode_selected_model';

  let isOpen = false;
  let loading = false;
  let error: string | null = null;
  let loadedClient: PrivatemodeClient | null = null;
  let pickerElement: HTMLDivElement;
  let dropdownElement: HTMLDivElement;
  let buttonElement: HTMLButtonElement;

  onMount(() => {
    const savedModel = localStorage.getItem(SELECTED_MODEL_KEY);

    // A retired model has no config left, so drop the selection and let the default apply.
    if (savedModel && Object.hasOwn(modelConfig, savedModel)) {
      selectedModel = savedModel;
      onModelChange(savedModel);
    } else if (savedModel) {
      localStorage.removeItem(SELECTED_MODEL_KEY);
    }

    const handleOnline = () => {
      if (privatemodeAIClient && error) {
        loadModels();
      }
    };

    document.addEventListener('click', handleClickOutside);
    document.addEventListener('keydown', handleKeydown);
    window.addEventListener('online', handleOnline);

    return () => {
      document.removeEventListener('click', handleClickOutside);
      document.removeEventListener('keydown', handleKeydown);
      window.removeEventListener('online', handleOnline);
    };
  });

  $: if (privatemodeAIClient && privatemodeAIClient !== loadedClient) {
    // eslint-disable-next-line no-useless-assignment -- read on the next run of this reactive block
    loadedClient = privatemodeAIClient;
    loadModels();
  }

  function handleClickOutside(event: MouseEvent) {
    if (pickerElement && !pickerElement.contains(event.target as Node)) {
      isOpen = false;
    }
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && isOpen) {
      isOpen = false;
      buttonElement?.focus();
    }
  }

  async function loadModels() {
    if (!privatemodeAIClient) return;
    try {
      loading = true;
      error = null;
      const loadedModels = await loadModelsFromClient(privatemodeAIClient);

      const filteredModels = Object.keys(modelConfig)
        .map((id) => loadedModels.find((m) => m.id === id))
        .filter((m): m is Model => m !== undefined);

      if (!selectedModel && filteredModels.length > 0) {
        selectedModel = filteredModels.find((m) => m.id === DEFAULT_MODEL_ID)
          ? DEFAULT_MODEL_ID
          : filteredModels[0].id;

        onModelChange(selectedModel);
        localStorage.setItem(SELECTED_MODEL_KEY, selectedModel);
      }
    } catch (e) {
      error = e instanceof Error ? e.message : $t('model.loadFailed');
      console.error('Error loading models:', e);
    } finally {
      loading = false;
    }
  }

  function togglePicker() {
    if (disabled) return;
    isOpen = !isOpen;
    if (isOpen) {
      requestAnimationFrame(() => {
        if (dropdownElement && pickerElement && window.innerWidth <= 768) {
          const pickerRect = pickerElement.getBoundingClientRect();
          const bottomOffset = window.innerHeight - pickerRect.top + 8;
          dropdownElement.style.bottom = `${bottomOffset}px`;
        }
      });
    }
  }

  function selectModel(modelId: string) {
    selectedModel = modelId;
    isOpen = false;
    localStorage.setItem(SELECTED_MODEL_KEY, modelId);
    onModelChange(modelId);
  }

  function getModelDisplayName(modelId: string): string {
    return modelConfig[modelId]?.displayName || modelId;
  }

  function getModelSubtitle(modelId: string): string | undefined {
    if (modelId === 'openai/gpt-oss-120b') return $t('model.gptOssSubtitle');
    if (modelId === 'glm-latest') return $t('model.glmSubtitle');
    if (modelId === 'glm-flash-latest') return $t('model.glmFlashSubtitle');
    return modelConfig[modelId]?.subtitle;
  }

  $: filteredModels = Object.keys(modelConfig)
    .map((id) => $modelsStore.find((m) => m.id === id))
    .filter((m): m is Model => m !== undefined);
</script>

<div
  class="model-picker"
  bind:this={pickerElement}
>
  {#if disabled}
    <Tooltip text={$t('model.locked')}>
      <button
        class="model-button disabled"
        type="button"
      >
        <span class="model-name"
          >{getModelDisplayName(lockedModel ?? selectedModel)}</span
        >
        <ChevronDown size={16} />
      </button>
    </Tooltip>
  {:else}
    <button
      class="model-button"
      type="button"
      aria-expanded={isOpen}
      bind:this={buttonElement}
      on:click={togglePicker}
    >
      <span class="model-name"
        >{selectedModel
          ? getModelDisplayName(selectedModel)
          : $t('model.select')}</span
      >
      {#if isOpen}
        <ChevronUp size={16} />
      {:else}
        <ChevronDown size={16} />
      {/if}
    </button>
  {/if}

  {#if isOpen}
    <div
      class="model-dropdown"
      bind:this={dropdownElement}
    >
      {#if loading}
        <div
          class="loading"
          role="status"
        >
          {$t('model.loading')}
        </div>
      {:else if error}
        <div class="error">{error}</div>
      {:else if filteredModels.length === 0}
        <div class="empty">{$t('model.none')}</div>
      {:else}
        {#each filteredModels as model (model.id)}
          <button
            class="model-option"
            class:selected={model.id === selectedModel}
            type="button"
            on:click={() => selectModel(model.id)}
          >
            <div class="model-option-content">
              <div class="model-name-display">
                {getModelDisplayName(model.id)}
              </div>
              {#if getModelSubtitle(model.id)}
                <div class="model-subtitle">{getModelSubtitle(model.id)}</div>
              {/if}
            </div>
          </button>
        {/each}
      {/if}
    </div>
  {/if}
</div>

<style>
  .model-picker {
    position: relative;
  }

  .model-button {
    background: none;
    border: none;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 0.25rem;
    color: var(--color-text-secondary);
    transition: color 0.2s;
    font-size: 0.875rem;
    padding: 0.5rem;
    border-radius: 0.375rem;
  }

  .model-button:hover:not(.disabled) {
    background-color: var(--color-bg-hover);
  }

  .model-button.disabled {
    opacity: 0.5;
    cursor: default;
  }

  .model-button :global(svg) {
    pointer-events: none;
  }

  .model-name {
    max-width: 150px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .model-dropdown {
    position: absolute;
    bottom: 100%;
    right: 0;
    margin-bottom: 0.5rem;
    background: var(--color-bg-tooltip);
    border-radius: 0.5rem;
    box-shadow: var(--shadow-md);
    min-width: 20rem;
    max-height: 300px;
    overflow-y: auto;
    z-index: 10;
    padding: 0.5rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .model-option {
    width: 100%;
    background: none;
    border: none;
    cursor: pointer;
    padding: 0.75rem 1rem;
    text-align: left;
    color: var(--color-dropdown-text);
    transition: background-color 0.2s;
    border-radius: 0.3rem;
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 1rem;
  }

  .model-option:hover {
    background-color: var(--color-dropdown-hover);
  }

  .model-option.selected {
    border: var(--color-dropdown-border) 1px solid;
  }

  .model-option-content {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    flex: 1;
  }

  .model-name-display {
    font-size: 0.875rem;
    font-weight: 500;
  }

  .model-subtitle {
    font-size: 0.75rem;
    color: var(--color-dropdown-muted);
  }

  .loading,
  .error,
  .empty {
    padding: 0.75rem 1rem;
    font-size: 0.875rem;
    color: var(--color-dropdown-muted);
    text-align: center;
  }

  .error {
    color: var(--color-error);
  }

  @media (max-width: 768px) {
    .model-dropdown {
      min-width: 0;
      position: fixed;
      right: 1rem;
      left: 1rem;
      width: auto;
    }
  }
</style>
