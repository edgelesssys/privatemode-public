<script lang="ts">
  import { params } from 'svelte-spa-router'
  import { pinMainMenu } from './Storage.svelte'
  import burger from '../assets/burger.svg'
import logo from '../assets/privatemode-black.svg'

$: activeChatId = $params && $params.chatId ? parseInt($params.chatId) : undefined
</script>

<nav class="navbar" aria-label="main navigation">
	<div class="navbar-brand chat-navbar" style="display: flex; width: 100%;">
		<!-- Left section with menu button -->
		<div class="navbar-item menu-button" style="flex: 0 0 auto;">
      <button class="button" on:click|stopPropagation={() => { $pinMainMenu = true }}>
				<img width="23" height="14" src={burger} alt="open menu icon" />
      </button>
    </div>
    
    <!-- Center section with logo -->
    <div style="flex: 1; display: flex; justify-content: center; align-items: center;">
      <a class="navbar-item" href={'#/'}>
        <img src={logo} alt="Privatemode" width="140" height="22" style="max-width: 100%; height: auto;" />
      </a>
    </div>
    
    <!-- Right section (empty for balance) -->
    <div style="flex: 0 0 auto; width: 23px;"></div>
  </div>
</nav>

<style>
	.navbar-item-text {
		overflow: hidden;
		text-overflow: ellipsis;
	}
	
	.navbar {
		transition: top 0.3s ease;
		position: relative; /* Changed from sticky since parent is now sticky */
		z-index: 45; /* Higher than banner (40) but lower than sidebar (50) */
		background-color: white; /* Ensure it's not transparent */
	}
</style>
