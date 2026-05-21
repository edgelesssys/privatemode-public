<script lang="ts">
  import { LogIn, LogOut } from 'lucide-svelte';
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
</script>

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
        <Tooltip text="Sign out">
          <button
            class="auth-sign-out-btn collapsible-text"
            class:hidden={collapsed}
            type="button"
            on:click={signOut}
          >
            <LogOut size={16} />
          </button>
        </Tooltip>
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

  .auth-sign-out-btn {
    background: none;
    border: none;
    color: var(--color-text-secondary);
    cursor: pointer;
    padding: 0.25rem;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    opacity: 0;
    transition: opacity 0.2s;
  }

  .auth-user:hover .auth-sign-out-btn {
    opacity: 1;
  }

  .auth-sign-out-btn:hover {
    color: var(--color-text-primary);
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
