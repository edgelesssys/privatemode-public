<script lang="ts">
  import { setApiKey } from '$lib/apiKey';
  import Icon from '@iconify/svelte';
  import logo from '$lib/assets/logo.svg';

  let stage = $state<'welcome' | 'apikey'>('welcome');
  let apiKey = $state('');
  let error = $state('');
  let showApiKey = $state(false);

  function handleGetStarted() {
    stage = 'apikey';
  }

  function handleBack() {
    stage = 'welcome';
    error = '';
  }

  function handleSubmit() {
    const trimmed = apiKey.trim();
    if (!trimmed) {
      error = 'Please enter an access key';
      return;
    }

    const uuidV4Regex = new RegExp(
      /^[0-9A-F]{8}-[0-9A-F]{4}-4[0-9A-F]{3}-[89AB][0-9A-F]{3}-[0-9A-F]{12}$/i,
    );
    if (!uuidV4Regex.test(trimmed)) {
      error = 'Invalid access key format';
      return;
    }

    setApiKey(trimmed);
    window.location.reload();
  }
</script>

<div class="setup-container">
  <div class="setup-card">
    {#if stage === 'welcome'}
      <img
        class="logo"
        src={logo}
        alt="Privatemode Logo"
      />

      <h1>We keep your conversations private.</h1>
      <p class="welcome-text">Welcome to Privatemode!</p>
      <p class="subtitle">
        Privatemode keeps your prompts and your data encrypted.
      </p>

      <button
        onclick={handleGetStarted}
        class="submit-btn"
      >
        Get started
        <Icon
          icon="material-symbols:arrow-forward"
          width="16"
          height="16"
        />
      </button>
    {:else}
      <button
        onclick={handleBack}
        class="back-btn"
      >
        <Icon
          icon="material-symbols:arrow-back"
          width="16"
          height="16"
        />
        Back
      </button>

      <h1 class="api-key-enter-title">Enter your access key</h1>
      <p class="subtitle">
        Enter the access key you've retrieved from the
        <a
          href="https://portal.privatemode.ai/access-keys"
          target="_blank"
          rel="noopener noreferrer">Privatemode portal</a
        >. For more information, consult the
        <a
          href="https://docs.privatemode.ai/guides/desktop-app"
          target="_blank"
          rel="noopener noreferrer">documentation</a
        >
      </p>

      <form onsubmit={handleSubmit}>
        <div class="input-group">
          <div class="input-wrapper">
            <input
              id="api-key"
              type={showApiKey ? 'text' : 'password'}
              bind:value={apiKey}
              placeholder="550e8400-e2..."
              class:error
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
          {#if error}
            <span class="error-message">
              <Icon
                icon="material-symbols:error"
                width="16"
                height="16"
              />
              {error}
            </span>
          {/if}
        </div>

        <button
          type="submit"
          class="submit-btn"
        >
          Continue
        </button>
      </form>

      <div class="help-text">
        <Icon
          icon="material-symbols:info"
          width="18"
          height="18"
        />
        <p>
          Need help? <a
            href="https://www.privatemode.ai/contact"
            target="_blank"
            rel="noopener noreferrer">Contact us</a
          >
        </p>
      </div>
    {/if}
  </div>
</div>

<style>
  .setup-container {
    display: flex;
    justify-content: center;
    align-items: center;
    min-height: 100vh;
    background: linear-gradient(
      180deg,
      #000000 0%,
      #7a49f6 63.6%,
      #f3f4f5 127.2%
    );
  }

  .logo {
    display: block;
    width: 80px;
    margin: 0 auto;
  }

  .setup-card {
    display: flex;
    flex-direction: column;
    justify-content: center;
    padding: 48px;
    max-width: 500px;
    width: 100%;

    background: white;
    border-radius: 16px;

    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
  }

  h1 {
    margin: 30px 0 0 0;
    font-size: 26px;
    text-align: center;
  }

  .subtitle {
    margin: 12px 0 42px 0;
    text-align: center;
    font-size: 16px;
  }

  .welcome-text {
    margin: 42dpx 0 8px 0;
    text-align: center;
    font-size: 16px;
    font-weight: 500;
  }

  .subtitle a {
    color: #7a49f6;
    text-decoration: none;
  }

  .input-group {
    margin-bottom: 24px;
  }

  .input-wrapper {
    position: relative;
    display: flex;
    align-items: center;
  }

  input {
    width: 100%;
    padding: 12px 48px 12px 16px;
    border: 2px solid #e0e0e0;
    border-radius: 8px;
    font-size: 16px;
    transition: border-color 0.2s;
    box-sizing: border-box;
  }

  input:focus {
    outline: none;
    border-color: #7a49f6;
  }

  input.error {
    border-color: #ef4444;
  }

  .error-message {
    display: flex;
    align-items: center;
    gap: 4px;
    color: #ef4444;
    font-size: 14px;
    margin-top: 8px;
  }

  .submit-btn {
    margin: 0 auto;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 4px;
    padding: 14px 32px;

    border: none;
    border-radius: 100px;
    background: #7a49f6;
    color: white;

    font-size: 16px;
    font-weight: 600;

    cursor: pointer;
    box-shadow: 0px 4px 20px 0px #00000040;
    transition:
      transform 0.2s,
      box-shadow 0.2s ease-in-out;
  }

  .submit-btn:hover {
    transform: translateY(-2px);
    box-shadow: 0px 4px 20px 0px #00000050;
  }

  .submit-btn:active {
    transform: translateY(0);
  }

  .help-text {
    margin-top: 24px;
    padding-top: 24px;
    border-top: 1px solid #e0e0e0;
    display: flex;
    align-items: center;
    gap: 8px;
    color: #666;
    font-size: 14px;
    justify-content: center;
  }

  .help-text p {
    margin: 0;
  }

  .help-text a {
    color: #7a49f6;
    text-decoration: none;
    font-weight: 600;
  }

  .help-text a:hover {
    text-decoration: underline;
  }

  .back-btn {
    background: none;
    border: none;
    font-size: 14px;
    font-weight: 600;
    cursor: pointer;
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 0;
    margin-bottom: 24px;
    opacity: 0.8;
    transition:
      transform 0.2s,
      opacity 0.2s ease-in-out;
  }

  .back-btn:hover {
    transform: translateY(-2px);
    opacity: 0.6;
  }

  .api-key-enter-title {
    margin-top: 0;
    margin-bottom: 8px;
  }

  .eye-btn {
    position: absolute;
    right: 12px;
    background: none;
    border: none;
    color: #666;
    cursor: pointer;
    padding: 4px;
    display: flex;
    align-items: center;
    transition: color 0.2s;
  }

  .eye-btn:hover {
    color: #7a49f6;
  }
</style>
