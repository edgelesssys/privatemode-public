<script lang="ts">
  import Sidebar from '$lib/components/sidebar/Sidebar.svelte';
  import Toast from '$lib/components/common/Toast.svelte';
  import { Menu } from 'lucide-svelte';
  import { chatStore } from '$lib/chatStore';
  import { initTheme } from '$lib/themeStore';
  import { initializeClient } from '$lib/clientStore';
  import {
    initializeAuth,
    isLimitedMode,
    isSignedIn,
    authIdentity,
    signIn,
    clerkLoaded,
  } from '$lib/authStore';
  import { onMount } from 'svelte';
  import { get } from 'svelte/store';
  import '@fontsource-variable/inter/wght.css';
  import '$lib/styles/theme.css';
  let { children } = $props();

  onMount(() => {
    let disposed = false;
    let unsubscribeAuth: (() => void) | undefined;
    const themeCleanup = initTheme();

    // Serialize client (re-)initialization. If the auth identity changes while
    // an init is still in flight, queue a follow-up run instead of dropping it,
    // so the client always converges on the current identity.
    let initInFlight = false;
    let reinitQueued = false;
    let initializedIdentity: string | null = null;

    const runInit = async () => {
      if (initInFlight) {
        reinitQueued = true;
        return;
      }
      initInFlight = true;
      try {
        do {
          reinitQueued = false;
          if (disposed) return;
          initializedIdentity = get(authIdentity);
          await initializeClient();
        } while (reinitQueued);
      } finally {
        initInFlight = false;
      }
    };

    initializeAuth().then(() => {
      if (disposed) return;
      runInit();

      // Re-initialize the client when the auth identity changes (sign-in/out or
      // active organization), as that changes the resolved credential and secret.
      // The subscribe callback also fires immediately with the current value,
      // which the identity check below skips.
      unsubscribeAuth = authIdentity.subscribe((identity) => {
        if (disposed || !$clerkLoaded) return;
        if (identity === initializedIdentity) return;
        runInit();
      });
    });

    return () => {
      disposed = true;
      unsubscribeAuth?.();
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
  <title>Privatemode Chat</title>
  <meta
    name="description"
    content="Privatemode Chat is your confidential AI chat app for sensitive conversations. Use leading AI models while keeping your data protected."
  />

  <link
    rel="canonical"
    href="https://chat.privatemode.ai/"
  />

  <!-- Open Graph -->
  <meta
    property="og:type"
    content="website"
  />
  <meta
    property="og:site_name"
    content="Privatemode"
  />
  <meta
    property="og:url"
    content="https://chat.privatemode.ai/"
  />
  <meta
    property="og:title"
    content="Privatemode Chat"
  />
  <meta
    property="og:description"
    content="Privatemode Chat is your confidential AI chat app for sensitive conversations. Use leading AI models while keeping your data protected."
  />
  <meta
    property="og:image"
    content="https://chat.privatemode.ai/og-image.png"
  />
  <meta
    property="og:image:width"
    content="2400"
  />
  <meta
    property="og:image:height"
    content="1260"
  />

  <!-- Twitter Card -->
  <meta
    name="twitter:card"
    content="summary_large_image"
  />
  <meta
    name="twitter:title"
    content="Privatemode Chat"
  />
  <meta
    name="twitter:description"
    content="Privatemode Chat is your confidential AI chat app for sensitive conversations. Use leading AI models while keeping your data protected."
  />
  <meta
    name="twitter:image"
    content="https://chat.privatemode.ai/og-image.png"
  />
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
    {#if !$isLimitedMode}
      <button
        class="mobile-menu-btn"
        type="button"
        aria-label="Open sidebar"
        onclick={() => (sidebarOpen = true)}
      >
        <Menu size={24} />
      </button>
    {/if}
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
              href="https://portal.privatemode.ai/api-keys"
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

  .mobile-menu-btn {
    display: none;
    position: absolute;
    top: 12px;
    left: 12px;
    z-index: 10;
    width: 40px;
    height: 40px;
    align-items: center;
    justify-content: center;
    background: var(--color-bg-surface);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    color: var(--color-text-primary);
    cursor: pointer;
    box-shadow: var(--shadow-sm);
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

    .limited-banner {
      padding-left: 48px;
      padding-right: 48px;
    }

    .mobile-menu-btn {
      display: flex;
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
