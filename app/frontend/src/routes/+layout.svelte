<script lang="ts">
  import Sidebar from '$lib/components/Sidebar.svelte';
  import Setup from '$lib/components/Setup.svelte';
  import { getApiKey } from '$lib/apiKey';
  import { chatStore } from '$lib/chatStore';
  import '@fontsource-variable/inter/wght.css';
  let { children } = $props();

  let hasApiKey = $state(false);
  let currentChatId = $state<string | null>(null);

  $effect(() => {
    hasApiKey = !!getApiKey();
  });

  chatStore.currentChatId.subscribe((id) => {
    currentChatId = id;
  });

  function handleNewChat() {
    const chatId = chatStore.createChat();
    chatStore.currentChatId.set(chatId);
  }

  function handleSelectChat(chatId: string) {
    chatStore.currentChatId.set(chatId);
  }
</script>

<svelte:head></svelte:head>

{#if hasApiKey}
  <main>
    <Sidebar
      {currentChatId}
      onNewChat={handleNewChat}
      onSelectChat={handleSelectChat}
    />
    <div class="content">
      {@render children?.()}
    </div>
  </main>
{:else}
  <Setup />
{/if}

<style>
  :global(body) {
    font-family: 'Inter Variable', sans-serif;
    margin: 0;
    overflow: hidden;
  }

  :global(h1, h2, h3, h4, h5, h6) {
    font-family: 'Inter Variable', sans-serif;
    font-weight: 600;
  }

  .content {
    margin-left: 250px;
    background-color: #f3f4f5;
  }
</style>
