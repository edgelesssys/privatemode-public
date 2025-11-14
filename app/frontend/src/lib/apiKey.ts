// Key under which the API key is stored in localStorage.
const API_KEY_STORAGE_KEY = 'privatemode_api_key';

// Get the API key from localStorage.
export function getApiKey(): string | null {
  return localStorage.getItem(API_KEY_STORAGE_KEY);
}

// Set the API key in localStorage.
export function setApiKey(key: string): void {
  localStorage.setItem(API_KEY_STORAGE_KEY, key);
}

// Clear the API key from localStorage.
export function clearApiKey(): void {
  localStorage.removeItem(API_KEY_STORAGE_KEY);
}
