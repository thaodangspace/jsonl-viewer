// state.svelte.js — per-session history loading state.
//
// Manages: load generation (for staleness), pagination cursor, loading flags,
// error state, live-event buffer, and dedup key tracking.
//
// This module is imported by session.js (for loading) and ChatArea.svelte (for
// older-page triggers).

import { transformEventsToMessages } from './transform.js';
import { dedupBatch } from './dedup.js';
import { fetchSessionHistory } from '$lib/api/sessions.js';
import { setLiveBufferActive, drainLiveBuffer } from '$lib/utils/events.js';

// ---- reactive state ----

let _loadGeneration = $state(0);
let _nextCursor = $state(null);
let _hasMore = $state(false);
let _initialLoading = $state(false);
let _olderLoading = $state(false);
let _historyError = $state(null);
let _seenKeys = new Set();
let _abortController = null;
let _activeSession = null;

// ---- getters (exported for components) ----

export function getLoadGeneration() { return _loadGeneration; }
export function getNextCursor() { return _nextCursor; }
export function getHasMore() { return _hasMore; }
export function getInitialLoading() { return _initialLoading; }
export function getOlderLoading() { return _olderLoading; }
export function getHistoryError() { return _historyError; }
export function getSeenKeys() { return _seenKeys; }

// ---- actions ----

/**
 * Mark the UI as loading a newly-selected session before the history request starts.
 */
export function beginInitialHistoryLoad(sessionID) {
  _activeSession = sessionID;
  _historyError = null;
  _initialLoading = true;
}


/**
 * Load the latest history page for a session.  Called from selectSession().
 *
 * 1. Increments load generation (invalidates stale requests).
 * 2. Subscribes to session via WS (separate step in session.js).
 * 3. Fetches latest page.
 * 4. Transforms, deduplicates, sets messages.
 * 5. Merges buffered live events.
 *
 * @param {string} sessionID
 * @param {Function} setMessages - (messages) => void to update the messages store
 * @returns {Promise<void>}
 */
export async function loadInitialHistory(sessionID, setMessages) {
  // Cancel any in-flight request
  if (_abortController) {
    _abortController.abort();
    _abortController = null;
  }

  _loadGeneration++;
  const gen = _loadGeneration;
  _activeSession = sessionID;
  _nextCursor = null;
  _hasMore = false;
  _historyError = null;
  _seenKeys = new Set();
  _initialLoading = true;

  // Start buffering live events during the fetch window
  setLiveBufferActive(true);

  const controller = new AbortController();
  _abortController = controller;

  try {
    const page = await fetchSessionHistory(sessionID, {
      limit: 20,
      signal: controller.signal,
    });

    // Stale check: generation changed while fetching
    if (_loadGeneration !== gen) return;

    // Dedup and transform
    const deduped = dedupBatch(sessionID, page.events || [], _seenKeys);
    const viewModels = transformEventsToMessages(deduped);

    setMessages(viewModels);

    _nextCursor = page.next_cursor || null;
    _hasMore = page.has_more || false;

    // Merge buffered live events (received during fetch)
    const buffered = drainLiveBuffer();
    if (buffered.length > 0) {
      const bufferedDeduped = dedupBatch(sessionID, buffered, _seenKeys);
      const bufferedModels = transformEventsToMessages(bufferedDeduped);
      if (bufferedModels.length > 0) {
        setMessages(prev => [...prev, ...bufferedModels]);
      }
    }
  } catch (err) {
    if (err.name === 'AbortError') return;
    if (_loadGeneration !== gen) return;

    _historyError = { message: err.message || 'Failed to load history' };

    // On error, drain the buffer and let live events flow normally
    drainLiveBuffer();
  } finally {
    if (_loadGeneration === gen) {
      _initialLoading = false;
    }
  }
}

/**
 * Load an older page of history (previous to what's currently displayed).
 * Returns the new view models, or null on failure.
 *
 * @param {string} sessionID
 * @param {Function} setMessages - (updater) => void
 * @returns {Promise<Array|null>} new view models to prepend, or null
 */
export async function loadOlderHistory(sessionID, setMessages) {
  if (_olderLoading || !_nextCursor || !_hasMore) return null;
  if (_activeSession !== sessionID) return null;

  _olderLoading = true;
  _historyError = null;
  const gen = _loadGeneration;
  const cursor = _nextCursor;

  try {
    const page = await fetchSessionHistory(sessionID, {
      limit: 20,
      cursor,
    });

    if (_loadGeneration !== gen) return null;

    const deduped = dedupBatch(sessionID, page.events || [], _seenKeys);
    const viewModels = transformEventsToMessages(deduped);

    _nextCursor = page.next_cursor || null;
    _hasMore = page.has_more || false;

    return viewModels;
  } catch (err) {
    if (err.name === 'AbortError') return null;
    if (_loadGeneration !== gen) return null;
    _historyError = { message: err.message || 'Failed to load older history' };
    return null;
  } finally {
    if (_loadGeneration === gen) {
      _olderLoading = false;
    }
  }
}

/**
 * Retry loading the previous page (after an error).
 */
export function retryOlderHistory(sessionID, setMessagesFn) {
  _historyError = null;
  loadOlderHistory(sessionID, setMessagesFn);
}

/**
 * Reset all history state (called when switching away from a session).
 */
export function resetHistory() {
  if (_abortController) {
    _abortController.abort();
    _abortController = null;
  }
  _loadGeneration++;
  _nextCursor = null;
  _hasMore = false;
  _initialLoading = false;
  _olderLoading = false;
  _historyError = null;
  _seenKeys = new Set();
  _activeSession = null;
  setLiveBufferActive(false);
}
