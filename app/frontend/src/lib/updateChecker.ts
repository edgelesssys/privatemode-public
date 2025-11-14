export interface MotdResponse {
  latestVersion: string;
  outdatedMsg: string;
}

export interface UpdateInfo {
  hasUpdate: boolean;
  currentVersion: string;
  latestVersion: string;
  message: string;
}

const MOTD_URL = 'https://cdn.confidential.cloud/privatemode/v2/motd.json';

function compareVersions(current: string, latest: string): boolean {
  const cleanCurrent = current.replace(/^v/, '').replace(/-pre.*/, '');
  const cleanLatest = latest.replace(/^v/, '');

  const currentParts = cleanCurrent.split('.').map(Number);
  const latestParts = cleanLatest.split('.').map(Number);

  for (let i = 0; i < Math.max(currentParts.length, latestParts.length); i++) {
    const curr = currentParts[i] || 0;
    const lat = latestParts[i] || 0;

    if (lat > curr) return true;
    if (lat < curr) return false;
  }

  return false;
}

export async function checkForUpdates(): Promise<UpdateInfo> {
  try {
    const currentVersion = await window.electron.getVersion();

    const response = await fetch(MOTD_URL);
    if (!response.ok) {
      throw new Error(`Failed to fetch update info: ${response.statusText}`);
    }

    const data: MotdResponse = await response.json();
    const hasUpdate = compareVersions(currentVersion, data.latestVersion);

    return {
      hasUpdate,
      currentVersion,
      latestVersion: data.latestVersion,
      message: data.outdatedMsg,
    };
  } catch (error) {
    console.error('Error checking for updates:', error);
    const currentVersion = await window.electron.getVersion();
    return {
      hasUpdate: false,
      currentVersion,
      latestVersion: currentVersion,
      message: '',
    };
  }
}
