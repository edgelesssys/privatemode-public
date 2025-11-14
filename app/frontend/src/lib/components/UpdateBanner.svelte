<script lang="ts">
  import { onMount } from 'svelte';
  import Icon from '@iconify/svelte';
  import { checkForUpdates, type UpdateInfo } from '$lib/updateChecker';

  let updateInfo: UpdateInfo | null = null;
  let dismissed = false;

  onMount(async () => {
    updateInfo = await checkForUpdates();
  });

  function dismiss() {
    dismissed = true;
  }
</script>

{#if updateInfo?.hasUpdate && !dismissed}
  <div class="update-banner">
    <div class="banner-content">
      <Icon
        icon="material-symbols:information-outline"
        class="info-icon"
      />
      <div class="message-container">
        <span class="message">{updateInfo.message}</span>
        <span class="version-info"
          >Installed version: {updateInfo.currentVersion}, Latest version: {updateInfo.latestVersion}</span
        >
      </div>
      <button
        class="dismiss-button"
        on:click={dismiss}
        aria-label="Dismiss"
      >
        <Icon icon="material-symbols:close" />
      </button>
    </div>
  </div>
{/if}

<style>
  .update-banner {
    background: white;
    color: #232323;
    border-top: 4px solid #7a49f6;
    padding: 12px 16px;
    position: fixed;
    top: 0;
    right: 0;
    left: 250px;
    box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
    z-index: 1000;
  }

  .banner-content {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .banner-content :global(.info-icon) {
    font-size: 20px;
    flex-shrink: 0;
  }

  .message-container {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .message {
    font-size: 14px;
    font-weight: 500;
  }

  .version-info {
    font-size: 12px;
    opacity: 0.9;
  }

  .dismiss-button {
    background: none;
    border: none;
    color: #232323;
    cursor: pointer;
    padding: 4px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 4px;
    transition: background-color 0.2s;
    flex-shrink: 0;
  }

  .dismiss-button:hover {
    background-color: rgba(0, 0, 0, 0.05);
  }

  .dismiss-button :global(svg) {
    font-size: 18px;
  }
</style>
