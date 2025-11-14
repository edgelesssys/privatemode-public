import type { ForgeConfig } from '@electron-forge/shared-types';
import { MakerDeb } from '@electron-forge/maker-deb';
import { MakerRpm } from '@electron-forge/maker-rpm';
import { MakerDMG } from '@electron-forge/maker-dmg';
import { MakerMSIX } from '@electron-forge/maker-msix';
import { VitePlugin } from '@electron-forge/plugin-vite';
import { FusesPlugin } from '@electron-forge/plugin-fuses';
import { FuseV1Options, FuseVersion } from '@electron/fuses';
import * as path from 'path';

const libPath = path.join(
  require('./package.json').config.libprivatemode,
  'lib',
);

const config: ForgeConfig = {
  packagerConfig: {
    asar: {
      unpack: '*.node',
    },
    icon: 'src/assets/icons/icon',
    extraResource: [
      'build',
      'src/renderer',
      'src/assets/icons/icon.png',
      libPath,
    ],
    appCopyright: `Edgeless Systems GmbH, ${new Date().getFullYear()}`,
    osxSign: process.env.PRIVATEMODE_SIGN_APP === '1' ? {} : undefined,
    osxNotarize:
      process.env.PRIVATEMODE_SIGN_APP === '1'
        ? {
            appleId: process.env.PRIVATEMODE_APPLE_ID,
            appleIdPassword: process.env.PRIVATEMODE_APPLE_PASSWORD,
            teamId: process.env.PRIVATEMODE_APPLE_TEAM_ID,
          }
        : undefined,
    // Contrary to the Electron-Forge documentation, on MacOS,
    // setting the executable name makes the .app bundle name
    // use it and be lowercase, which is against macOS conventions.
    // Therefore, we only set it on other platforms.
    ...(process.platform !== 'darwin' && {
      executableName: 'privatemode',
      name: 'Privatemode',
    }),
  },
  hooks: {
    packageAfterCopy: async (_config, buildPath) => {
      const { spawnSync } = require('child_process');
      spawnSync('node-gyp', ['rebuild'], {
        cwd: buildPath,
        stdio: 'inherit',
        shell: true,
      });
    },
  },
  makers: [
    new MakerDeb({
      options: {
        icon: 'src/assets/icons/icon.png',
      },
    }),
    new MakerRpm({
      options: {
        icon: 'src/assets/icons/icon.png',
      },
    }),
    new MakerDMG({
      name: 'Privatemode',
      format: 'ULFO',
      icon: 'src/assets/icons/icon.icns',
      background: 'src/assets/dmg-background.png',
    }),
    new MakerMSIX({
      packageAssets: 'src/assets/msix',
      manifestVariables: {
        publisher:
          'CN=Edgeless Systems GmbH, O=Edgeless Systems GmbH, L=Bochum, C=DE',
        publisherDisplayName: 'Edgeless Systems',
        packageDisplayName: 'Privatemode',
        packageDescription: 'Desktop App for Privatemode.',
        packageBackgroundColor: '#7A49F6',
        appDisplayName: 'Privatemode',
      },
    }),
  ],
  plugins: [
    new VitePlugin({
      // `build` can specify multiple entry builds, which can be Main process, Preload scripts, Worker process, etc.
      // If you are familiar with Vite configuration, it will look really familiar.
      build: [
        {
          // `entry` is just an alias for `build.lib.entry` in the corresponding file of `config`.
          entry: 'src/main.ts',
          config: 'vite.main.config.ts',
          target: 'main',
        },
        {
          entry: 'src/preload.ts',
          config: 'vite.preload.config.ts',
          target: 'preload',
        },
      ],
      renderer: [
        {
          name: 'main_window',
          config: 'vite.renderer.config.ts',
        },
      ],
    }),
    // Fuses are used to enable/disable various Electron functionality
    // at package time, before code signing the application
    new FusesPlugin({
      version: FuseVersion.V1,
      [FuseV1Options.RunAsNode]: false,
      [FuseV1Options.EnableCookieEncryption]: true,
      [FuseV1Options.EnableNodeOptionsEnvironmentVariable]: false,
      [FuseV1Options.EnableNodeCliInspectArguments]: false,
      [FuseV1Options.EnableEmbeddedAsarIntegrityValidation]: true,
      [FuseV1Options.OnlyLoadAppFromAsar]: true,
    }),
  ],
};

export default config;
