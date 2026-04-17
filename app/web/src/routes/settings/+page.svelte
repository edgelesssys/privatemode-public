<script lang="ts">
  import { chatStore } from '$lib/chatStore';
  import { ArrowLeft, Trash2, Monitor, Sun, Moon } from 'lucide-svelte';
  import { goto } from '$app/navigation';
  import { themePreference } from '$lib/themeStore';
  import type { ThemePreference } from '$lib/themeStore';
  import { onMount } from 'svelte';

  let showDeleteConfirm = $state(false);
  let currentTheme = $state<ThemePreference>('system');

  onMount(() => {
    return themePreference.subscribe((v) => {
      currentTheme = v;
    });
  });

  function setTheme(value: ThemePreference) {
    themePreference.set(value);
  }

  const themeOptions: {
    value: ThemePreference;
    label: string;
    icon: typeof Monitor;
  }[] = [
    { value: 'system', label: 'System', icon: Monitor },
    { value: 'light', label: 'Light', icon: Sun },
    { value: 'dark', label: 'Dark', icon: Moon },
  ];

  function handleDeleteAllChats() {
    chatStore.clear();
    showDeleteConfirm = false;
    goto('/');
  }

  function handleBack() {
    goto('/');
  }
</script>

<div class="settings-container">
  <div class="settings-header">
    <button
      onclick={handleBack}
      class="back-btn"
    >
      <ArrowLeft size={20} />
      Back
    </button>
    <h1>Settings</h1>
  </div>

  <div class="settings-content">
    <section class="setting-section">
      <h2>Appearance</h2>
      <p class="setting-description">Choose your preferred color theme</p>
      <div class="theme-picker">
        {#each themeOptions as option}
          <button
            class="theme-option"
            class:active={currentTheme === option.value}
            onclick={() => setTheme(option.value)}
          >
            <option.icon size={18} />
            {option.label}
          </button>
        {/each}
      </div>
    </section>

    <section class="setting-section danger-section">
      <h2>Danger zone</h2>
      <p class="setting-description">Delete all conversations permanently</p>

      {#if !showDeleteConfirm}
        <button
          onclick={() => (showDeleteConfirm = true)}
          class="danger-btn"
        >
          <Trash2 size={20} />
          Delete all chats
        </button>
      {:else}
        <div class="confirm-delete">
          <p class="confirm-text">
            Are you sure? This action cannot be undone.
          </p>
          <div class="confirm-actions">
            <button
              onclick={handleDeleteAllChats}
              class="confirm-danger-btn"
            >
              Yes, delete all
            </button>
            <button
              onclick={() => (showDeleteConfirm = false)}
              class="cancel-btn"
            >
              Cancel
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
