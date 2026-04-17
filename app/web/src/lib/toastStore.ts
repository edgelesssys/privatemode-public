import { writable } from 'svelte/store';

export interface Toast {
  id: number;
  message: string;
  type: 'error' | 'info';
  dangerouslyRenderHTML: boolean;
}

interface ShowToastOptions {
  type?: 'error' | 'info';
  duration?: number;
  dangerouslyRenderHTML?: boolean;
}

let nextId = 0;

export const toasts = writable<Toast[]>([]);

export function showToast(
  message: string,
  {
    type = 'info',
    duration = 6000,
    dangerouslyRenderHTML = false,
  }: ShowToastOptions = {},
): void {
  const id = nextId++;
  toasts.update((t) => {
    if (t.some((toast) => toast.message === message)) return t;
    return [...t, { id, message, type, dangerouslyRenderHTML }];
  });
  setTimeout(() => {
    toasts.update((t) => t.filter((toast) => toast.id !== id));
  }, duration);
}
