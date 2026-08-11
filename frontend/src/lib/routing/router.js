const HOME_PATH = '/';
const SESSIONS_PREFIX = '/sessions/';

export const RouteName = Object.freeze({
  SESSIONS: 'sessions',
  SESSION: 'session',
  NOT_FOUND: 'notFound',
});

function sessionsRoute() {
  return { name: RouteName.SESSIONS };
}

function notFoundRoute() {
  return { name: RouteName.NOT_FOUND };
}

/**
 * Remove URL query and fragment components without decoding the pathname.
 * Path decoding is intentionally done only for the session ID below so a
 * malformed escape sequence becomes a not-found route instead of throwing.
 */
function pathnameFromInput(input) {
  if (typeof input !== 'string') return null;

  try {
    if (/^[a-z][a-z\d+.-]*:\/\//i.test(input)) {
      return new URL(input).pathname;
    }
  } catch {
    return null;
  }

  const queryStart = input.search(/[?#]/);
  return queryStart === -1 ? input : input.slice(0, queryStart);
}

/**
 * Resolve a browser pathname into the closed application route model:
 * sessions, session, or notFound.
 */
export function parseRoute(input) {
  const pathname = pathnameFromInput(input);
  if (pathname === HOME_PATH || pathname === '/sessions/') {
    return sessionsRoute();
  }

  if (!pathname || !pathname.startsWith('/') || !pathname.startsWith(SESSIONS_PREFIX)) {
    return notFoundRoute();
  }

  const encodedID = pathname.slice(SESSIONS_PREFIX.length);
  // A session ID is exactly one encoded path segment. A literal slash means
  // an extra segment, while %2F remains part of the ID and is valid.
  if (!encodedID || encodedID.includes('/')) {
    return notFoundRoute();
  }

  let sessionId;
  try {
    sessionId = decodeURIComponent(encodedID);
  } catch {
    return notFoundRoute();
  }

  return sessionId ? { name: RouteName.SESSION, sessionId } : notFoundRoute();
}

export function routeToPath(route) {
  if (route?.name === RouteName.SESSIONS) return HOME_PATH;
  if (route?.name === RouteName.SESSION && route.sessionId) {
    return `${SESSIONS_PREFIX}${encodeURIComponent(route.sessionId)}`;
  }
  throw new TypeError('Cannot generate a URL for an invalid route');
}

export function sessionRoutePath(sessionId) {
  return routeToPath({ name: RouteName.SESSION, sessionId });
}

function mergeHistoryState(currentState, nextState) {
  const current = currentState && typeof currentState === 'object' ? currentState : {};
  const next = nextState && typeof nextState === 'object' ? nextState : {};
  return { ...current, ...next };
}

function currentPath(location) {
  return `${location.pathname || '/'}${location.search || ''}${location.hash || ''}`;
}

function canonicalPathFor(route, location) {
  return route.name === RouteName.NOT_FOUND ? location.pathname : routeToPath(route);
}

/**
 * Small History API adapter. It is dependency-injected so route behavior can
 * be tested with a fake location/history surface without a browser harness.
 */
export function createHistoryRouter({
  history = globalThis.history,
  location = globalThis.location,
  eventTarget = globalThis,
} = {}) {
  if (!history || !location) {
    throw new Error('A history and location surface are required');
  }

  const listeners = new Set();

  function resolveCurrent() {
    return parseRoute(currentPath(location));
  }

  function notify(route) {
    for (const listener of listeners) listener(route);
    return route;
  }

  function canonicalize(route = resolveCurrent()) {
    const path = canonicalPathFor(route, location);
    if (location.pathname !== path || location.search || location.hash) {
      history.replaceState(mergeHistoryState(history.state, {}), '', path);
    }
    return route;
  }

  function navigate(route, { replace = false, state = {} } = {}) {
    const path = routeToPath(route);
    const sameCanonicalPath = location.pathname === path;

    // A route transition should never add a duplicate entry. If the current
    // URL has tolerated query/fragment noise, normalize it in place instead.
    if (sameCanonicalPath) {
      const hasState = state && typeof state === 'object' && Object.keys(state).length > 0;
      if (location.search || location.hash || hasState) {
        history.replaceState(mergeHistoryState(history.state, state), '', path);
      }
      return resolveCurrent();
    }

    const nextState = mergeHistoryState(history.state, state);
    if (replace) history.replaceState(nextState, '', path);
    else history.pushState(nextState, '', path);
    return notify(route);
  }

  function handlePopState() {
    const route = resolveCurrent();
    canonicalize(route);
    notify(route);
  }

  function subscribe(listener, { emitCurrent = true } = {}) {
    if (typeof listener !== 'function') throw new TypeError('Route listener must be a function');
    listeners.add(listener);
    if (emitCurrent) listener(resolveCurrent());
    return () => listeners.delete(listener);
  }

  // Direct requests to /sessions/ and URLs with tolerated query/fragment
  // components are canonicalized without adding a history entry.
  canonicalize(resolveCurrent());
  eventTarget?.addEventListener?.('popstate', handlePopState);

  return {
    get current() {
      return resolveCurrent();
    },
    navigate,
    canonicalize,
    subscribe,
    destroy() {
      eventTarget?.removeEventListener?.('popstate', handlePopState);
      listeners.clear();
    },
  };
}

export const createRouter = createHistoryRouter;

let browserRouter = null;

export function getBrowserRouter() {
  if (typeof window === 'undefined') {
    throw new Error('Browser routing is unavailable outside a browser');
  }
  if (!browserRouter) browserRouter = createHistoryRouter();
  return browserRouter;
}

export function navigateTo(route, options) {
  return getBrowserRouter().navigate(route, options);
}
