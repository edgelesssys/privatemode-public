<script lang="ts">
  import {
    ShieldCheck,
    Settings,
    CircleHelp,
    Headset,
    LoaderCircle,
    TriangleAlert,
  } from 'lucide-svelte';
  import { clientError, clientReady, clientVerifying } from '$lib/clientStore';
  import { resolve } from '$app/paths';
  import { t } from '$lib/i18n';

  export let collapsed: boolean;
  export let onMobileClose: () => void;
</script>

<div class="info-section">
  <div class="info-section-content">
    {#if $clientError && !$clientVerifying}
      <p
        class="info-item error-info"
        role="status"
        title={collapsed ? $t('sidebar.connectionFailed') : ''}
      >
        <TriangleAlert size={18} />
        <span
          class="collapsible-text"
          class:hidden={collapsed}>{$t('sidebar.connectionFailed')}</span
        >
      </p>
    {:else if $clientVerifying}
      <!-- No live region here: the chat page already announces this state. -->
      <p
        class="info-item connecting-info"
        title={collapsed ? $t('sidebar.connecting') : ''}
      >
        <LoaderCircle size={18} />
        <span
          class="collapsible-text"
          class:hidden={collapsed}>{$t('sidebar.connecting')}</span
        >
      </p>
    {:else if $clientReady}
      <a
        class="info-item security-info"
        href={resolve('/security')}
        on:click={onMobileClose}
        title={collapsed ? $t('sidebar.sessionSecure') : ''}
      >
        <ShieldCheck size={18} />
        <span
          class="collapsible-text"
          class:hidden={collapsed}>{$t('sidebar.sessionSecure')}</span
        >
      </a>
    {/if}
    <a
      class="info-item"
      href={resolve('/settings')}
      on:click={onMobileClose}
      title={collapsed ? $t('sidebar.settings') : ''}
    >
      <Settings size={18} />
      <span
        class="collapsible-text"
        class:hidden={collapsed}>{$t('sidebar.settings')}</span
      >
    </a>
    <a
      class="info-item"
      href="https://docs.privatemode.ai/"
      target="_blank"
      rel="noopener noreferrer"
      on:click={onMobileClose}
      title={collapsed ? $t('sidebar.howItWorks') : ''}
    >
      <CircleHelp size={18} />
      <span
        class="collapsible-text"
        class:hidden={collapsed}>{$t('sidebar.howItWorks')}</span
      >
    </a>
    <a
      class="info-item"
      href="https://www.privatemode.ai/contact-support"
      target="_blank"
      rel="noopener noreferrer"
      on:click={onMobileClose}
      title={collapsed ? $t('sidebar.support') : ''}
    >
      <Headset size={18} />
      <span
        class="collapsible-text"
        class:hidden={collapsed}>{$t('sidebar.support')}</span
      >
    </a>
  </div>
</div>

<style>
  .info-section {
    border-top: 1px solid var(--color-border);
    overflow: hidden;
  }

  .info-section-content {
    width: 85%;
    margin: 0 auto;
    padding: 30px 0;
    transition: width 0.25s ease;
  }

  :global(.sidebar.collapsed) .info-section-content {
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
  }

  .info-item {
    margin: 15px 0;
    display: flex;
    align-items: center;
    gap: 10px;

    color: var(--color-text-primary);
    text-decoration: none;
    font-weight: 500;
    transition: gap 0.25s ease;
  }

  :global(.sidebar.collapsed) .info-item {
    justify-content: center;
    gap: 0;
  }

  .info-item:first-child {
    margin-top: 0;
  }

  .info-item:last-child {
    margin-bottom: 0;
  }

  .info-item:hover {
    cursor: pointer;
    opacity: 0.8;
    transition: opacity 0.2s ease-in-out;
  }

  .security-info {
    color: var(--color-accent-green);
  }

  .connecting-info {
    color: var(--color-text-secondary);
  }

  .error-info {
    color: var(--color-error);
  }

  .collapsible-text {
    white-space: nowrap;
    overflow: hidden;
    opacity: 1;
    transition: opacity 0.15s ease;
  }

  .collapsible-text.hidden {
    opacity: 0;
    width: 0;
    padding: 0;
    overflow: hidden;
  }

  @keyframes spin {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }

  .connecting-info :global(svg) {
    animation: spin 1s linear infinite;
  }
</style>
