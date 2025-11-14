import { app, BrowserWindow, shell, ipcMain, dialog } from 'electron';
import path from 'node:path';
import started from 'electron-squirrel-startup';
import { startProxy } from './privatemode';
import serve from 'electron-serve';
import packageJson from '../package.json';

let loadStaticFiles: ReturnType<typeof serve>;
if (process.env.PRIVATEMODE_IS_PLAYWRIGHT_TEST === '1') {
  loadStaticFiles = serve({
    directory: path.join(__dirname, '..', '..', '..', 'frontend', 'build'),
  });
} else {
  loadStaticFiles = serve({
    directory: path.join(process.resourcesPath, 'renderer'),
  });
}

// Handle creating/removing shortcuts on Windows when installing/uninstalling.
if (started) {
  app.quit();
}

let proxyPort: string;

const createWindow = () => {
  // Create the browser window.
  const mainWindow = new BrowserWindow({
    width: 1280,
    height: 720,
    title: 'Privatemode',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
    },
  });

  mainWindow.setMenuBarVisibility(false);

  // and load the index.html of the app.
  if (import.meta.env.DEV) {
    console.log('Loading from Vite dev server');
    mainWindow.loadURL('http://localhost:5173');

    // Open the DevTools.
    mainWindow.webContents.openDevTools();
  } else if (process.env.PRIVATEMODE_IS_PLAYWRIGHT_TEST === '1') {
    console.log('Loading static files from frontend directory for testing');
    console.log(path.join(__dirname, '..', '..', '..', 'frontend', 'build'));
    loadStaticFiles(mainWindow);
  } else {
    console.log('Loading static files');
    loadStaticFiles(mainWindow);

    // If on Linux, set the app icon explicitly.
    if (process.platform === 'linux') {
      mainWindow.setIcon(path.join(process.resourcesPath, 'icon.png'));
    }
  }

  mainWindow.webContents.setWindowOpenHandler((details) => {
    shell.openExternal(details.url); // Open URL in user's browser.
    return { action: 'deny' }; // Prevent the app from opening the URL.
  });
};

// This method will be called when Electron has finished
// initialization and is ready to create browser windows.
// Some APIs can only be used after this event occurs.
app.on('ready', () => {
  const res = startProxy();
  if (!res.success) {
    console.error(`Failed to start privatemode-proxy: ${res.error}`);
    dialog.showErrorBox(
      'Initialization Error',
      `Failed to start privatemode-proxy: ${res.error}`,
    );
    app.quit();
    return;
  }
  proxyPort = res.port;
  console.log('Proxy listening on port', proxyPort);

  ipcMain.handle('get-proxy-port', () => {
    console.log('Renderer requested proxy port:', proxyPort);
    return proxyPort;
  });

  ipcMain.handle('get-version', () => {
    return `v${packageJson.version}`;
  });

  createWindow();
});

// Quit when all windows are closed, except on macOS. There, it's common
// for applications and their menu bar to stay active until the user quits
// explicitly with Cmd + Q.
app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') {
    app.quit();
  }
});

app.on('activate', () => {
  // On OS X it's common to re-create a window in the app when the
  // dock icon is clicked and there are no other windows open.
  if (BrowserWindow.getAllWindows().length === 0) {
    createWindow();
  }
});

// In this file you can include the rest of your app's specific main process
// code. You can also put them in separate files and import them here.
