<script lang="ts">
  import {
    LogIn,
    LogOut,
    CreditCard,
    Key,
    ChartColumn,
    MoreHorizontal,
  } from 'lucide-svelte';
  import Tooltip from '../common/Tooltip.svelte';
  import OrgSwitcher from './OrgSwitcher.svelte';
  import {
    isSignedIn,
    userDisplayName,
    userImageUrl,
    orgName,
    userOrganizations,
    signIn,
    signOut,
    clerkLoaded,
  } from '$lib/authStore';

  export let collapsed: boolean;

  let menuOpen = false;
  let menuContainerRef: HTMLDivElement | null = null;
  let menuButtonRef: HTMLButtonElement | null = null;
  let menuStyle = '';

  function updateMenuPosition() {
    if (!menuButtonRef) return;
    const rect = menuButtonRef.getBoundingClientRect();
    const left = rect.right - 160;
    const bottom = window.innerHeight - rect.top + 4;
    menuStyle = `position:fixed;left:${left}px;bottom:${bottom}px;width:160px;`;
  }

  function toggleMenu() {
    menuOpen = !menuOpen;
    if (menuOpen) updateMenuPosition();
  }

  function handleClickOutside(event: MouseEvent) {
    if (menuContainerRef && !menuContainerRef.contains(event.target as Node)) {
      menuOpen = false;
    }
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && menuOpen) {
      menuOpen = false;
      menuButtonRef?.focus();
    }
  }

  function handleSignOut() {
    menuOpen = false;
    signOut();
  }
</script>

<svelte:window
  on:click={handleClickOutside}
  on:keydown={handleKeydown}
/>

<div class="auth-section">
  <div class="auth-section-content">
    {#if !$clerkLoaded}
      <div
        class="auth-skeleton"
        class:collapsed-auth={collapsed}
      >
        <div class="skeleton-avatar"></div>
        <div
          class="skeleton-text collapsible-text"
          class:hidden={collapsed}
        ></div>
      </div>
    {:else if $isSignedIn}
      <div
        class="auth-user"
        class:collapsed-auth={collapsed}
      >
        {#if $userImageUrl}
          <img
            src={$userImageUrl}
            alt="Profile"
            class="auth-avatar"
            title={collapsed ? ($userDisplayName ?? '') : ''}
          />
        {:else}
          <div
            class="auth-avatar-placeholder"
            title={collapsed ? ($userDisplayName ?? '') : ''}
          ></div>
        {/if}
        <div
          class="auth-user-info collapsible-text"
          class:hidden={collapsed}
        >
          <span class="auth-user-name">{$userDisplayName}</span>
          {#if $userOrganizations.length > 1}
            <OrgSwitcher />
          {:else if $orgName}
            <span class="org-subtitle-static">{$orgName}</span>
          {/if}
        </div>
        <div
          class="auth-menu collapsible-text"
          class:hidden={collapsed}
          bind:this={menuContainerRef}
        >
          <Tooltip text={menuOpen ? '' : 'Account menu'}>
            <button
              class="auth-menu-btn"
              type="button"
              bind:this={menuButtonRef}
              on:click={toggleMenu}
              aria-label="Account menu"
              aria-haspopup="true"
              aria-expanded={menuOpen}
            >
              <MoreHorizontal size={16} />
            </button>
          </Tooltip>
          {#if menuOpen}
            <div
              class="auth-menu-dropdown"
              style={menuStyle}
              role="menu"
            >
              <a
                class="auth-menu-item"
                href="https://portal.privatemode.ai/usage"
                target="_blank"
                rel="noopener noreferrer"
                role="menuitem"
                on:click={() => (menuOpen = false)}
              >
                <ChartColumn size={14} />
                <span>Usage</span>
              </a>
              <a
                class="auth-menu-item"
                href="https://portal.privatemode.ai/billing"
                target="_blank"
                rel="noopener noreferrer"
                role="menuitem"
                on:click={() => (menuOpen = false)}
              >
                <CreditCard size={14} />
                <span>Billing</span>
              </a>
              <button
                class="auth-menu-item"
                type="button"
                role="menuitem"
                on:click={handleSignOut}
              >
                <LogOut size={14} />
                <span>Sign out</span>
              </button>
            </div>
          {/if}
        </div>
      </div>
    {:else}
      <button
        class="info-item auth-btn sign-in"
        type="button"
        on:click={signIn}
        title={collapsed ? 'Sign in' : ''}
      >
        <LogIn size={18} />
        <span
          class="collapsible-text"
          class:hidden={collapsed}>Sign in</span
        >
      </button>
    {/if}
  </div>
</div>

<style>
  .auth-section {
    border-top: 1px solid var(--color-border);
  }

  .auth-section-content {
    width: 85%;
    margin: 0 auto;
    padding: 15px 0;
    transition: width 0.25s ease;
  }

  :global(.sidebar.collapsed) .auth-section-content {
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

  .org-subtitle-static {
    font-size: 0.8125rem;
    font-weight: 500;
    color: var(--color-text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .auth-menu {
    position: relative;
    flex-shrink: 0;
  }

  .auth-menu-btn {
    background: none;
    border: none;
    color: var(--color-text-secondary);
    cursor: pointer;
    padding: 0.25rem;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 0.25rem;
    transition:
      color 0.2s,
      background-color 0.2s;
  }

  .auth-menu-btn:hover {
    color: var(--color-text-primary);
    background-color: var(--color-bg-hover);
  }

  .auth-menu-dropdown {
    background-color: var(--color-dropdown-bg);
    border: 1px solid var(--color-dropdown-border);
    border-radius: 0.375rem;
    box-shadow: var(--shadow-md);
    z-index: 1000;
    padding: 4px;
    display: flex;
    flex-direction: column;
  }

  .auth-menu-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    box-sizing: border-box;
    background: none;
    border: none;
    border-radius: 0.25rem;
    color: var(--color-dropdown-text);
    font-size: 0.8125rem;
    font-weight: 500;
    padding: 6px 8px;
    cursor: pointer;
    font-family: 'Inter Variable', sans-serif;
    text-decoration: none;
    text-align: left;
    transition: background-color 0.2s;
  }

  .auth-menu-item:hover {
    background-color: var(--color-dropdown-hover);
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

  :global(.sidebar.collapsed) .info-item {
    justify-content: center;
    gap: 0;
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
</style>
