export interface ElectronAPI {
  getProxyPort: () => Promise<string>;
  getVersion: () => Promise<string>;
  getCurrentManifest: () => Promise<string>;
}

declare global {
  interface Window {
    electron: ElectronAPI;
  }
}
