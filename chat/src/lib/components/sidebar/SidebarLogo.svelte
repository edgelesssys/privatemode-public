<script lang="ts">
  import sidebarLogoDark from '#lib/assets/logo-text.svg';
  import sidebarLogoLight from '#lib/assets/logo-text-light.svg';
  import sidebarLogoSmallDark from '#lib/assets/logo.svg';
  import sidebarLogoSmallLight from '#lib/assets/logo-light.svg';
  import { isDark } from '#lib/themeStore.js';

  export let collapsed: boolean;

  $: logo = $isDark ? sidebarLogoLight : sidebarLogoDark;
  $: logoSmall = $isDark ? sidebarLogoSmallLight : sidebarLogoSmallDark;
</script>

<div class="sidebar-logo">
  <img
    src={logoSmall}
    alt="Privatemode Logo"
    class="logo-small"
    class:logo-visible={collapsed}
    aria-hidden={!collapsed}
  />
  <img
    src={logo}
    alt="Privatemode Logo"
    class="logo-full"
    class:logo-visible={!collapsed}
    aria-hidden={collapsed}
  />
</div>

<style>
  .sidebar-logo {
    width: 100%;
    height: 68px;
    border-bottom: 1px solid var(--color-border);
    display: flex;
    align-items: center;
    justify-content: center;
    position: relative;
    flex-shrink: 0;
    overflow: hidden;
  }

  .sidebar-logo img {
    margin: 20px 0;
    position: absolute;
    opacity: 0;
    transition: opacity 0.2s ease;
    pointer-events: none;
  }

  .sidebar-logo img.logo-visible {
    opacity: 1;
    pointer-events: auto;
  }

  .sidebar-logo .logo-full {
    width: 85%;
  }

  .sidebar-logo img.logo-small {
    width: 28px;
  }
</style>
