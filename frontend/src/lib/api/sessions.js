export async function fetchSessions(sortBy = 'last_updated', groupBy = false) {
  const res = await fetch(`/api/sessions?page=1&sort=${sortBy}&group_by=${groupBy ? 'project' : ''}`);
  if (!res.ok) throw new Error('Failed to fetch sessions');
  const data = await res.json();
  return data.sessions;
}

export async function fetchSession(id) {
  const res = await fetch(`/api/sessions/${id}`);
  if (!res.ok) throw new Error('Session not found');
  return res.json();
}

export async function createSession(cwd) {
  const res = await fetch('/api/sessions/create', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ cwd }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function fetchUnreadIds() {
  const res = await fetch('/api/sessions/unread');
  if (!res.ok) throw new Error('Failed to fetch unread IDs');
  const data = await res.json();
  return new Set(data.unread_ids || []);
}

export async function markSessionRead(id, lineCount = 0) {
  const res = await fetch(`/api/sessions/${id}/mark-read`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ line_count: lineCount }),
  });
  if (!res.ok) throw new Error('Failed to mark session as read');
  return res.json();
}

/**
 * Fetch a page of session history via HTTP cursor pagination.
 * Returns { events, next_cursor, has_more, snapshot }.
 */
export async function fetchSessionHistory(id, { limit = 20, cursor = null, signal = null } = {}) {
  const params = new URLSearchParams();
  params.set('limit', String(limit));
  if (cursor) params.set('cursor', cursor);
  const res = await fetch(`/api/sessions/${encodeURIComponent(id)}/history?${params}`, { signal });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(`History fetch failed (${res.status}): ${text}`);
  }
  return res.json();
}
