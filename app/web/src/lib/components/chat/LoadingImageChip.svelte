<div class="image-chip loading-image-chip">
  <div class="image-thumbnail-placeholder">
    <div class="loading-spinner"></div>
  </div>
  <span class="file-name">Loading...</span>
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

  .image-thumbnail-placeholder {
    width: 32px;
    height: 32px;
    border-radius: 0.25rem;
    background-color: var(--color-bg-active);
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .loading-spinner {
    width: 16px;
    height: 16px;
    border: 2px solid var(--color-border);
    border-top-color: var(--color-text-secondary);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  .file-name {
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
