import { writable, get } from 'svelte/store';

export interface AttachedFile {
  name: string;
  content: string;
}

export interface Message {
  id: string;
  role: 'user' | 'assistant' | 'system';
  content: string;
  timestamp: number;
  attachedFiles?: AttachedFile[];
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

    const chats = JSON.parse(stored) as Chat[];
    return chats.map((chat) => ({
      ...chat,
      lastUserMessageAt:
        chat.lastUserMessageAt ?? chat.updatedAt ?? chat.createdAt,
    }));
  };

  const loadCurrentChatId = (): string | null => {
    if (typeof window === 'undefined') return null;
    return localStorage.getItem(CURRENT_CHAT_KEY);
  };

  const { subscribe, set, update } = writable<Chat[]>(loadChats());
  const currentChatId = writable<string | null>(loadCurrentChatId());

  const saveToStorage = (chats: Chat[]) => {
    if (typeof window !== 'undefined') {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(chats));
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

  return {
    subscribe,
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
        title: 'New Chat',
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

    updateMessage: (chatId: string, messageId: string, content: string) => {
      update((chats) => {
        const updated = chats.map((chat) => {
          if (chat.id === chatId) {
            const updatedMessages = chat.messages.map((msg) =>
              msg.id === messageId ? { ...msg, content } : msg,
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
      update((chats) => {
        const updated = chats.filter((chat) => chat.id !== chatId);
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

    getChat: (chatId: string): Chat | undefined => {
      const chats = get({ subscribe });
      return chats.find((chat) => chat.id === chatId);
    },

    clear: () => {
      set([]);
      saveToStorage([]);
      currentChatId.set(null);
      saveCurrentChatId(null);
    },
  };
}

export const chatStore = createChatStore();
