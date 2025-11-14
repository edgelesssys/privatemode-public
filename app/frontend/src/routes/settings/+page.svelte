<script lang="ts">
  import { getApiKey, setApiKey } from '$lib/apiKey';
  import { chatStore } from '$lib/chatStore';
  import Icon from '@iconify/svelte';
  import { goto } from '$app/navigation';

  let apiKey = $state(getApiKey() || '');
  let showApiKey = $state(false);
  let apiKeyError = $state('');
  let apiKeySuccess = $state(false);
  let showDeleteConfirm = $state(false);

  function handleApiKeyUpdate() {
    const trimmed = apiKey.trim();
    if (!trimmed) {
      apiKeyError = 'Please enter an access key';
      return;
    }

    const uuidV4Regex = new RegExp(
      /^[0-9A-F]{8}-[0-9A-F]{4}-4[0-9A-F]{3}-[89AB][0-9A-F]{3}-[0-9A-F]{12}$/i,
    );
    if (!uuidV4Regex.test(trimmed)) {
      apiKeyError = 'Invalid access key format';
      return;
    }

    setApiKey(trimmed);
    apiKeyError = '';
    apiKeySuccess = true;
    setTimeout(() => (apiKeySuccess = false), 3000);
  }

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
      <Icon
        icon="material-symbols:arrow-back"
        width="20"
        height="20"
      />
      Back
    </button>
    <h1>Settings</h1>
  </div>

  <div class="settings-content">
    <section class="setting-section">
      <h2>Access Key</h2>
      <p class="setting-description">
        Update your
        <a
          href="https://portal.privatemode.ai/access-keys"
          target="_blank"
          rel="noopener noreferrer">Privatemode access key</a
        >. For more information, consult the
        <a
          href="https://docs.privatemode.ai/guides/desktop-app"
          target="_blank"
          rel="noopener noreferrer">documentation</a
        >
      </p>

      <div class="input-group">
        <div class="input-wrapper">
          <input
            type={showApiKey ? 'text' : 'password'}
            bind:value={apiKey}
            placeholder="550e8400-e2..."
            class:error={apiKeyError}
          />
          <button
            type="button"
            class="eye-btn"
            onclick={() => (showApiKey = !showApiKey)}
            aria-label={showApiKey ? 'Hide access key' : 'Show access key'}
          >
            <Icon
              icon={showApiKey
                ? 'material-symbols:visibility-off'
                : 'material-symbols:visibility'}
              width="20"
              height="20"
            />
          </button>
        </div>
        {#if apiKeyError}
          <span class="error-message">
            <Icon
              icon="material-symbols:error"
              width="16"
              height="16"
            />
            {apiKeyError}
          </span>
        {/if}
        {#if apiKeySuccess}
          <span class="success-message">
            <Icon
              icon="material-symbols:check-circle"
              width="16"
              height="16"
            />
            Access key updated successfully
          </span>
        {/if}
      </div>

      <button
        onclick={handleApiKeyUpdate}
        class="update-btn"
      >
        Update
      </button>
    </section>

    <section class="setting-section danger-section">
      <h2>Danger Zone</h2>
      <p class="setting-description">Delete all conversations permanently</p>

      {#if !showDeleteConfirm}
        <button
          onclick={() => (showDeleteConfirm = true)}
          class="danger-btn"
        >
          <Icon
            icon="material-symbols:delete-outline"
            width="20"
            height="20"
          />
          Delete All Chats
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
              Yes, Delete All
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
    padding: 40px 20px;
    height: 100vh;
    overflow: hidden;
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
    color: #666;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    padding: 8px 0;
    margin-bottom: 16px;
    font-family: 'Inter Variable', sans-serif;
    transition: color 0.2s;
  }

  .back-btn:hover {
    color: #1a1a1a;
  }

  .settings-header h1 {
    font-size: 32px;
    font-weight: 600;
    color: #1a1a1a;
    margin: 0;
  }

  .settings-content {
    display: flex;
    flex-direction: column;
    gap: 32px;
  }

  .setting-section {
    background: white;
    border-radius: 12px;
    padding: 24px;
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
  }

  .setting-section h2 {
    font-size: 20px;
    font-weight: 600;
    color: #1a1a1a;
    margin: 0 0 8px 0;
  }

  .setting-description {
    color: #666;
    font-size: 14px;
    margin: 0 0 20px 0;
  }

  .setting-description a {
    color: #7a49f6;
    text-decoration: none;
  }

  .input-group {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-bottom: 16px;
  }

  .input-wrapper {
    position: relative;
    display: flex;
    align-items: center;
  }

  input {
    width: 100%;
    padding: 12px 48px 12px 16px;
    border: 1px solid #e0e0e0;
    border-radius: 8px;
    font-size: 14px;
    font-family: 'Inter Variable', sans-serif;
    transition: border-color 0.2s;
    box-sizing: border-box;
  }

  input:focus {
    outline: none;
    border-color: #7a49f6;
  }

  input.error {
    border-color: #ff4444;
  }

  .eye-btn {
    position: absolute;
    right: 12px;
    background: none;
    border: none;
    cursor: pointer;
    color: #666;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 4px;
  }

  .eye-btn:hover {
    color: #1a1a1a;
  }

  .error-message {
    display: flex;
    align-items: center;
    gap: 6px;
    color: #ff4444;
    font-size: 13px;
  }

  .success-message {
    display: flex;
    align-items: center;
    gap: 6px;
    color: #7a49f6;
    font-size: 13px;
  }

  .update-btn {
    padding: 12px 24px;
    background: #7a49f6;
    color: white;
    border: none;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    transition: background-color 0.2s;
    font-family: 'Inter Variable', sans-serif;
  }

  .update-btn:hover {
    background: #8a5cff;
  }

  .danger-section {
    border: 1px solid #ffe0e0;
  }

  .danger-btn {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px 24px;
    background: white;
    color: #ff4444;
    border: 1px solid #ff4444;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s;
    font-family: 'Inter Variable', sans-serif;
  }

  .danger-btn:hover {
    background: #fff5f5;
  }

  .confirm-delete {
    background: #fff5f5;
    padding: 16px;
    border-radius: 8px;
    border: 1px solid #ffe0e0;
  }

  .confirm-text {
    margin: 0 0 16px 0;
    color: #ff4444;
    font-weight: 500;
    font-size: 14px;
  }

  .confirm-actions {
    display: flex;
    gap: 12px;
  }

  .confirm-danger-btn {
    padding: 10px 20px;
    background: #ff4444;
    color: white;
    border: none;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    transition: background-color 0.2s;
    font-family: 'Inter Variable', sans-serif;
  }

  .confirm-danger-btn:hover {
    background: #dd3333;
  }

  .cancel-btn {
    padding: 10px 20px;
    background: white;
    color: #666;
    border: 1px solid #e0e0e0;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    transition: all 0.2s;
    font-family: 'Inter Variable', sans-serif;
  }

  .cancel-btn:hover {
    background: #f5f5f5;
  }
</style>
