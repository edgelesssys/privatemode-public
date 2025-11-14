<script lang="ts">
  import { onMount } from 'svelte';
  import Icon from '@iconify/svelte';
  import type { Model } from '$lib/privatemodeClient';
  import { PrivatemodeClient } from '$lib/privatemodeClient';
  import { modelConfig, DEFAULT_MODEL_ID } from '$lib/models';
  import { models as modelsStore, modelsLoaded } from '$lib/proxyStore';

  export let proxyPort: string;
  export let selectedModel: string = '';
  export let onModelChange: (modelId: string) => void = () => {};

  const SELECTED_MODEL_KEY = 'privatemode_selected_model';

  let models: Model[] = [];
  let isOpen = false;
  let loading = false;
  let error: string | null = null;
  let pickerElement: HTMLDivElement;

  onMount(() => {
    const savedModel = localStorage.getItem(SELECTED_MODEL_KEY);

    if (savedModel) {
      selectedModel = savedModel;
      onModelChange(savedModel);
    }

    const handleOnline = () => {
      if (proxyPort && error) {
        loadModels();
      }
    };

    document.addEventListener('click', handleClickOutside);
    window.addEventListener('online', handleOnline);

    return () => {
      document.removeEventListener('click', handleClickOutside);
      window.removeEventListener('online', handleOnline);
    };
  });

  $: if (proxyPort) {
    loadModels();
  }

  function handleClickOutside(event: MouseEvent) {
    if (pickerElement && !pickerElement.contains(event.target as Node)) {
      isOpen = false;
    }
  }

  async function loadModels() {
    try {
      loading = true;
      error = null;
      const client = new PrivatemodeClient(proxyPort);
      models = await client.fetchModels();
      modelsStore.set(models);
      modelsLoaded.set(true);

      const filteredModels = Object.keys(modelConfig)
        .map((id) => models.find((m) => m.id === id))
        .filter((m): m is Model => m !== undefined);

      if (!selectedModel && filteredModels.length > 0) {
        selectedModel = filteredModels.find((m) => m.id === DEFAULT_MODEL_ID)
          ? DEFAULT_MODEL_ID
          : filteredModels[0].id;

        onModelChange(selectedModel);
        localStorage.setItem(SELECTED_MODEL_KEY, selectedModel);
      }
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load models';
      console.error('Error loading models:', e);
      modelsLoaded.set(false);
    } finally {
      loading = false;
    }
  }

  function togglePicker() {
    isOpen = !isOpen;
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
    return modelConfig[modelId]?.subtitle;
  }

  $: filteredModels = Object.keys(modelConfig)
    .map((id) => models.find((m) => m.id === id))
    .filter((m): m is Model => m !== undefined);
</script>

<div
  class="model-picker"
  bind:this={pickerElement}
>
  <button
    class="model-button"
    type="button"
    on:click={togglePicker}
  >
    <span class="model-name"
      >{selectedModel
        ? getModelDisplayName(selectedModel)
        : 'Select model'}</span
    >
    {#if isOpen}
      <Icon
        icon="material-symbols:arrow-drop-up"
        width="20"
        height="20"
      />
    {:else}
      <Icon
        icon="material-symbols:arrow-drop-down"
        width="20"
        height="20"
      />
    {/if}
  </button>

  {#if isOpen}
    <div class="model-dropdown">
      {#if loading}
        <div class="loading">Loading models...</div>
      {:else if error}
        <div class="error">{error}</div>
      {:else if filteredModels.length === 0}
        <div class="empty">No models available</div>
      {:else}
        {#each filteredModels as model}
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
    color: #374151;
    transition: color 0.2s;
    font-size: 0.875rem;
    padding: 0.25rem 0.5rem;
    border-radius: 0.375rem;
    background-color: #f3f4f6;
    border: 1px solid #e5e7eb;
  }

  .model-button:hover {
    background-color: #e5e7eb;
    border-color: #d1d5db;
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
    background: #232323;
    border-radius: 0.5rem;
    box-shadow: 0 0 12px rgba(0, 0, 0, 0.4);
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
    color: white;
    transition: background-color 0.2s;
    border-radius: 0.3rem;
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 1rem;
  }

  .model-option:hover {
    background-color: #535353;
  }

  .model-option.selected {
    border: #535353 1px solid;
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
    color: #9ca3af;
  }

  .loading,
  .error,
  .empty {
    padding: 0.75rem 1rem;
    font-size: 0.875rem;
    color: #9ca3af;
    text-align: center;
  }

  .error {
    color: #ef4444;
  }
</style>
