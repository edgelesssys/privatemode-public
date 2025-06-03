<script lang="ts">
  import Router, { location, querystring } from 'svelte-spa-router'
  import { wrap } from 'svelte-spa-router/wrap'
  import { onMount } from 'svelte'
  import { isNativeApp } from './lib/Util.svelte'

  import Navbar from './lib/Navbar.svelte'
  import Sidebar from './lib/Sidebar.svelte'
  import Home from './lib/Home.svelte'
  import Chat from './lib/Chat.svelte'
  import NewChat from './lib/NewChat.svelte'
  import ToastContainer from './lib/ToastContainer.svelte'
  import { chatsStorage, setGlobalSettingValueByKey } from './lib/Storage.svelte'
  import { Modals, closeModal } from 'svelte-modals'
  import { dispatchModalEsc, checkModalEsc } from './lib/Util.svelte'
  import { set as setOpenAI } from './lib/providers/openai/util.svelte'
  import { hasActiveModels } from './lib/Models.svelte'
  import UpdateBanner from './lib/UpdateBanner.svelte'

  // Check if the API key is passed in as a "key" query parameter - if so, save it
  // Example: https://niek.github.io/chatgpt-web/#/?key=sk-...
  const urlParams: URLSearchParams = new URLSearchParams($querystring)
  if (urlParams.has('key')) {
    setOpenAI({ apiKey: urlParams.get('key') as string })
  }
  if (urlParams.has('petals')) {
    console.log('enablePetals')
    setGlobalSettingValueByKey('enablePetals', true)
  }

  // The definition of the routes with some conditions
  const routes = {
    '/': Home,

    '/chat/new': wrap({
      component: NewChat,
      conditions: () => {
        return hasActiveModels()
      }
    }),

    '/chat/:chatId': wrap({
      component: Chat,
      conditions: (detail) => {
        return $chatsStorage.find((chat) => chat.id === parseInt(detail?.params?.chatId as string)) !== undefined
      }
    }),

    '*': Home
  }

  const onLocationChange = (...args:any) => {
    // close all modals on route change
    dispatchModalEsc()
  }

  $: onLocationChange($location)

  // Set up global link handler
  onMount(() => {
    if (isNativeApp) {
      document.body.addEventListener('click', function (e) {
        const anchor = (e.target as HTMLElement).closest('a') as HTMLAnchorElement
        if (!anchor?.href) return

        const isExternal = anchor.href.startsWith('http') && !anchor.href.includes('wails.localhost') && !anchor.href.includes('127.0.0.1')
        if (isExternal || anchor.target === '_blank') {
          e.preventDefault();
          (window as any).runtime.BrowserOpenURL(anchor.href)
        }
      })
    }
  })
</script>

<div class="app-container">
  <div class="sticky-header">
    <UpdateBanner />
    <Navbar />
  </div>
  <div class="side-bar-column">
    <Sidebar />
  </div>
  <div class="main-content-column" id="content">
    {#key $location}
      <Router {routes} />
    {/key}
  </div>
  <ToastContainer />
</div>

<Modals>
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <div
    slot="backdrop"
    class="backdrop"
    on:click={closeModal}
  />
</Modals>

<svelte:window
  on:keydown={(e) => checkModalEsc(e)}
/>

<style>
  .backdrop {
    position: fixed;
    top: 0;
    bottom: 0;
    right: 0;
    left: 0;
    background: transparent
  }
  
  /* App container styles */
  .app-container {
    display: flex;
    flex-direction: column;
    min-height: 100vh;
    width: 100%;
  }
  
  /* Sticky header container */
  .sticky-header {
    position: sticky;
    top: 0;
    z-index: 40; /* Lower than sidebar (which is 50+) */
    width: 100%;
    display: flex;
    flex-direction: column;
  }
  
  /* Ensure sidebar column has proper z-index */
  .side-bar-column {
    z-index: 50; /* Higher than sticky header */
  }
</style>
