<script lang="ts">
  import { params } from 'svelte-spa-router'
  import ChatMenuItem from './ChatMenuItem.svelte'
  import { chatsStorage, pinMainMenu, checkStateChange, getChatSortOption, deleteAllChats } from './Storage.svelte'
  import logo from '../assets/logo.svg'
  import { clickOutside } from 'svelte-use-click-outside'
  import { startNewChatWithWarning } from './Util.svelte'
  import { hasActiveModels } from './Models.svelte'
  import plus from '../assets/plus.svg'
  import trash from '../assets/trash-white.svg'
  import docs from '../assets/docs.svg'
  import support from '../assets/support.svg'
  import { replace } from 'svelte-spa-router'
  import settings from '../assets/settings.svg'
  import security from '../assets/security.svg'
  import { TestChatController } from './SmokeTest'
  $: sortedChats = $chatsStorage.sort(getChatSortOption().sortFn)
  $: activeChatId = $params && $params.chatId ? parseInt($params.chatId) : undefined

  let sortOption = getChatSortOption()
  let hasModels = hasActiveModels()
  // const showWaitlist = writable(false)

  // const openWaitlist = () => {
  //   showWaitlist.set(true)
  // }

  const onStateChange = (...args:any) => {
    sortOption = getChatSortOption()
    sortedChats = $chatsStorage.sort(sortOption.sortFn)
    hasModels = hasActiveModels()
  }

  $: onStateChange($checkStateChange)

  const delAllChats = () => {
    replace('/').then(() => {
      deleteAllChats()
    })
  }

  let newChatButton: HTMLButtonElement | null = null

  TestChatController.newChat = async () => {
    if (newChatButton && !newChatButton.disabled) {
      newChatButton.click()
    } else {
      throw new Error('New chat button is not available or disabled')
    }
  }

  TestChatController.deleteActiveChat = async () => {
    const index = $chatsStorage.findIndex(c => c.id === activeChatId)
    if (index !== -1) {
      chatsStorage.update(chats => {
        chats.splice(index, 1)
        return chats
      })
    } else {
      throw new Error(`Chat with id ${activeChatId} not found`)
    }
  }

</script>

<aside class="menu main-menu" class:pinned={$pinMainMenu} use:clickOutside={() => { $pinMainMenu = false }}>
  <div class="menu-expanse">
      <div class="menu-nav-bar">
        <span class="navbar-item gpt-logo">
          <img src={logo} alt="Conntinuum AI" width="180" height="22" />
        </span>
      </div>
      <div class="level-right">
        <div class="level-item">
          <button 
            bind:this={newChatButton}
            on:click={async () => { $pinMainMenu = false; await startNewChatWithWarning(activeChatId) }} class="panel-block button" title="Start new chat with default profile" class:is-disabled={!hasModels}>
						<img src={plus} alt="add new chat" width="11" height="11" class="mr-2" />
						New chat
		  </button>
        </div>
      </div>
      {#if sortedChats.length > 1}
      <p class="previous-text">Previous</p>
    {/if}
    <ul class="menu-list menu-expansion-list">
      {#if sortedChats.length === 0}
        <li><a href={'#'} class="is-disabled" style="color: white; opacity: 0.6;">No chats yet...</a></li>
      {:else}
        {#key $checkStateChange}
        {#each sortedChats as chat, i}
        {#key chat.id}
        <ChatMenuItem activeChatId={activeChatId} chat={chat} prevChat={sortedChats[i - 1]} nextChat={sortedChats[i + 1]} />
        {/key}
        {/each}
        {/key}
      {/if}
    </ul>

    <div class="side-info-block">
    <div class="flex attestation-link">
      <img src={security} alt="security shield" width="20" height="20" />
      <p>Your session is secure</p>
    </div>
    {#if sortedChats.length > 1}
      <a
        href="#/"
        class="flex"
        on:click|preventDefault={() => delAllChats()}
      >
        <img src={trash} alt="clear icon" width="20" height="20" />
        <p>Clear conversations</p>
      </a>
    {/if}
    <a
      href="#/"
      class="flex"
    >
      <img src={settings} alt="key icon" width="20" height="20" />
      <p>Settings</p>
    </a>
    <a
      href="https://docs.privatemode.ai/guides/desktop-app"
      class="flex"
      target="_blank"
      rel="noopener noreferrer"
    >
      <img src={docs} alt="docs icon" width="20" height="20" />
      <p>Documentation</p>
    </a>
    <a
      href="https://www.privatemode.ai/contact"
      class="flex"
      target="_blank"
      rel="noopener noreferrer"
    >
      <img src={support} alt="support icon" width="20" height="20" />
      <p>Need help?</p>
    </a>
    </div>
  </div>
</aside>
<div
  class="modal-backdrop fade show {$pinMainMenu ? 'd-block' : 'd-none'}"
  style="z-index: 31;"
></div>

<style>
  .attestation-link p {
    color: #75FB7A !important;
  }

  .attestation-link:hover {
    /* remove the background styling of .side-info-block */
    background: rgb(0, 0, 0, 0) !important;
  }
</style>
