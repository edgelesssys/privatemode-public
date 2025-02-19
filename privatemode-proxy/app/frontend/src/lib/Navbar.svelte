<script lang="ts">
  import { params } from 'svelte-spa-router'
  import { pinMainMenu } from './Storage.svelte'
  import logo from '../assets/logo.svg'
  import ChatOptionMenu from './ChatOptionMenu.svelte'
	import burger from "../assets/burger.svg";
  import { faBars, faXmark } from '@fortawesome/free-solid-svg-icons/index'

$: activeChatId = $params && $params.chatId ? parseInt($params.chatId) : undefined
</script>

<nav class="navbar position-sticky top-0" aria-label="main navigation">
	<div class="navbar-brand chat-navbar">
		<div class="navbar-item menu-button">
			
      <!-- {#if $pinMainMenu}
      <button class="button" on:click|stopPropagation={() => { $pinMainMenu = false }}>
        <span class="icon">
          <Fa icon={faXmark} />
        </span>
      </button>
      {:else} -->
      <button class="button" on:click|stopPropagation={() => { $pinMainMenu = true }}>
				<img width="23" height="14" src={burger} alt="open menu icon" />
      </button>
      <!-- {/if} -->
    </div>
		<a class="navbar-item flex-shrink-1" href={'#/'} style="min-width: 0;">
			<p class="is-size-6 has-text-weight-medium navbar-item-text"
				>Confidential chat with secure connection.</p
			>
		</a>
		<!-- <div class="chat-option-menu navbar-item is-pulled-right">
      <ChatOptionMenu bind:chatId={activeChatId} />
    </div> -->
	</div>
</nav>

<style>
	.navbar-item-text {
		overflow: hidden;
		text-overflow: ellipsis;
	}
</style>
