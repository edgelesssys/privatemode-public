<script lang="ts">
  import { FileUp } from 'lucide-svelte';
  import { onMount } from 'svelte';

  export let enabled = true;
  export let onDrop: (files: globalThis.File[]) => void;

  let isDragging = false;
  let dragCounter = 0;

  $: if (!enabled && isDragging) {
    isDragging = false;
    dragCounter = 0;
  }

  onMount(() => {
    const isFileDrag = (e: DragEvent) =>
      e.dataTransfer?.types?.includes('Files') ?? false;

    const handleEnter = (e: DragEvent) => {
      if (!enabled) return;
      if (isFileDrag(e)) {
        dragCounter++;
        isDragging = true;
      }
    };
    const handleLeave = () => {
      dragCounter--;
      if (dragCounter <= 0) {
        dragCounter = 0;
        isDragging = false;
      }
    };
    const handleOver = (e: DragEvent) => {
      if (isFileDrag(e)) e.preventDefault();
    };
    const handleWindowDrop = (e: DragEvent) => {
      if (isFileDrag(e)) e.preventDefault();
      dragCounter = 0;
      isDragging = false;
    };

    window.addEventListener('dragenter', handleEnter);
    window.addEventListener('dragleave', handleLeave);
    window.addEventListener('dragover', handleOver);
    window.addEventListener('drop', handleWindowDrop);

    return () => {
      window.removeEventListener('dragenter', handleEnter);
      window.removeEventListener('dragleave', handleLeave);
      window.removeEventListener('dragover', handleOver);
      window.removeEventListener('drop', handleWindowDrop);
    };
  });

  function handleOverlayDrop(event: DragEvent) {
    event.preventDefault();
    isDragging = false;
    if (!enabled) return;
    const files = event.dataTransfer?.files;
    if (!files || files.length === 0) return;
    onDrop(Array.from(files));
  }
</script>

{#if isDragging && enabled}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="fullpage-drop-overlay"
    on:dragover={(e) => e.preventDefault()}
    on:drop={handleOverlayDrop}
  >
    <div class="fullpage-drop-content">
      <FileUp size={48} />
      <span>Drop file to attach</span>
    </div>
  </div>
{/if}

<style>
  .fullpage-drop-overlay {
    position: fixed;
    inset: 0;
    background-color: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(6px);
    z-index: 9999;
    display: flex;
    align-items: center;
    justify-content: center;
    animation: overlay-fade-in 0.15s ease-out;
  }

  .fullpage-drop-content {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 1rem;
    color: rgba(255, 255, 255, 0.9);
    font-size: 1.125rem;
    font-weight: 500;
    letter-spacing: 0.02em;
    padding: 2.5rem 3.5rem;
    border-radius: 1rem;
    border: 2px dashed var(--color-accent);
    background-color: color-mix(in srgb, var(--color-accent) 12%, #1a1a1a);
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.05);
    pointer-events: none;
    animation: card-scale-in 0.2s ease-out;
  }

  .fullpage-drop-content span {
    opacity: 0.7;
  }

  @keyframes overlay-fade-in {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }

  @keyframes card-scale-in {
    from {
      opacity: 0;
      transform: scale(0.95);
    }
    to {
      opacity: 1;
      transform: scale(1);
    }
  }
</style>
