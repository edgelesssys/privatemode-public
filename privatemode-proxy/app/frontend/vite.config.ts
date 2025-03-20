import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import dsv from '@rollup/plugin-dsv'

import purgecss from '@fullhuman/postcss-purgecss'

const plugins = [svelte(), dsv()]

// https://vitejs.dev/config/
export default defineConfig(({ command, mode, ssrBuild }) => {
  // Only run PurgeCSS in production builds
  if (command === 'build') {
    return {
      plugins,
      css: {
        postcss: {
          plugins: [
            purgecss({
              content: ['./**/*.html', './**/*.svelte', './**/*.ts', './**/*.js'],
              // Just use very broad safelist patterns to ensure no CSS is accidentally purged
              safelist: {
                standard: [
                  'pre',
                  'code',
                  'update-banner-container',
                  'sticky-header',
                  'side-bar-column',
                  'main-content-column',
                  'app-container',
                  'navbar',
                  'update-banner'
                ],
                // Use this for regex patterns
                deep: [/^navbar/, /^update-banner/, /svelte/]
              }
            })
          ]
        }
      },
      base: './'
    }
  } else {
    return {
      plugins
    }
  }
})
