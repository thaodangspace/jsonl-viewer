import { activeSession, activeSessionPath, sessions, unreadSessionIds } from '$lib/stores/session.svelte.js';
import { messages, userScrolledUp, newMessageCount } from '$lib/stores/messages.svelte.js';
import { fetchSession, fetchSessions, markSessionRead } from '$lib/api/sessions.js';
import { clearSeenEvents, setActiveSessionID } from '$lib/utils/events.js';
import { clearCollapseState } from '$lib/stores/collapseState.js';
import { ws } from '$lib/stores/ws.svelte.js';
import { beginInitialHistoryLoad, loadInitialHistory, resetHistory } from '$lib/history/state.svelte.js';
import { navigateTo, RouteName } from '$lib/routing/router.js';
import { tick } from 'svelte';

let lifecycleGeneration = 0;

function clearDetailState() {
  resetHistory();
  clearSeenEvents();
  clearCollapseState();
  setActiveSessionID(null);
  activeSession.set(null);
  activeSessionPath.set(null);
  messages.set([]);
  userScrolledUp.set(false);
  newMessageCount.set(0);
}

/** Clear detail state when leaving the selected session. */
export function clearCurrentSession() {
  lifecycleGeneration += 1;
  clearDetailState();
}

/**
 * Load the session selected by a resolved route. URL mutation is deliberately
 * not performed here; this keeps popstate handling separate from lifecycle
 * work and prevents push/pop loops.
 */
export async function loadSession(id) {
  if (!id) return { ok: false, error: new Error('Session ID is missing') };

  const generation = ++lifecycleGeneration;
  clearDetailState();
  activeSession.set(id);
  setActiveSessionID(id);
  beginInitialHistoryLoad(id);

  let sessionInfo;
  try {
    sessionInfo = await fetchSession(id);
  } catch (error) {
    if (generation !== lifecycleGeneration) return { ok: false, stale: true, error };
    clearDetailState();
    return { ok: false, error };
  }

  if (generation !== lifecycleGeneration) return { ok: false, stale: true };
  activeSessionPath.set(sessionInfo.file);

  // Mark as read with the metadata's current line count.
  markSessionRead(id, sessionInfo.line_count || 0).catch(() => {});
  unreadSessionIds.update(set => {
    set.delete(id);
    return new Set(set);
  });

  // Flush DOM updates so the detail container is empty before history loads.
  await tick();
  if (generation !== lifecycleGeneration) return { ok: false, stale: true };

  // Subscribe to live events before fetching history. The history module
  // buffers events during the request and drains them after the snapshot.
  let socket = null;
  ws.subscribe(s => { socket = s; })();
  if (socket && socket.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify({ type: 'subscribe', session_id: id }));
  }

  const historyResult = await loadInitialHistory(id, (msgs) => {
    if (typeof msgs === 'function') messages.update(msgs);
    else messages.set(msgs);
  });

  if (generation !== lifecycleGeneration) return { ok: false, stale: true };
  if (historyResult && !historyResult.ok) {
    clearDetailState();
    return { ok: false, error: historyResult.error };
  }

  return { ok: true, sessionInfo };
}

/** User-initiated navigation to a session detail route. */
export function selectSession(id) {
  if (!id) return;
  return navigateTo({ name: RouteName.SESSION, sessionId: id });
}

/** User-initiated navigation back to the session landing route. */
export function navigateHome() {
  return navigateTo({ name: RouteName.SESSIONS });
}

export async function refreshSessions() {
  try {
    const list = await fetchSessions();
    sessions.set(list);
  } catch (e) {
    console.error('Failed to refresh sessions:', e);
  }
}
