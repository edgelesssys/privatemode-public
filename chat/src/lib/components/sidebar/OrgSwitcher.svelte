<script lang="ts">
  import { ChevronDown, Check } from 'lucide-svelte';
  import {
    orgName,
    orgId,
    userOrganizations,
    switchOrganization,
  } from '$lib/authStore';
  import { t } from '$lib/i18n';

  let open = false;
  let containerRef: HTMLDivElement | null = null;
  let buttonRef: HTMLButtonElement | null = null;
  let dropdownStyle = '';

  function updatePosition() {
    if (!buttonRef) return;
    const rect = buttonRef.getBoundingClientRect();
    const left = rect.left;
    const bottom = window.innerHeight - rect.top + 4;
    const width = Math.max(rect.width, 180);
    dropdownStyle = `position:fixed;left:${left}px;bottom:${bottom}px;width:${width}px;`;
  }

  function toggle() {
    open = !open;
    if (open) updatePosition();
  }

  function handleClickOutside(event: MouseEvent) {
    if (containerRef && !containerRef.contains(event.target as Node)) {
      open = false;
    }
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && open) {
      open = false;
      buttonRef?.focus();
    }
  }

  function pickOrg(id: string) {
    if (id !== $orgId) {
      switchOrganization(id).catch((err) => {
        console.error('Failed to switch organization:', err);
      });
    }
    open = false;
  }
</script>

<svelte:window
  on:click={handleClickOutside}
  on:keydown={handleKeydown}
/>

<div
  class="org-switcher"
  bind:this={containerRef}
>
  <button
    class="org-subtitle"
    type="button"
    aria-expanded={open}
    bind:this={buttonRef}
    on:click={toggle}
  >
    <span class="org-subtitle-name">{$orgName ?? $t('sidebar.personal')}</span>
    <span
      class="org-chevron"
      class:open
    >
      <ChevronDown size={12} />
    </span>
  </button>
  {#if open}
    <div
      class="org-switcher-dropdown"
      style={dropdownStyle}
    >
      {#each $userOrganizations as org (org.id)}
        <button
          class="org-switcher-item"
          class:active={org.id === $orgId}
          type="button"
          on:click={() => pickOrg(org.id)}
        >
          {#if org.imageUrl}
            <img
              class="org-switcher-item-img"
              src={org.imageUrl}
              alt={org.name}
            />
          {:else}
            <span class="org-switcher-item-initials"
              >{org.name.charAt(0).toUpperCase()}</span
            >
          {/if}
          <span class="org-switcher-item-name">{org.name}</span>
          {#if org.id === $orgId}
            <Check size={14} />
          {/if}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .org-switcher {
    position: relative;
  }

  .org-subtitle {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    max-width: 100%;
    background: none;
    border: none;
    color: var(--color-text-secondary);
    font-size: 0.8125rem;
    font-weight: 500;
    padding: 0;
    cursor: pointer;
    font-family: 'Inter Variable', sans-serif;
  }

  .org-subtitle:hover {
    color: var(--color-text-primary);
  }

  .org-subtitle-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .org-chevron {
    display: inline-flex;
    transition: transform 0.2s ease;
  }

  .org-chevron.open {
    transform: rotate(180deg);
  }

  .org-switcher-dropdown {
    background-color: var(--color-dropdown-bg);
    border: 1px solid var(--color-dropdown-border);
    border-radius: 0.375rem;
    box-shadow: var(--shadow-md);
    z-index: 1000;
    max-height: 200px;
    overflow-y: auto;
    padding: 4px;
  }

  .org-switcher-item {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    background: none;
    border: none;
    border-radius: 0.25rem;
    color: var(--color-dropdown-text);
    font-size: 0.8125rem;
    font-weight: 500;
    padding: 6px 8px;
    cursor: pointer;
    font-family: 'Inter Variable', sans-serif;
    transition: background-color 0.2s;
  }

  .org-switcher-item:hover {
    background-color: var(--color-dropdown-hover);
  }

  .org-switcher-item.active {
    color: var(--color-dropdown-accent);
  }

  .org-switcher-item-img {
    width: 20px;
    height: 20px;
    border-radius: 4px;
    object-fit: cover;
    flex-shrink: 0;
  }

  .org-switcher-item-initials {
    width: 20px;
    height: 20px;
    border-radius: 4px;
    background-color: var(--color-bg-hover);
    color: var(--color-text-secondary);
    font-size: 0.6875rem;
    font-weight: 600;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .org-switcher-item-name {
    flex: 1;
    min-width: 0;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
