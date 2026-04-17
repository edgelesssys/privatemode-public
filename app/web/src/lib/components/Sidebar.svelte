<script lang="ts">
  import sidebarLogoDark from '$lib/assets/logo-text.svg';
  import sidebarLogoLight from '$lib/assets/logo-text-light.svg';
  import sidebarLogoSmallDark from '$lib/assets/logo.svg';
  import sidebarLogoSmallLight from '$lib/assets/logo-light.svg';
  import {
    ChevronRight,
    ChevronLeft,
    ChevronDown,
    Check,
    MessageCirclePlus,
    Pencil,
    Trash2,
    ShieldCheck,
    Settings,
    CircleHelp,
    Headset,
    LoaderCircle,
    TriangleAlert,
    LogIn,
    LogOut,
  } from 'lucide-svelte';
  import Tooltip from './Tooltip.svelte';

  import { chatStore } from '$lib/chatStore';
  import type { Chat } from '$lib/chatStore';
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import { clientError, clientReady, clientVerifying } from '$lib/clientStore';
  import { isDark } from '$lib/themeStore';
  import {
    isSignedIn,
    userDisplayName,
    userImageUrl,
    orgName,
    orgId,
    userOrganizations,
    signIn,
    signOut,
    switchOrganization,
    clerkLoaded,
  } from '$lib/authStore';

  $: sidebarLogo = $isDark ? sidebarLogoLight : sidebarLogoDark;
  $: sidebarLogoSmall = $isDark ? sidebarLogoSmallLight : sidebarLogoSmallDark;

  export let onNewChat: () => void = () => {};
  export let onSelectChat: (chatId: string) => void = () => {};
  export let currentChatId: string | null = null;
  export let collapsed: boolean = false;
  export let onToggleCollapse: () => void = () => {};
  export let mobileOpen: boolean = false;
  export let onMobileClose: () => void = () => {};

  let renamingChatId: string | null = null;
  let renameValue: string = '';
  let renameInputElement: HTMLInputElement | null = null;
  let orgSwitcherOpen: boolean = false;
  let orgSwitcherRef: HTMLDivElement | null = null;
  let orgSubtitleBtn: HTMLButtonElement | null = null;
  let dropdownStyle: string = '';

  function updateDropdownPosition() {
    if (!orgSubtitleBtn) return;
    const rect = orgSubtitleBtn.getBoundingClientRect();
    const left = rect.left;
    const bottom = window.innerHeight - rect.top + 4;
    const width = Math.max(rect.width, 180);
    dropdownStyle = `position:fixed;left:${left}px;bottom:${bottom}px;width:${width}px;`;
  }

  function toggleOrgSwitcher() {
    orgSwitcherOpen = !orgSwitcherOpen;
    if (orgSwitcherOpen) {
      updateDropdownPosition();
    }
  }

  function handleOrgSwitcherClickOutside(event: MouseEvent) {
    if (orgSwitcherRef && !orgSwitcherRef.contains(event.target as Node)) {
      orgSwitcherOpen = false;
    }
  }

  $: if (renamingChatId && renameInputElement) {
    renameInputElement.focus();
    renameInputElement.select();
  }

  interface GroupedChats {
    today: Chat[];
    yesterday: Chat[];
    lastWeek: Chat[];
    older: Chat[];
  }

  function groupChatsByDate(chats: Chat[]): GroupedChats {
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const yesterday = new Date(today);
    yesterday.setDate(yesterday.getDate() - 1);
    const lastWeek = new Date(today);
    lastWeek.setDate(lastWeek.getDate() - 7);

    const grouped: GroupedChats = {
      today: [],
      yesterday: [],
      lastWeek: [],
      older: [],
    };

    for (const chat of chats) {
      const chatDate = new Date(chat.lastUserMessageAt);

      if (chatDate >= today) {
        grouped.today.push(chat);
      } else if (chatDate >= yesterday) {
        grouped.yesterday.push(chat);
      } else if (chatDate >= lastWeek) {
        grouped.lastWeek.push(chat);
      } else {
        grouped.older.push(chat);
      }
    }

    return grouped;
  }

  function truncateTitle(title: string, maxLength: number = 30): string {
    if (title.length <= maxLength) return title;
    return title.slice(0, maxLength) + '...';
  }

  function handleNewChat() {
    if ($page.url.pathname !== '/') {
      goto('/');
    }
    onNewChat();
    onMobileClose();
  }

  function handleSelectChat(chatId: string) {
    if ($page.url.pathname !== '/') {
      chatStore.currentChatId.set(chatId);
      goto('/');
    } else {
      onSelectChat(chatId);
    }
    onMobileClose();
  }

  function handleDeleteChat(event: MouseEvent, chatId: string) {
    event.stopPropagation();
    if (confirm('Are you sure you want to delete this chat?')) {
      chatStore.deleteChat(chatId);
    }
  }

  function startRename(
    event: MouseEvent,
    chatId: string,
    currentTitle: string,
  ) {
    event.stopPropagation();
    renamingChatId = chatId;
    renameValue = currentTitle;
  }

  function confirmRename(chatId: string) {
    if (renameValue.trim()) {
      chatStore.renameChat(chatId, renameValue.trim());
    }
    renamingChatId = null;
    renameValue = '';
  }

  function cancelRename() {
    renamingChatId = null;
    renameValue = '';
  }

  function handleRenameKeydown(event: KeyboardEvent, chatId: string) {
    if (event.key === 'Enter') {
      confirmRename(chatId);
    } else if (event.key === 'Escape') {
      cancelRename();
    }
  }

  $: effectiveCollapsed = mobileOpen ? false : collapsed;

  $: sortedChats = [...$chatStore].sort(
    (a, b) => b.lastUserMessageAt - a.lastUserMessageAt,
  );
  $: groupedChats = groupChatsByDate(sortedChats);
  $: chatGroups = [
    { label: 'Today', chats: groupedChats.today },
    { label: 'Yesterday', chats: groupedChats.yesterday },
    { label: 'Last 7 Days', chats: groupedChats.lastWeek },
    { label: 'Older', chats: groupedChats.older },
  ].filter((g) => g.chats.length > 0);
