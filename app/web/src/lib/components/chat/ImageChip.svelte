<script lang="ts">
  import { X } from 'lucide-svelte';
  import type { AttachedImage } from '$lib/chatStore';
  import Tooltip from '../common/Tooltip.svelte';

  export let image: AttachedImage;
  export let onPreview: ((image: AttachedImage) => void) | undefined =
    undefined;
  export let onRemove: (() => void) | undefined = undefined;
</script>

<div class="image-chip">
  {#if image.dataUrl && onPreview}
    <button
      class="image-thumbnail-button"
      type="button"
      aria-label={`Preview ${image.name}`}
      on:click={() => onPreview?.(image)}
    >
      <img
        src={image.dataUrl}
        alt={image.name}
        class="image-thumbnail"
      />
    </button>
  {:else if image.dataUrl}
    <img
      src={image.dataUrl}
      alt={image.name}
      class="image-thumbnail"
    />
  {:else}
    <div class="image-thumbnail placeholder"></div>
  {/if}
  <span class="file-name">{image.name}</span>
  {#if onRemove}
    <Tooltip text="Remove image">
      <button
        class="remove-file"
        on:click={onRemove}
        type="button"
        aria-label={`Remove ${image.name}`}
      >
        <X size={16} />
      </button>
    </Tooltip>
  {/if}
</div>

<style>
  .image-chip {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    padding: 0.375rem 0.5rem;
    background-color: var(--chip-bg, var(--color-bg-surface-muted));
    border-radius: 0.5rem;
    font-size: 0.875rem;
    color: var(--color-text-primary);
  }

  .image-thumbnail-button {
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    border-radius: 0.25rem;
    display: flex;
  }

  .image-thumbnail-button:hover .image-thumbnail {
    opacity: 0.8;
  }

  .image-thumbnail {
    width: 32px;
    height: 32px;
    object-fit: cover;
    border-radius: 0.25rem;
  }

  .image-thumbnail.placeholder {
    background-color: var(--color-bg-active);
    animation: placeholder-pulse 1.5s ease-in-out infinite;
  }

  @keyframes placeholder-pulse {
    0%,
    100% {
      opacity: 0.4;
    }
    50% {
      opacity: 0.8;
    }
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
