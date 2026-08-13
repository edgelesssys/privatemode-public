<script lang="ts">
  import { Copy, Check, X } from 'lucide-svelte';
  import { modalFocus } from '$lib/modalFocus';
  import type { AttachedFile } from '$lib/chatStore';
  import { t } from '$lib/i18n';

  export let file: AttachedFile;
  export let onClose: () => void;

  let copied = false;

  function copy() {
    navigator.clipboard.writeText(file.content).catch(() => {});
    copied = true;
    setTimeout(() => {
      copied = false;
    }, 2000);
  }
</script>

<svelte:window
  on:keydown={(e) => {
    if (e.key === 'Escape') onClose();
  }}
/>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="image-preview-overlay"
  role="dialog"
  aria-modal="true"
  aria-label={file.name}
  tabindex="-1"
  use:modalFocus
  onclick={onClose}
>
  <button
    class="image-preview-close"
    type="button"
    aria-label={$t('common.closePreview')}
    onclick={onClose}
  >
    <X size={24} />
  </button>

  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="transcription-preview-container"
    onclick={(e) => e.stopPropagation()}
  >
    <div class="transcription-preview-header">
      <h2>{file.name}</h2>
      <button
        class="transcription-copy-button"
        class:copied
        type="button"
        onclick={copy}
        aria-label={copied
          ? $t('chat.copiedTranscription')
          : $t('chat.copyTranscription')}
      >
        {#if copied}
          <Check size={16} />
        {:else}
          <Copy size={16} />
        {/if}
      </button>
    </div>
    <p>{file.content}</p>
  </div>
</div>

<style>
  .image-preview-overlay {
    position: fixed;
    inset: 0;
    background-color: rgba(0, 0, 0, 0.8);
    backdrop-filter: blur(4px);
    z-index: 10000;
    display: flex;
    align-items: center;
    justify-content: center;
    animation: overlay-fade-in 0.15s ease-out;
  }

  @keyframes overlay-fade-in {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }

  .image-preview-close {
    position: absolute;
    top: 1rem;
    right: 1rem;
    background: none;
    border: none;
    color: rgba(255, 255, 255, 0.8);
    cursor: pointer;
    padding: 0.5rem;
    border-radius: 0.5rem;
    display: flex;
    transition: color 0.2s;
  }

  .image-preview-close:hover {
    color: white;
  }

  .transcription-preview-container {
    width: min(720px, calc(100vw - 2rem));
    max-height: 80vh;
    overflow-y: auto;
    padding: 1.25rem;
    border-radius: 0.5rem;
    background-color: var(--color-bg-surface);
    color: var(--color-text-primary);
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.35);
  }

  .transcription-preview-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    margin-bottom: 1rem;
  }

  .transcription-preview-header h2 {
    margin: 0;
    font-size: 1rem;
    font-weight: 600;
  }

  .transcription-copy-button {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    padding: 0.375rem;
    border: none;
    border-radius: 0.375rem;
    background: none;
    color: var(--color-text-secondary);
    cursor: pointer;
    transition:
      background-color 0.2s,
      color 0.2s;
  }

  .transcription-copy-button:hover {
    background-color: var(--color-bg-hover);
    color: var(--color-text-primary);
  }

  .transcription-copy-button.copied {
    color: var(--color-accent-green-check);
  }

  .transcription-preview-container p {
    margin: 0;
    white-space: pre-wrap;
    line-height: 1.6;
    color: var(--color-text-secondary);
  }
</style>
