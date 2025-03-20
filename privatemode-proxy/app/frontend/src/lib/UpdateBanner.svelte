<script lang="ts">
  import { onMount } from 'svelte'
  import { compareVersions } from 'compare-versions'

  // Download link for updates
  const downloadLink = 'https://www.privatemode.ai/download-app'
  
  // Update notification variables
  let showUpdateBanner = false
  let updateMessage = ''
  let latestVersion = ''
  // Get current version from environment variable
  const APP_VERSION = import.meta.env.VITE_VERSION || 'v0.0.0'

  // Function to check for updates by fetching from the API
  async function checkForUpdates () {
    try {
      console.log('Current app version:', APP_VERSION)
      const response = await fetch('https://cdn.confidential.cloud/privatemode/v2/motd.json')
      if (response.ok) {
        const data = await response.json()
        console.log('Latest version from API:', data.latestVersion)
        latestVersion = data.latestVersion
  
        // Compare versions to determine if update is needed
        const comparison = compareVersions(APP_VERSION, latestVersion)
        console.log('Version comparison result:', comparison, '(negative means update needed)')
  
        if (comparison < 0) {
          // Include version information in the update message
          updateMessage = `${data.outdatedMsg}`
          showUpdateBanner = true
          console.log('Update needed, showing banner with message:', updateMessage)
        } else {
          console.log('App is up to date, no banner needed')
          showUpdateBanner = false
        }
      }
    } catch (error) {
      console.error('Failed to check for updates:', error)
    }
  }

  onMount(async () => {
    // Check for updates immediately
    await checkForUpdates()
  
    // Schedule update checks every 24 hours
    setInterval(checkForUpdates, 24 * 60 * 60 * 1000) // 24 hours in milliseconds
  })
</script>

<style>
  /* Banner styles */
  .update-banner-container {
    background-color: rgb(235, 228, 254) !important;
    color: rgb(122, 73, 246);
    text-align: center;
    padding: 0.5rem;
    width: 100%;
    box-sizing: border-box;
    position: relative;
    z-index: 40; /* Lower than navbar (45) and sidebar (50) */
    left: 0;
    right: 0;
  }

  /* Desktop styles */
  @media (min-width: 769px) {
    .update-banner-container {
      margin-left: 250px;
      width: calc(100% - 250px); /* Adjust width to account for sidebar */
    }
  }

  .update-banner-container a {
    color: rgb(122, 73, 246);
    font-weight: bold;
    text-decoration: none;
  }

  .update-banner-container a:hover {
    text-decoration: underline;
  }
</style>

{#if showUpdateBanner}
<div class="update-banner-container update-banner">
  <span class="is-size-7">{updateMessage} <a href={downloadLink} target="_blank" rel="noopener noreferrer">Download {latestVersion}</a></span>
</div>
{/if} 
