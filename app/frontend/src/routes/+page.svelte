<script lang="ts">
  import ChatInput from '$lib/components/ChatInput.svelte';
  import ChatPanel from '$lib/components/ChatPanel.svelte';
  import UpdateBanner from '$lib/components/UpdateBanner.svelte';
  import { chatStore } from '$lib/chatStore';
  import { proxyPort as proxyPortStore } from '$lib/proxyStore';

  let proxyPort: string;
  let currentChatId: string | null = null;

  window.electron.getProxyPort().then((port) => {
    proxyPort = port;
    proxyPortStore.set(port);
    console.log('Received proxy port:', proxyPort);
  });

  chatStore.currentChatId.subscribe((id) => {
    currentChatId = id;
  });

  function handleChatCreated(chatId: string) {
    chatStore.currentChatId.set(chatId);
  }
</script>

<UpdateBanner />
<div class="content">
  <ChatPanel chatId={currentChatId} />
  <ChatInput
    {proxyPort}
    {currentChatId}
    onChatCreated={handleChatCreated}
  />
</div>

<style>
  .content {
    width: 80%;
    height: 100vh;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
  }
</style>
