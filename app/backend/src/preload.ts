import { contextBridge, ipcRenderer } from 'electron';

console.log('Preload script loaded');

contextBridge.exposeInMainWorld('electron', {
  getProxyPort: () => ipcRenderer.invoke('get-proxy-port'),
  getVersion: () => ipcRenderer.invoke('get-version'),
  getCurrentManifest: () => ipcRenderer.invoke('get-current-manifest'),
});