</script>

<svelte:window on:click={handleOrgSwitcherClickOutside} />

<div
  class="sidebar"
  class:collapsed={effectiveCollapsed}
  class:mobile-open={mobileOpen}
>
  <button
    class="collapse-btn"
    type="button"
    onclick={onToggleCollapse}
    aria-label={effectiveCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
  >
    {#if effectiveCollapsed}
      <ChevronRight size={14} />
    {:else}
      <ChevronLeft size={14} />
    {/if}
  </button>
  <div class="sidebar-logo">
    <img
      src={sidebarLogoSmall}
      alt="Privatemode Logo"
      class="logo-small"
      class:logo-visible={effectiveCollapsed}
    />
    <img
      src={sidebarLogo}
      alt="Privatemode Logo"
      class="logo-full"
      class:logo-visible={!effectiveCollapsed}
    />
  </div>
  <div class="sidebar-content">
    <button
      class="new-chat-btn"
      type="button"
      onclick={handleNewChat}
      title={effectiveCollapsed ? 'New Chat' : ''}
    >
      <MessageCirclePlus size={18} />
      <span
        class="collapsible-text"
        class:hidden={effectiveCollapsed}>New Chat</span
      >
    </button>

    <div
      class="chats-section"
      class:hidden={effectiveCollapsed}
      aria-hidden={effectiveCollapsed}
    >
      {#each chatGroups as group (group.label)}
        <div class="chat-group">
          <h3 class="group-title">{group.label}</h3>
          {#each group.chats as chat (chat.id)}
            <div
              class="chat-item-wrapper"
              class:active={currentChatId === chat.id}
              role="button"
              tabindex={effectiveCollapsed ? -1 : 0}
              onclick={() =>
                renamingChatId !== chat.id && handleSelectChat(chat.id)}
              onkeydown={(e) => {
                if (
                  (e.key === 'Enter' || e.key === ' ') &&
                  renamingChatId !== chat.id
                ) {
                  e.preventDefault();
                  handleSelectChat(chat.id);
                }
              }}
            >
              {#if renamingChatId === chat.id}
                <input
                  class="rename-input"
                  type="text"
                  bind:value={renameValue}
                  bind:this={renameInputElement}
                  onkeydown={(e) => handleRenameKeydown(e, chat.id)}
                  onblur={() => confirmRename(chat.id)}
                />
              {:else}
                <span class="chat-title">{truncateTitle(chat.title)}</span>
                <div class="chat-actions">
                  <Tooltip text="Rename chat">
                    <button
                      class="rename-btn"
                      type="button"
                      onclick={(e) => startRename(e, chat.id, chat.title)}
                    >
                      <Pencil size={16} />
                    </button>
                  </Tooltip>
                  <Tooltip text="Delete chat">
                    <button
                      class="delete-btn"
                      type="button"
                      onclick={(e) => handleDeleteChat(e, chat.id)}
                    >
                      <Trash2 size={16} />
                    </button>
                  </Tooltip>
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/each}
    </div>
  </div>

  <div class="info-section">
    <div class="info-section-content">
      {#if $clientError && !$clientVerifying}
        <p
          class="info-item error-info"
          title={effectiveCollapsed ? 'Connection failed' : ''}
        >
          <TriangleAlert size={18} />
          <span
            class="collapsible-text"
            class:hidden={effectiveCollapsed}>Connection failed</span
          >
        </p>
      {:else if $clientVerifying}
        <p
          class="info-item connecting-info"
          title={effectiveCollapsed ? 'Connecting...' : ''}
        >
          <LoaderCircle size={18} />
          <span
            class="collapsible-text"
            class:hidden={effectiveCollapsed}>Connecting...</span
          >
        </p>
      {:else if $clientReady}
        <a
          class="info-item security-info"
          href="/security"
          onclick={onMobileClose}
          title={effectiveCollapsed ? 'Your session is secure' : ''}
        >
          <ShieldCheck size={18} />
          <span
            class="collapsible-text"
            class:hidden={effectiveCollapsed}>Your session is secure</span
          >
        </a>
      {/if}
      <a
        class="info-item"
        href="/settings"
        onclick={onMobileClose}
        title={effectiveCollapsed ? 'Settings' : ''}
      >
        <Settings size={18} />
        <span
          class="collapsible-text"
          class:hidden={effectiveCollapsed}>Settings</span
        >
      </a>
      <a
        class="info-item"
        href="https://docs.privatemode.ai/"
        target="_blank"
        rel="noopener noreferrer"
        onclick={onMobileClose}
        title={effectiveCollapsed ? 'How does it work?' : ''}
      >
        <CircleHelp size={18} />
        <span
          class="collapsible-text"
          class:hidden={effectiveCollapsed}>How does it work?</span
        >
      </a>
      <a
        class="info-item"
        href="https://www.privatemode.ai/contact-support"
        target="_blank"
        rel="noopener noreferrer"
        onclick={onMobileClose}
        title={effectiveCollapsed ? 'Get support' : ''}
      >
        <Headset size={18} />
        <span
          class="collapsible-text"
          class:hidden={effectiveCollapsed}>Get support</span
        >
      </a>
    </div>
  </div>

  <div class="auth-section">
    <div class="auth-section-content">
      {#if !$clerkLoaded}
        <div
          class="auth-skeleton"
          class:collapsed-auth={effectiveCollapsed}
        >
          <div class="skeleton-avatar"></div>
          <div
            class="skeleton-text collapsible-text"
            class:hidden={effectiveCollapsed}
          ></div>
        </div>
      {:else if $isSignedIn}
        <div
          class="auth-user"
          class:collapsed-auth={effectiveCollapsed}
        >
          {#if $userImageUrl}
            <img
              src={$userImageUrl}
              alt="Profile"
              class="auth-avatar"
              title={effectiveCollapsed ? ($userDisplayName ?? '') : ''}
            />
          {:else}
            <div
              class="auth-avatar-placeholder"
              title={effectiveCollapsed ? ($userDisplayName ?? '') : ''}
            ></div>
          {/if}
          <div
            class="auth-user-info collapsible-text"
            class:hidden={effectiveCollapsed}
          >
            <span class="auth-user-name">{$userDisplayName}</span>
            {#if $userOrganizations.length > 1}
              <div
                class="org-switcher"
                bind:this={orgSwitcherRef}
              >
                <button
                  class="org-subtitle"
                  type="button"
                  bind:this={orgSubtitleBtn}
                  onclick={toggleOrgSwitcher}
                >
                  <span class="org-subtitle-name">{$orgName ?? 'Personal'}</span
                  >
                  <span
                    class="org-chevron"
                    class:open={orgSwitcherOpen}
                  >
                    <ChevronDown size={12} />
                  </span>
                </button>
                {#if orgSwitcherOpen}
                  <div
                    class="org-switcher-dropdown"
                    style={dropdownStyle}
                  >
                    {#each $userOrganizations as org (org.id)}
                      <button
                        class="org-switcher-item"
                        class:active={org.id === $orgId}
                        type="button"
                        onclick={() => {
                          if (org.id !== $orgId) {
                            switchOrganization(org.id).catch((err) => {
                              console.error(
                                'Failed to switch organization:',
                                err,
                              );
                            });
                          }
                          orgSwitcherOpen = false;
                        }}
                      >
                        {#if org.imageUrl}
                          <img
                            class="org-switcher-item-img"
                            src={org.imageUrl}
                            alt={org.name}
                          />
                        {:else}
                          <span class="org-switcher-item-initials"
                            >{org.name.charAt(0).toUpperCase()}</span
                          >
                        {/if}
                        <span class="org-switcher-item-name">{org.name}</span>
                        {#if org.id === $orgId}
                          <Check size={14} />
                        {/if}
                      </button>
                    {/each}
                  </div>
                {/if}
              </div>
            {:else if $orgName}
              <span class="org-subtitle-static">{$orgName}</span>
            {/if}
          </div>
          <Tooltip text="Sign out">
            <button
              class="auth-sign-out-btn collapsible-text"
              class:hidden={effectiveCollapsed}
              type="button"
              onclick={signOut}
            >
              <LogOut size={16} />
            </button>
          </Tooltip>
        </div>
      {:else}
        <button
          class="info-item auth-btn sign-in"
          type="button"
          onclick={signIn}
          title={effectiveCollapsed ? 'Sign in' : ''}
        >
          <LogIn size={18} />
          <span
            class="collapsible-text"
            class:hidden={effectiveCollapsed}>Sign in</span
          >
        </button>
      {/if}
    </div>
  </div>
</div>

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

  .sidebar-logo {
    width: 100%;
    height: 68px;
    border-bottom: 1px solid var(--color-border);
    display: flex;
    align-items: center;
    justify-content: center;
    position: relative;
    flex-shrink: 0;
    overflow: hidden;
  }

  .sidebar-logo img {
    margin: 20px 0;
    position: absolute;
    opacity: 0;
    transition: opacity 0.2s ease;
    pointer-events: none;
  }

  .sidebar-logo img.logo-visible {
    opacity: 1;
    pointer-events: auto;
  }

  .sidebar-logo .logo-full {
    width: 85%;
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

  .sidebar-logo img.logo-small {
    width: 28px;
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

  .chats-section.hidden {
    opacity: 0;
    pointer-events: none;
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

  .chats-section {
    margin-top: 30px;
    overflow-y: auto;
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 1rem;
    padding-right: 8px;
    opacity: 1;
    transition: opacity 0.15s ease;
  }

  .chats-section::-webkit-scrollbar {
    width: 4px;
  }

  .chats-section::-webkit-scrollbar-thumb {
    border-radius: 2px;
    background: transparent;
    transition: background 0.2s;
  }

  .sidebar:hover .chats-section::-webkit-scrollbar-thumb {
    background: var(--color-scrollbar);
  }

  .chat-group {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .group-title {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--color-text-secondary);
    text-transform: uppercase;
    margin: 0;
    padding: 0;
  }

  .chat-item-wrapper {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem;
    border-radius: 0.375rem;
    transition: background-color 0.2s;
    cursor: pointer;
  }

  .chat-item-wrapper:hover {
    background-color: var(--color-bg-hover);
  }

  .chat-item-wrapper.active {
    background-color: var(--color-bg-active);
  }

  .chat-title {
    flex: 1;
    min-width: 0;
    color: var(--color-text-primary);
    font-size: 0.875rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chat-actions {
    display: flex;
    align-items: center;
    flex-shrink: 0;
  }

  .rename-btn {
    background: none;
    border: none;
    color: var(--color-text-secondary);
    cursor: pointer;
    padding: 0.25rem;
    display: flex;
    align-items: center;
    justify-content: center;
    opacity: 0;
    transition: opacity 0.2s;
    flex-shrink: 0;
  }

  .chat-item-wrapper:hover .rename-btn {
    opacity: 1;
  }

  .rename-btn:hover {
    color: var(--color-text-primary);
  }

  .delete-btn {
    background: none;
    border: none;
    color: var(--color-text-secondary);
    cursor: pointer;
    padding: 0.25rem;
    display: flex;
    align-items: center;
    justify-content: center;
    opacity: 0;
    transition: opacity 0.2s;
    flex-shrink: 0;
  }

  .chat-item-wrapper:hover .delete-btn {
    opacity: 1;
  }

  .delete-btn:hover {
    color: var(--color-danger);
  }

  .rename-input {
    flex: 1;
    min-width: 0;
    width: 100%;
    background: var(--color-bg-surface-secondary);
    border: 1px solid var(--color-border-secondary);
    border-radius: 0.25rem;
    color: var(--color-text-primary);
    padding: 0.25rem 0.5rem;
    font-size: 0.875rem;
    outline: none;
  }

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

  .collapsed .info-item {
    justify-content: center;
    gap: 0;
  }

  .collapsed .info-section-content {
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
  }

  .collapsed .sidebar-content {
    width: 100%;
    align-items: center;
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

  .auth-section {
    border-top: 1px solid var(--color-border);
  }

  .auth-section-content {
    width: 85%;
    margin: 0 auto;
    padding: 15px 0;
    transition: width 0.25s ease;
  }

  .collapsed .auth-section-content {
    width: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
  }

  .auth-user {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .auth-user.collapsed-auth {
    justify-content: center;
    gap: 0;
  }

  .auth-avatar {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    object-fit: cover;
    flex-shrink: 0;
  }

  .auth-avatar-placeholder {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    background: var(--color-bg-hover);
    flex-shrink: 0;
  }

  .auth-user-info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .auth-user-name {
    font-size: 0.875rem;
    font-weight: 500;
    color: var(--color-text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .auth-sign-out-btn {
    background: none;
    border: none;
    color: var(--color-text-secondary);
    cursor: pointer;
    padding: 0.25rem;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    opacity: 0;
    transition: opacity 0.2s;
  }

  .auth-user:hover .auth-sign-out-btn {
    opacity: 1;
  }

  .auth-sign-out-btn:hover {
    color: var(--color-text-primary);
  }

  .org-switcher {
    position: relative;
  }

  .org-subtitle {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    max-width: 100%;
    background: none;
    border: none;
    color: var(--color-text-secondary);
    font-size: 0.8125rem;
    font-weight: 500;
    padding: 0;
    cursor: pointer;
    font-family: 'Inter Variable', sans-serif;
  }

  .org-subtitle:hover {
    color: var(--color-text-primary);
  }

  .org-subtitle-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .org-chevron {
    display: inline-flex;
    transition: transform 0.2s ease;
  }

  .org-chevron.open {
    transform: rotate(180deg);
  }

  .org-subtitle-static {
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--color-text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .org-switcher-dropdown {
    background-color: var(--color-dropdown-bg);
    border: 1px solid var(--color-dropdown-border);
    border-radius: 0.375rem;
    box-shadow: var(--shadow-md);
    z-index: 1000;
    max-height: 200px;
    overflow-y: auto;
    padding: 4px;
  }

  .org-switcher-item {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    background: none;
    border: none;
    border-radius: 0.25rem;
    color: var(--color-dropdown-text);
    font-size: 0.8125rem;
    font-weight: 500;
    padding: 6px 8px;
    cursor: pointer;
    font-family: 'Inter Variable', sans-serif;
    transition: background-color 0.2s;
  }

  .org-switcher-item:hover {
    background-color: var(--color-dropdown-hover);
  }

  .org-switcher-item.active {
    color: var(--color-dropdown-accent);
  }

  .org-switcher-item-img {
    width: 20px;
    height: 20px;
    border-radius: 4px;
    object-fit: cover;
    flex-shrink: 0;
  }

  .org-switcher-item-initials {
    width: 20px;
    height: 20px;
    border-radius: 4px;
    background-color: var(--color-bg-hover);
    color: var(--color-text-secondary);
    font-size: 0.6875rem;
    font-weight: 600;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .org-switcher-item-name {
    flex: 1;
    min-width: 0;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .auth-btn {
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    font-family: 'Inter Variable', sans-serif;
    font-size: 0.875rem;
  }

  .auth-btn.sign-in {
    color: var(--color-accent);
  }

  .auth-skeleton {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .auth-skeleton.collapsed-auth {
    justify-content: center;
  }

  .skeleton-avatar {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    background: var(--color-bg-hover);
    flex-shrink: 0;
    animation: skeleton-pulse 1.5s ease-in-out infinite;
  }

  .skeleton-text {
    height: 14px;
    width: 100px;
    border-radius: 4px;
    background: var(--color-bg-hover);
    animation: skeleton-pulse 1.5s ease-in-out infinite;
  }

  @keyframes skeleton-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.4;
    }
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
