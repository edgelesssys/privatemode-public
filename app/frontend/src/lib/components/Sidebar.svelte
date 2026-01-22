<script lang="ts">
  import sidebarLogo from '$lib/assets/logo-text.svg';
  import Icon from '@iconify/svelte';
  import Tooltip from './Tooltip.svelte';

  import { chatStore } from '$lib/chatStore';
  import type { Chat } from '$lib/chatStore';
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import { modelsLoaded } from '$lib/proxyStore';
  import { onMount } from 'svelte';

  export let onNewChat: () => void = () => {};
  export let onSelectChat: (chatId: string) => void = () => {};
  export let currentChatId: string | null = null;

  let renamingChatId: string | null = null;
  let renameValue: string = '';
  let renameInputElement: HTMLInputElement | null = null;
  let appVersion: string = '';

  onMount(async () => {
    appVersion = await window.electron.getVersion();
  });

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
  }

  function handleSelectChat(chatId: string) {
    if ($page.url.pathname !== '/') {
      chatStore.currentChatId.set(chatId);
      goto('/');
    } else {
      onSelectChat(chatId);
    }
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

  $: sortedChats = [...$chatStore].sort(
    (a, b) => b.lastUserMessageAt - a.lastUserMessageAt,
  );
  $: groupedChats = groupChatsByDate(sortedChats);
</script>

<div class="sidebar">
  <div class="sidebar-content">
    <img
      src={sidebarLogo}
      class="sidebar-logo"
      alt="Privatemode Logo"
    />
    <button
      class="new-chat-btn"
      type="button"
      onclick={handleNewChat}
    >
      <Icon
        icon="material-symbols:add"
        width="18"
        height="18"
      />
      New Chat
    </button>

    <div class="chats-section">
      {#if groupedChats.today.length > 0}
        <div class="chat-group">
          <h3 class="group-title">Today</h3>
          {#each groupedChats.today as chat (chat.id)}
            <div
              class="chat-item-wrapper"
              class:active={currentChatId === chat.id}
              role="button"
              tabindex="0"
              onclick={() =>
                renamingChatId !== chat.id && handleSelectChat(chat.id)}
              onkeydown={(e) =>
                (e.key === 'Enter' || e.key === ' ') &&
                renamingChatId !== chat.id &&
                handleSelectChat(chat.id)}
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
                      <Icon
                        icon="material-symbols:edit-outline"
                        width="16"
                        height="16"
                      />
                    </button>
                  </Tooltip>
                  <Tooltip text="Delete chat">
                    <button
                      class="delete-btn"
                      type="button"
                      onclick={(e) => handleDeleteChat(e, chat.id)}
                    >
                      <Icon
                        icon="material-symbols:delete-outline"
                        width="16"
                        height="16"
                      />
                    </button>
                  </Tooltip>
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}

      {#if groupedChats.yesterday.length > 0}
        <div class="chat-group">
          <h3 class="group-title">Yesterday</h3>
          {#each groupedChats.yesterday as chat (chat.id)}
            <div
              class="chat-item-wrapper"
              class:active={currentChatId === chat.id}
              role="button"
              tabindex="0"
              onclick={() =>
                renamingChatId !== chat.id && handleSelectChat(chat.id)}
              onkeydown={(e) =>
                (e.key === 'Enter' || e.key === ' ') &&
                renamingChatId !== chat.id &&
                handleSelectChat(chat.id)}
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
                      <Icon
                        icon="material-symbols:edit-outline"
                        width="16"
                        height="16"
                      />
                    </button>
                  </Tooltip>
                  <Tooltip text="Delete chat">
                    <button
                      class="delete-btn"
                      type="button"
                      onclick={(e) => handleDeleteChat(e, chat.id)}
                    >
                      <Icon
                        icon="material-symbols:delete-outline"
                        width="16"
                        height="16"
                      />
                    </button>
                  </Tooltip>
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}

      {#if groupedChats.lastWeek.length > 0}
        <div class="chat-group">
          <h3 class="group-title">Last 7 Days</h3>
          {#each groupedChats.lastWeek as chat (chat.id)}
            <div
              class="chat-item-wrapper"
              class:active={currentChatId === chat.id}
              role="button"
              tabindex="0"
              onclick={() =>
                renamingChatId !== chat.id && handleSelectChat(chat.id)}
              onkeydown={(e) =>
                (e.key === 'Enter' || e.key === ' ') &&
                renamingChatId !== chat.id &&
                handleSelectChat(chat.id)}
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
                      <Icon
                        icon="material-symbols:edit-outline"
                        width="16"
                        height="16"
                      />
                    </button>
                  </Tooltip>
                  <Tooltip text="Delete chat">
                    <button
                      class="delete-btn"
                      type="button"
                      onclick={(e) => handleDeleteChat(e, chat.id)}
                    >
                      <Icon
                        icon="material-symbols:delete-outline"
                        width="16"
                        height="16"
                      />
                    </button>
                  </Tooltip>
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}

      {#if groupedChats.older.length > 0}
        <div class="chat-group">
          <h3 class="group-title">Older</h3>
          {#each groupedChats.older as chat (chat.id)}
            <div
              class="chat-item-wrapper"
              class:active={currentChatId === chat.id}
              role="button"
              tabindex="0"
              onclick={() =>
                renamingChatId !== chat.id && handleSelectChat(chat.id)}
              onkeydown={(e) =>
                (e.key === 'Enter' || e.key === ' ') &&
                renamingChatId !== chat.id &&
                handleSelectChat(chat.id)}
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
                      <Icon
                        icon="material-symbols:edit-outline"
                        width="16"
                        height="16"
                      />
                    </button>
                  </Tooltip>
                  <Tooltip text="Delete chat">
                    <button
                      class="delete-btn"
                      type="button"
                      onclick={(e) => handleDeleteChat(e, chat.id)}
                    >
                      <Icon
                        icon="material-symbols:delete-outline"
                        width="16"
                        height="16"
                      />
                    </button>
                  </Tooltip>
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <div class="info-section">
      {#if $modelsLoaded}
        <a
          class="info-item security-info"
          href="/security"
        >
          <Icon
            icon="material-symbols:security"
            width="18"
            height="18"
          />
          Your session is secure
        </a>
      {:else}
        <p class="info-item connecting-info">
          <Icon
            icon="svg-spinners:180-ring"
            width="18"
            height="18"
          />
          Connecting...
        </p>
      {/if}
      <a
        class="info-item"
        href="/settings"
      >
        <Icon
          icon="material-symbols:settings"
          width="18"
          height="18"
        />
        Settings
      </a>
      <a
        class="info-item"
        href="https://docs.privatemode.ai/"
        target="_blank"
        rel="noopener noreferrer"
      >
        <Icon
          icon="material-symbols:help"
          width="18"
          height="18"
        />
        How does it work?
      </a>
      <a
        class="info-item"
        href="https://www.privatemode.ai/contact-support"
        target="_blank"
        rel="noopener noreferrer"
      >
        <Icon
          icon="material-symbols:support-agent"
          width="18"
          height="18"
        />
        Get support
      </a>
      {#if appVersion}
        <span class="version-text">Privatemode {appVersion}</span>
      {/if}
    </div>
  </div>
</div>

<style>
  .sidebar {
    background-color: #232323;
    width: 250px;
    height: 100vh;
    position: fixed;
    left: 0;
    top: 0;
  }

  .sidebar-content {
    width: 85%;
    height: 100vh;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    justify-content: flex-start;
  }

  .sidebar-logo {
    margin-top: 30px;
    width: 100%;
    align-self: center;
  }

  .new-chat-btn {
    margin-top: 40px;
    display: flex;
    align-items: center;
    gap: 5px;
    background: none;
    border: none;
    color: white;
    text-decoration: none;
    font-size: 1rem;
    padding: 0;
  }

  .new-chat-btn:hover {
    cursor: pointer;
    opacity: 0.8;
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
    background: rgba(255, 255, 255, 0.3);
  }

  .chat-group {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .group-title {
    font-size: 0.75rem;
    font-weight: 600;
    color: #9ca3af;
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
    background-color: #2d2d2d;
  }

  .chat-item-wrapper.active {
    background-color: #3b3b3b;
  }

  .chat-title {
    flex: 1;
    min-width: 0;
    color: white;
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
    color: #9ca3af;
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
    color: #60a5fa;
  }

  .delete-btn {
    background: none;
    border: none;
    color: #9ca3af;
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
    color: #ff4444;
  }

  .rename-input {
    flex: 1;
    min-width: 0;
    width: 100%;
    background: #3b3b3b;
    border: 1px solid rgba(255, 255, 255, 0.3);
    border-radius: 0.25rem;
    color: white;
    padding: 0.25rem 0.5rem;
    font-size: 0.875rem;
    outline: none;
  }

  .info-section {
    margin-top: auto;
    margin-bottom: 30px;
    justify-self: flex-end;
  }

  .info-item {
    margin: 15px 0;
    display: flex;
    align-items: center;
    gap: 10px;

    color: white;
    text-decoration: none;
    font-weight: 500;
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
    color: #75fb7a;
  }

  .connecting-info {
    color: #9ca3af;
  }

  .version-text {
    font-size: 0.75rem;
    color: #9ca3af;
  }
</style>
