<script lang="ts">
  import { apiKeyStorage, lastChatId, getChat, started, checkStateChange } from './Storage.svelte'
  import Footer from './Footer.svelte'
  import { replace } from 'svelte-spa-router'
  import { afterUpdate, onMount } from 'svelte'
  import { set as setOpenAI } from './providers/openai/util.svelte'
  import { hasActiveModels } from './Models.svelte'
  import { getProfiles } from './Profiles.svelte'
  import { isNativeApp } from './Util.svelte'
  import { SmokeTest } from './SmokeTest'
  import { faCheck } from '@fortawesome/free-solid-svg-icons'
  import Fa from 'svelte-fa'

  import { GetConfiguredAPIKey } from '../../wailsjs/go/main/ConfigurationService'

  $: apiKey = $apiKeyStorage
  let hasModels = hasActiveModels()

  const initApiKey = async () => {
    // In a native app try to read it from the config file
    // but only if not already set or an empty string (so the
    // user can recover the key from the config file).
    if (!isNativeApp || apiKey) {
      return
    }

    const configuredKey = await GetConfiguredAPIKey()
    if (configuredKey) {
      setOpenAI({ apiKey: configuredKey })
    }
  }

  onMount(async () => {
    if (!$started) {
      $started = true
      await initApiKey()
      if (hasActiveModels() && getChat($lastChatId)) {
        const chatId = $lastChatId
        $lastChatId = 0
        replace(`/chat/${chatId}`)
      }

      // Note: this will not return but exit the app if running the smoke test
      await SmokeTest.runIfActivated()
    }
    $lastChatId = 0
  })

  afterUpdate(() => {
    hasModels = hasActiveModels()
    $checkStateChange++
  })

  async function handleSubmit (e: SubmitEvent) {
    let val = ''
    if (e.target && (e.target as HTMLFormElement)[0]) {
      val = ((e.target as HTMLFormElement)[0] as HTMLInputElement).value.trim()
    }
    setOpenAI({ apiKey: val })
    hasModels = hasActiveModels()

    // Force model update after api key change
    if (apiKey) {
      await getProfiles(true)
    }
  }
</script>

<section class="section">
  <article class="message">
    <div class="message-body">
      <p>
      In order to chat with Privatemode, you need to set your access key below. Don't have a key yet?
      <a href="https://privatemode.ai/sign-up-app"
         rel="noopener noreferrer">
        Request one here
      </a>.
    </p>
    </div>
  </article>
  <article class="message" class:is-danger={!hasModels} class:is-warning={!apiKey} class:is-info={apiKey}>
    <div class="message-body">
      <p class="is-size-8 mb-4">
        Enter your access key below{#if isNativeApp}&nbsp;or set it in the configuration file{/if}.
      </p>
      <form 
        class="field has-addons has-addons-right"
        on:submit|preventDefault={handleSubmit}
      >
        <p class="control is-expanded">
          <input
            aria-label="Privatemode access key"
            placeholder="Your access key"
            autocomplete="off"
            autocorrect="off"
            autocapitalize="off"
            class="input"
            class:is-danger={!hasModels}
            class:is-warning={!apiKey}
            class:is-info={apiKey}
            value={apiKey}
          />
        </p>
        <p class="control">
          <button class="button is-info" type="submit">
            Save

            {#if apiKey}
              <span class="position-absolute top-0 start-0 h-100 w-100 button is-info z-2 animation-fade-out">
                <Fa icon={faCheck} />
              </span>
            {/if}
          </button>
        </p>
      </form>
    </div>
  </article>

  {#if apiKey}
    <article class="message is-info">
      <div class="message-body">
        Select an existing chat on the sidebar, or
        <a href={'#/chat/new'}>create a new chat</a>
      </div>
    </article>
  {/if}
</section>
<Footer pin={true} />
