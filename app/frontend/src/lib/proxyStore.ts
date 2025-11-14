import { writable } from 'svelte/store';
import type { Model } from './privatemodeClient';

export const proxyPort = writable<string | null>(null);
export const modelsLoaded = writable<boolean>(false);
export const models = writable<Model[]>([]);
