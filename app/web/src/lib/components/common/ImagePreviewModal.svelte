<script lang="ts">
  import { X } from 'lucide-svelte';
  import type { AttachedImage } from '$lib/chatStore';

  export let image: AttachedImage;
  export let onClose: () => void;
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
  onclick={onClose}
>
  <button
    class="image-preview-close"
    type="button"
    onclick={onClose}
  >
    <X size={24} />
  </button>

  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="image-preview-container"
    onclick={(e) => e.stopPropagation()}
  >
    <img
      src={image.dataUrl}
      alt={image.name}
      class="image-preview-full"
    />
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

  .image-preview-full {
    max-width: 90vw;
    max-height: 90vh;
    object-fit: contain;
    border-radius: 0.5rem;
  }
</style>
