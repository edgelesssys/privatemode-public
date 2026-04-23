import { writable, get } from 'svelte/store';
import { saveImage, loadImage, deleteImages } from './imageStore';

export interface AttachedFile {
  name: string;
  content: string;
}

export interface AttachedImage {
  id: string;
  name: string;
  /** May be empty before hydration from IndexedDB completes. */
  dataUrl: string;
}

/** Image reference as persisted in localStorage (without the data URL). */
type PersistedImage = Omit<AttachedImage, 'dataUrl'>;

export interface Message {
  id: string;
  role: 'user' | 'assistant' | 'system';
  content: string;
  reasoning?: string;
  thoughtDurationMs?: number;
  timestamp: number;
  attachedFiles?: AttachedFile[];
  attachedImages?: AttachedImage[];
  isError?: boolean;
}

export interface Chat {
  id: string;
  title: string;
  messages: Message[];
  createdAt: number;
  updatedAt: number;
  lastUserMessageAt: number;
  isStreaming?: boolean;
  wordCount?: number;
  hasError?: boolean;
  modelId?: string;
}

export function countWords(text: string): number {
  return text
    .trim()
    .split(/\s+/)
    .filter((word) => word.length > 0).length;
}

function createChatStore() {
  const STORAGE_KEY = 'privatemode_chats';
  const CURRENT_CHAT_KEY = 'privatemode_current_chat';

  const calculateChatWordCount = (messages: Message[]): number => {
    return messages.reduce((total, msg) => {
      let count = countWords(msg.content);
      if (msg.attachedFiles) {
        count += msg.attachedFiles.reduce(
          (fileTotal, file) => fileTotal + countWords(file.content),
          0,
        );
      }
      return total + count;
    }, 0);
  };

  const loadChats = (): Chat[] => {
    if (typeof window === 'undefined') return [];
    const stored = localStorage.getItem(STORAGE_KEY);
    if (!stored) return [];

    try {
      const chats = JSON.parse(stored) as Chat[];
      return chats.map((chat) => ({
        ...chat,
        isStreaming: false,
        lastUserMessageAt:
          chat.lastUserMessageAt ?? chat.updatedAt ?? chat.createdAt,
        messages: chat.messages.map((msg) => ({
          ...msg,
          attachedImages: msg.attachedImages?.map((img) => ({
            ...img,
            dataUrl: img.dataUrl ?? '',
          })),
        })),
      }));
    } catch {
      console.error('Failed to parse stored chats, resetting');
      localStorage.removeItem(STORAGE_KEY);
      return [];
    }
  };

  const loadCurrentChatId = (): string | null => {
    if (typeof window === 'undefined') return null;
    return localStorage.getItem(CURRENT_CHAT_KEY);
  };

  const { subscribe, set, update } = writable<Chat[]>(loadChats());
  const currentChatId = writable<string | null>(loadCurrentChatId());

  const saveToStorage = (chats: Chat[]) => {
    if (typeof window !== 'undefined') {
      // Persist image data in IndexedDB and strip dataUrl from
      // localStorage to avoid exceeding its ~5 MB quota.
      const stripped = chats.map((chat) => ({
        ...chat,
        messages: chat.messages.map((msg) => {
          if (!msg.attachedImages || msg.attachedImages.length === 0)
            return msg;
          return {
            ...msg,
            attachedImages: msg.attachedImages.map(
              ({ dataUrl, ...rest }): PersistedImage => {
                saveImage(rest.id, dataUrl).catch((e) =>
                  console.error('Failed to persist image to IndexedDB:', e),
                );
                return rest;
              },
            ),
          };
        }),
      }));
      try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(stripped));
      } catch (e) {
        if (e instanceof DOMException && e.name === 'QuotaExceededError') {
          console.error('localStorage quota exceeded');
          alert(
            'Chat history storage is full. Please delete some chats to free up space.',
          );
        } else {
          throw e;
        }
      }
    }
  };

  const saveCurrentChatId = (chatId: string | null) => {
    if (typeof window !== 'undefined') {
      if (chatId) {
        localStorage.setItem(CURRENT_CHAT_KEY, chatId);
      } else {
        localStorage.removeItem(CURRENT_CHAT_KEY);
      }
    }
  };

  const hydrateImages = async () => {
    const chats = get({ subscribe });
    let changed = false;
    const hydrated = await Promise.all(
      chats.map(async (chat) => ({
        ...chat,
        messages: await Promise.all(
          chat.messages.map(async (msg) => {
            if (!msg.attachedImages || msg.attachedImages.length === 0)
              return msg;
            const images = await Promise.all(
              msg.attachedImages.map(async (img) => {
                if (img.dataUrl) return img;
                const dataUrl = await loadImage(img.id);
                if (dataUrl) {
                  changed = true;
                  return { ...img, dataUrl };
                }
                return img;
              }),
            );
            return { ...msg, attachedImages: images };
          }),
        ),
      })),
    );
    if (changed) {
      set(hydrated);
    }
  };

  return {
    subscribe,
    hydrateImages,
    currentChatId: {
      subscribe: currentChatId.subscribe,
      set: (id: string | null) => {
        currentChatId.set(id);
        saveCurrentChatId(id);
      },
    },

    createChat: () => {
      const now = Date.now();
      const newChat: Chat = {
        id: crypto.randomUUID(),
        title: 'New chat',
        messages: [],
        createdAt: now,
        updatedAt: now,
        lastUserMessageAt: now,
        wordCount: 0,
      };

      update((chats) => {
        const updated = [...chats, newChat];
        saveToStorage(updated);
        return updated;
      });

      currentChatId.set(newChat.id);
      saveCurrentChatId(newChat.id);

      return newChat.id;
    },

    addMessage: (
      chatId: string,
      message: Omit<Message, 'id' | 'timestamp'>,
    ) => {
      const newMessage: Message = {
        ...message,
        id: crypto.randomUUID(),
        timestamp: Date.now(),
      };

      update((chats) => {
        const updated = chats.map((chat) => {
          if (chat.id === chatId) {
            const updatedMessages = [...chat.messages, newMessage];
            const now = Date.now();
            const updatedChat = {
              ...chat,
              messages: updatedMessages,
              updatedAt: now,
              lastUserMessageAt:
                message.role === 'user' ? now : chat.lastUserMessageAt,
              wordCount: calculateChatWordCount(updatedMessages),
            };

            if (chat.messages.length === 0 && message.role === 'user') {
              updatedChat.title = message.content.slice(0, 50);
            }

            return updatedChat;
          }
          return chat;
        });
        saveToStorage(updated);
        return updated;
      });

      return newMessage.id;
    },

    updateMessage: (
      chatId: string,
      messageId: string,
      content: string,
      reasoning?: string | null,
      thoughtDurationMs?: number | null,
      isError?: boolean,
    ) => {
      update((chats) => {
        const updated = chats.map((chat) => {
          if (chat.id === chatId) {
            const updatedMessages = chat.messages.map((msg) =>
              msg.id === messageId
                ? (() => {
                    const updatedMessage: Message = { ...msg, content };

                    if (reasoning !== undefined) {
                      if (reasoning === null) {
                        delete updatedMessage.reasoning;
                      } else {
                        updatedMessage.reasoning = reasoning;
                      }
                    }

                    if (thoughtDurationMs !== undefined) {
                      if (thoughtDurationMs === null) {
                        delete updatedMessage.thoughtDurationMs;
                      } else {
                        updatedMessage.thoughtDurationMs = thoughtDurationMs;
                      }
                    }

                    if (isError) {
                      updatedMessage.isError = true;
                    }

                    return updatedMessage;
                  })()
                : msg,
            );
            return {
              ...chat,
              messages: updatedMessages,
              updatedAt: Date.now(),
              wordCount: calculateChatWordCount(updatedMessages),
            };
          }
          return chat;
        });
        saveToStorage(updated);
        return updated;
      });
    },

    setStreaming: (chatId: string, isStreaming: boolean) => {
      update((chats) => {
        return chats.map((chat) => {
          if (chat.id === chatId) {
            return { ...chat, isStreaming };
          }
          return chat;
        });
      });
    },

    setModelId: (chatId: string, modelId: string) => {
      update((chats) => {
        const updated = chats.map((chat) => {
          if (chat.id === chatId && !chat.modelId) {
            return { ...chat, modelId };
          }
          return chat;
        });
        saveToStorage(updated);
        return updated;
      });
    },

    setChatError: (chatId: string, hasError: boolean) => {
      update((chats) => {
        const updated = chats.map((chat) => {
          if (chat.id === chatId) {
            return { ...chat, hasError };
          }
          return chat;
        });
        saveToStorage(updated);
        return updated;
      });
    },

    renameChat: (chatId: string, newTitle: string) => {
      update((chats) => {
        const updated = chats.map((chat) => {
          if (chat.id === chatId) {
            return {
              ...chat,
              title: newTitle,
            };
          }
          return chat;
        });
        saveToStorage(updated);
        return updated;
      });
    },

    deleteChat: (chatId: string) => {
      const chats = get({ subscribe });
      const chat = chats.find((c) => c.id === chatId);
      if (chat) {
        const imageIds = chat.messages.flatMap(
          (msg) => msg.attachedImages?.map((img) => img.id) ?? [],
        );
        deleteImages(imageIds).catch((e) =>
          console.error('Failed to delete images from IndexedDB:', e),
        );
      }

      update((chats) => {
        const updated = chats.filter((c) => c.id !== chatId);
        saveToStorage(updated);
        return updated;
      });

      currentChatId.update((current) => {
        if (current === chatId) {
          saveCurrentChatId(null);
          return null;
        }
        return current;
      });
    },

    branchChat: (
      sourceChatId: string,
      atMessageId: string,
      includeMessage: boolean,
    ): { chatId: string; message: Message } | null => {
      const chats = get({ subscribe });
      const sourceChat = chats.find((c) => c.id === sourceChatId);
      if (!sourceChat) return null;

      const messageIndex = sourceChat.messages.findIndex(
        (m) => m.id === atMessageId,
      );
      if (messageIndex === -1) return null;

      const sourceMessage = sourceChat.messages[messageIndex];
      const sliceEnd = includeMessage ? messageIndex + 1 : messageIndex;

      const branchedMessages = sourceChat.messages
        .slice(0, sliceEnd)
        .filter((m) => !m.isError)
        .map((m) => ({
          ...m,
          id: crypto.randomUUID(),
          attachedImages: m.attachedImages?.map((img) => ({
            ...img,
            id: crypto.randomUUID(),
          })),
        }));

      const now = Date.now();
      const newChat: Chat = {
        id: crypto.randomUUID(),
        title: `${sourceChat.title} (branch)`,
        messages: branchedMessages,
        createdAt: now,
        updatedAt: now,
        lastUserMessageAt: now,
        wordCount: calculateChatWordCount(branchedMessages),
        modelId: sourceChat.modelId,
      };

      update((chats) => {
        const updated = [...chats, newChat];
        saveToStorage(updated);
        return updated;
      });

      currentChatId.set(newChat.id);
      saveCurrentChatId(newChat.id);

      return { chatId: newChat.id, message: sourceMessage };
    },

    getChat: (chatId: string): Chat | undefined => {
      const chats = get({ subscribe });
      return chats.find((chat) => chat.id === chatId);
    },

    clear: () => {
      const chats = get({ subscribe });
      const imageIds = chats.flatMap((chat) =>
        chat.messages.flatMap(
          (msg) => msg.attachedImages?.map((img) => img.id) ?? [],
        ),
      );
      deleteImages(imageIds).catch((e) =>
        console.error('Failed to delete images from IndexedDB:', e),
      );
      set([]);
      saveToStorage([]);
      currentChatId.set(null);
      saveCurrentChatId(null);
    },
  };
}

export const chatStore = createChatStore();
