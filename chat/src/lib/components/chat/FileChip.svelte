<script lang="ts">
  import { File, Mic, X } from 'lucide-svelte';
  import type { AttachedFile } from '$lib/chatStore';
  import Tooltip from '../common/Tooltip.svelte';
  import { t } from '$lib/i18n';

  export let file: AttachedFile;
  export let onPreview: ((file: AttachedFile) => void) | undefined = undefined;
  export let onRemove: (() => void) | undefined = undefined;
</script>

<div class="file-chip">
  {#if file.kind === 'audio-transcription' && onPreview}
    <button
      class="file-preview-button"
      type="button"
      aria-label={$t('chat.previewTranscription', { name: file.name })}
      on:click={() => onPreview?.(file)}
    >
      <Mic size={16} />
      <span class="file-name">{file.name}</span>
    </button>
  {:else if file.kind === 'audio-transcription'}
    <Mic size={16} />
    <span class="file-name">{file.name}</span>
  {:else}
    <File size={16} />
    <span class="file-name">{file.name}</span>
  {/if}
  {#if onRemove}
    <Tooltip text={$t('chat.removeFile')}>
      <button
        class="remove-file"
        on:click={onRemove}
        type="button"
        aria-label={$t('chat.removeNamed', { name: file.name })}
      >
        <X size={16} />
      </button>
    </Tooltip>
  {/if}
</div>

<style>
  .file-chip {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    padding: 0.375rem 0.5rem;
    background-color: var(--chip-bg, var(--color-bg-surface-muted));
    border-radius: 0.5rem;
    font-size: 0.875rem;
    color: var(--color-text-primary);
  }

  .file-preview-button {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    padding: 0;
    border: none;
    background: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }

  .file-preview-button:hover {
    color: var(--color-accent);
  }

  .file-name {
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .remove-file {
    background: none;
    border: none;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--color-text-secondary);
    transition: color 0.2s;
    padding: 0;
  }

  .remove-file:hover {
    color: var(--color-danger-text);
  }
</style>
