<script lang="ts">
  import ChatListItem from './ChatListItem.svelte';
  import { chatStore } from '$lib/chatStore';
  import type { Chat } from '$lib/chatStore';

  export let currentChatId: string | null;
  export let collapsed: boolean;
  export let onSelectChat: (chatId: string) => void;

  let renamingChatId: string | null = null;
  let renameValue: string = '';

  interface GroupedChats {
    today: Chat[];
    yesterday: Chat[];
    lastWeek: Chat[];
    older: Chat[];
  }

  function groupChatsByDate(chats: Chat[]): GroupedChats {
    const now = new Date();
    const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const yesterday = new Date(
      now.getFullYear(),
      now.getMonth(),
      now.getDate() - 1,
    );
    const lastWeek = new Date(
      now.getFullYear(),
      now.getMonth(),
      now.getDate() - 7,
    );

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

  function handleDelete(event: MouseEvent, chatId: string) {
    event.stopPropagation();
    if (confirm('Are you sure you want to delete this chat?')) {
      chatStore.deleteChat(chatId);
    }
  }

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

<div
  class="chats-section"
  class:hidden={collapsed}
  inert={collapsed}
>
  {#each chatGroups as group (group.label)}
    <div class="chat-group">
      <h3 class="group-title">{group.label}</h3>
      {#each group.chats as chat (chat.id)}
        <ChatListItem
          {chat}
          {collapsed}
          active={currentChatId === chat.id}
          editing={renamingChatId === chat.id}
          bind:renameValue
          onSelect={() => onSelectChat(chat.id)}
          onStartRename={(e) => startRename(e, chat.id, chat.title)}
          onConfirmRename={() => confirmRename(chat.id)}
          onCancelRename={cancelRename}
          onDelete={(e) => handleDelete(e, chat.id)}
        />
      {/each}
    </div>
  {/each}
</div>

<style>
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

  .chats-section.hidden {
    opacity: 0;
    pointer-events: none;
  }

  .chats-section::-webkit-scrollbar {
    width: 4px;
  }

  .chats-section::-webkit-scrollbar-thumb {
    border-radius: 2px;
    background: transparent;
    transition: background 0.2s;
  }

  :global(.sidebar:hover) .chats-section::-webkit-scrollbar-thumb {
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
</style>
