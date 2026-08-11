import { writable } from 'svelte/store';

// The selected session is derived from the current browser route. Keep this as
// runtime state for the existing history, WebSocket, and detail components,
// but do not persist it independently of the URL.
export const activeSession = writable(null);
export const activeSessionPath = writable(null);
export const sessions = writable([]);
export const unreadSessionIds = writable(new Set());
