<script lang="ts">
  import Sidebar from '$lib/components/Sidebar.svelte';
  import Toast from '$lib/components/Toast.svelte';
  import { Menu } from 'lucide-svelte';
  import { chatStore } from '$lib/chatStore';
  import { initTheme } from '$lib/themeStore';
  import {
    initializeClient,
    clientReady,
    clientVerifying,
  } from '$lib/clientStore';
  import {
    initializeAuth,
    isLimitedMode,
    isSignedIn,
    activeApiKey,
    signIn,
    clerkLoaded,
  } from '$lib/authStore';
  import { onMount } from 'svelte';
  import '@fontsource-variable/inter/wght.css';
  import '$lib/styles/theme.css';
  let { children } = $props();

  onMount(() => {
    let disposed = false;
    let unsubscribeApiKey: (() => void) | undefined;
    const themeCleanup = initTheme();

    initializeAuth().then(() => {
      if (disposed) return;
      initializeClient();

      // Re-initialize client when API key changes (e.g. after sign-in)
      unsubscribeApiKey = activeApiKey.subscribe(() => {
        if (!disposed && $clerkLoaded && ($clientReady || !$clientVerifying)) {
          clientReady.set(false);
          clientVerifying.set(true);
          initializeClient();
        }
      });
    });

    return () => {
      disposed = true;
      unsubscribeApiKey?.();
      if (typeof themeCleanup === 'function') themeCleanup();
    };
  });

  let currentChatId = $state<string | null>(null);
  let sidebarCollapsed = $state(false);
  let sidebarOpen = $state(false);

  $effect(() => {
    const unsubscribe = chatStore.currentChatId.subscribe((id) => {
      currentChatId = id;
    });
    return unsubscribe;
  });

  function handleNewChat() {
    const chatId = chatStore.createChat();
    chatStore.currentChatId.set(chatId);
  }

  function handleSelectChat(chatId: string) {
    chatStore.currentChatId.set(chatId);
  }
</script>

<svelte:head>
  <title>Privatemode</title>
</svelte:head>

<main>
  <Sidebar
    {currentChatId}
    onNewChat={handleNewChat}
    onSelectChat={handleSelectChat}
    collapsed={sidebarCollapsed}
    onToggleCollapse={() => {
      if (window.innerWidth <= 768) {
        sidebarOpen = false;
      } else {
        sidebarCollapsed = !sidebarCollapsed;
      }
    }}
    mobileOpen={sidebarOpen}
    onMobileClose={() => (sidebarOpen = false)}
  />
  {#if sidebarOpen}
    <button
      class="sidebar-overlay"
      type="button"
      aria-label="Close sidebar"
      onclick={() => (sidebarOpen = false)}
      tabindex="0"
    ></button>
  {/if}
  <div
    class="content"
    class:collapsed-sidebar={sidebarCollapsed}
  >
    {#if $isLimitedMode}
      <div class="limited-banner">
        <button
          class="hamburger-btn"
          type="button"
          aria-label="Open sidebar"
          onclick={() => (sidebarOpen = true)}
        >
          <Menu size={24} />
        </button>
        <span>You're using Privatemode in limited mode.</span>
        {#if $isSignedIn}
          <span>
            <a
              class="sign-in-link"
              href="https://portal.privatemode.ai/access-keys"
              target="_blank"
              rel="noopener noreferrer">Create a web app API key</a
            >
            in your organization for full access.
          </span>
        {:else}
          <span>
            <button
              class="sign-in-link"
              type="button"
              onclick={signIn}>Sign in</button
            >
            for full access.
          </span>
        {/if}
      </div>
    {:else}
      <div class="preview-banner">
        <button
          class="hamburger-btn"
          type="button"
          aria-label="Open sidebar"
          onclick={() => (sidebarOpen = true)}
        >
          <Menu size={24} />
        </button>
        🚧 Preview — This is an early version of the Privatemode web app.
      </div>
    {/if}
    <div class="page">
      {@render children?.()}
    </div>
  </div>
</main>

<Toast />

<style>
  :global(html) {
    background-color: var(--color-bg-page);
    color-scheme: light;
  }

  :global(html[data-theme='dark']) {
    color-scheme: dark;
  }

  :global(body) {
    font-family: 'Inter Variable', sans-serif;
    margin: 0;
    overflow: hidden;
    background-color: var(--color-bg-page);
  }

  :global(h1, h2, h3, h4, h5, h6) {
    font-family: 'Inter Variable', sans-serif;
    font-weight: 600;
  }

  .content {
    margin-left: 250px;
    display: flex;
    flex-direction: column;
    height: 100dvh;
    background-color: var(--color-bg-page);
    transition: margin-left 0.25s ease;
  }

  .content.collapsed-sidebar {
    margin-left: 60px;
  }

  .preview-banner {
    flex-shrink: 0;
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    background-color: var(--color-banner-bg);
    color: var(--color-banner-text);
    text-align: center;
    padding: 8px 16px;
    font-size: 0.875rem;
    font-weight: 500;
  }

  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  .limited-banner {
    flex-shrink: 0;
    position: relative;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: center;
    background-color: var(--color-accent);
    color: var(--color-text-inverse);
    text-align: center;
    padding: 8px 16px;
    font-size: 0.875rem;
    font-weight: 500;
    gap: 4px;
  }

  .sign-in-link {
    background: none;
    border: none;
    color: var(--color-text-inverse);
    font-size: 0.875rem;
    font-weight: 700;
    cursor: pointer;
    padding: 0;
    text-decoration: underline;
    font-family: 'Inter Variable', sans-serif;
  }

  .sign-in-link:hover {
    opacity: 0.85;
  }

  .hamburger-btn {
    display: none;
    background: none;
    border: none;
    color: var(--color-text-primary);
    cursor: pointer;
    padding: 4px;
    align-items: center;
    justify-content: center;
    position: absolute;
    left: 12px;
  }

  .limited-banner .hamburger-btn {
    color: var(--color-text-inverse);
  }

  .sidebar-overlay {
    display: none;
  }

  @media (max-width: 768px) {
    .content,
    .content.collapsed-sidebar {
      margin-left: 0;
    }

    .hamburger-btn {
      display: flex;
    }

    .preview-banner,
    .limited-banner {
      padding-left: 48px;
      padding-right: 48px;
    }

    .sidebar-overlay {
      display: block;
      position: fixed;
      top: 0;
      left: 0;
      right: 0;
      bottom: 0;
      background: rgba(0, 0, 0, 0.4);
      z-index: 19;
    }
  }
</style>
