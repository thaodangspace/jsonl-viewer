const collapsedByKey = new Map();
const touchedKeys = new Set();

function normalizeKey(key) {
  if (key === undefined || key === null || key === '') return null;
  return String(key);
}

/**
 * Apply a default collapsed value for a keyed UI section.
 * Defaults continue to update while the user has not interacted with the key;
 * once toggled, streaming prop updates will no longer overwrite the user's state.
 */
export function applyCollapseDefault(key, defaultCollapsed) {
  const normalized = normalizeKey(key);
  const fallback = Boolean(defaultCollapsed);

  if (!normalized) return fallback;

  if (!touchedKeys.has(normalized)) {
    collapsedByKey.set(normalized, fallback);
    return fallback;
  }

  return collapsedByKey.has(normalized) ? collapsedByKey.get(normalized) : fallback;
}

export function setCollapseState(key, collapsed, touched = true) {
  const normalized = normalizeKey(key);
  const value = Boolean(collapsed);

  if (!normalized) return value;

  collapsedByKey.set(normalized, value);
  if (touched) touchedKeys.add(normalized);
  return value;
}

export function toggleCollapseState(key, currentCollapsed) {
  return setCollapseState(key, !currentCollapsed, true);
}

export function clearCollapseState() {
  collapsedByKey.clear();
  touchedKeys.clear();
}
