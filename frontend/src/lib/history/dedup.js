// dedup.js — event deduplication for merged history and live events.

/**
 * Create a stable string key for an event, used to detect duplicates across
 * history pages and live WebSocket delivery.
 *
 * Format: {sessionID}:{eventID}
 * Falls back to a lightweight content hash when the event has no ID.
 */
export function makeEventKey(sessionID, event) {
  const data = event.data || event;
  const eventID = data.id || hashEvent(data);
  return `${sessionID}:${eventID}`;
}

/**
 * Filter a batch of events, removing any whose key is already in seenKeys.
 * Adds the new keys to seenKeys.
 *
 * @param {string} sessionID
 * @param {Array} events - raw event objects
 * @param {Set<string>} seenKeys
 * @returns {Array} events whose keys are not already in seenKeys
 */
export function dedupBatch(sessionID, events, seenKeys) {
  return events.filter(event => {
    const key = makeEventKey(sessionID, event);
    if (seenKeys.has(key)) return false;
    seenKeys.add(key);
    return true;
  });
}

/**
 * Lightweight content-based hash for events without IDs.
 * Uses a simple FNV-1a-like hash over the JSON representation.
 */
function hashEvent(data) {
  const str = typeof data === 'string' ? data : JSON.stringify(data);
  let hash = 0x811c9dc5;
  for (let i = 0; i < str.length; i++) {
    hash ^= str.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return (hash >>> 0).toString(16);
}
