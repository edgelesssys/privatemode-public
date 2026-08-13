<script lang="ts">
  import { ChevronRight, ChevronLeft, MessageCirclePlus } from 'lucide-svelte';
  import { chatStore } from '$lib/chatStore';
  import { goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { page } from '$app/stores';
  import SidebarLogo from './SidebarLogo.svelte';
  import ChatList from './ChatList.svelte';
  import InfoSection from './InfoSection.svelte';
  import AuthSection from './AuthSection.svelte';
  import { t } from '$lib/i18n';

  export let onNewChat: () => void = () => {};
  export let onSelectChat: (chatId: string) => void = () => {};
  export let currentChatId: string | null = null;
  export let collapsed: boolean = false;
  export let onToggleCollapse: () => void = () => {};
  export let mobileOpen: boolean = false;
  export let onMobileClose: () => void = () => {};

  $: effectiveCollapsed = mobileOpen ? false : collapsed;

  function handleNewChat() {
    if ($page.url.pathname !== '/') {
      goto(resolve('/'));
    }
    onNewChat();
    onMobileClose();
  }

  function handleSelectChat(chatId: string) {
    if ($page.url.pathname !== '/') {
      chatStore.currentChatId.set(chatId);
      goto(resolve('/'));
    } else {
      onSelectChat(chatId);
    }
    onMobileClose();
  }
</script>

<nav
  class="sidebar"
  class:collapsed={effectiveCollapsed}
  class:mobile-open={mobileOpen}
  aria-label={$t('sidebar.label')}
>
  <button
    class="collapse-btn"
    type="button"
    onclick={onToggleCollapse}
    aria-label={effectiveCollapsed
      ? $t('sidebar.expand')
      : $t('sidebar.collapse')}
  >
    {#if effectiveCollapsed}
      <ChevronRight size={14} />
    {:else}
      <ChevronLeft size={14} />
    {/if}
  </button>
  <SidebarLogo collapsed={effectiveCollapsed} />
  <div class="sidebar-content">
    <button
      class="new-chat-btn"
      type="button"
      onclick={handleNewChat}
      title={effectiveCollapsed ? $t('sidebar.newChat') : ''}
    >
      <MessageCirclePlus size={18} />
      <span
        class="collapsible-text"
        class:hidden={effectiveCollapsed}>{$t('sidebar.newChat')}</span
      >
    </button>

    <ChatList
      {currentChatId}
      collapsed={effectiveCollapsed}
      onSelectChat={handleSelectChat}
    />
  </div>

  <InfoSection
    collapsed={effectiveCollapsed}
    {onMobileClose}
  />

  <AuthSection collapsed={effectiveCollapsed} />
</nav>

<style>
  .sidebar {
    background-color: var(--color-bg-surface);
    width: 250px;
    height: 100dvh;
    position: fixed;
    left: 0;
    top: 0;
    border-right: 1px solid var(--color-border);
    display: flex;
    flex-direction: column;
    transition: width 0.25s ease;
  }

  .sidebar.collapsed {
    width: 60px;
  }

  .sidebar-content {
    width: 85%;
    flex: 1;
    min-height: 0;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    justify-content: flex-start;
    overflow: hidden;
    transition: width 0.25s ease;
  }

  .collapse-btn {
    position: absolute;
    top: 50%;
    right: -14px;
    transform: translateY(-50%);
    z-index: 10;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    background-color: var(--color-bg-surface);
    border: 1px solid var(--color-border);
    color: var(--color-text-secondary);
    cursor: pointer;
    padding: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    opacity: 0;
    transition: opacity 0.2s;
  }

  .sidebar:hover .collapse-btn,
  .collapse-btn:focus-visible {
    opacity: 1;
  }

  .collapse-btn:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }

  .collapse-btn:hover {
    color: var(--color-text-primary);
    background-color: var(--color-bg-hover);
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

  .new-chat-btn {
    margin-top: 30px;
    display: flex;
    align-items: center;
    gap: 5px;
    background: none;
    border: none;
    color: var(--color-text-primary);
    text-decoration: none;
    font-size: 1rem;
    padding: 0;
    transition: gap 0.25s ease;
  }

  .collapsed .new-chat-btn {
    justify-content: center;
    gap: 0;
  }

  .new-chat-btn:hover {
    cursor: pointer;
    opacity: 0.7;
    transition: opacity 0.2s ease-in-out;
  }

  .collapsed .sidebar-content {
    width: 100%;
    align-items: center;
  }

  @media (max-width: 768px) {
    .sidebar {
      transform: translateX(-100%);
      z-index: 20;
      width: 250px !important;
      transition: transform 0.25s ease;
      overflow: hidden;
    }
    .sidebar.mobile-open {
      transform: translateX(0);
    }
    .sidebar.collapsed {
      width: 250px !important;
    }
    .collapse-btn {
      display: none;
    }
  }
</style>
