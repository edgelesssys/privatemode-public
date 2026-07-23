import { writable } from 'svelte/store';

export interface ToastLink {
  href: string;
  label: string;
}

export interface Toast {
  id: number;
  message: string;
  type: 'error' | 'info';
  link?: ToastLink;
}

interface ShowToastOptions {
  type?: 'error' | 'info';
  duration?: number;
  link?: ToastLink;
}

let nextId = 0;

export const toasts = writable<Toast[]>([]);

export function showToast(
  message: string,
  { type = 'info', duration = 6000, link }: ShowToastOptions = {},
): void {
  const id = nextId++;
  toasts.update((t) => {
    if (t.some((toast) => toast.message === message)) return t;
    return [...t, { id, message, type, link }];
  });
  setTimeout(() => {
    toasts.update((t) => t.filter((toast) => toast.id !== id));
  }, duration);
}
