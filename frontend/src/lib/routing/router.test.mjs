import test from 'node:test';
import assert from 'node:assert/strict';
import {
  RouteName,
  createHistoryRouter,
  parseRoute,
  routeToPath,
} from './router.js';

function routeInput(path = '/') {
  const url = new URL(`http://example.test${path}`);
  return {
    get pathname() { return url.pathname; },
    get search() { return url.search; },
    get hash() { return url.hash; },
    setURL(next) { url.href = new URL(next, url).href; },
  };
}

function fakeBrowser(path = '/', initialState = { unrelated: 'keep' }) {
  const location = routeInput(path);
  const entries = [{ path, state: initialState }];
  let index = 0;
  const listeners = new Map();
  const eventTarget = {
    addEventListener(type, listener) {
      listeners.set(type, listener);
    },
    removeEventListener(type, listener) {
      if (listeners.get(type) === listener) listeners.delete(type);
    },
    emit(type) {
      listeners.get(type)?.();
    },
  };
  const history = {
    get state() { return entries[index].state; },
    pushState(state, _title, path) {
      entries.splice(index + 1);
      entries.push({ path, state });
      index += 1;
      location.setURL(path);
    },
    replaceState(state, _title, path) {
      entries[index] = { path, state };
      location.setURL(path);
    },
    back() {
      if (index === 0) return;
      index -= 1;
      location.setURL(entries[index].path);
      eventTarget.emit('popstate');
    },
    get length() { return entries.length; },
  };
  return { history, location, eventTarget, entries };
}

test('parses home and session routes while ignoring query and fragment', () => {
  assert.deepEqual(parseRoute('/'), { name: RouteName.SESSIONS });
  assert.deepEqual(parseRoute('/?sort=updated#top'), { name: RouteName.SESSIONS });
  assert.deepEqual(parseRoute('/sessions/example?tab=chat#latest'), {
    name: RouteName.SESSION,
    sessionId: 'example',
  });
});

test('canonicalizes the empty sessions route to home', () => {
  assert.deepEqual(parseRoute('/sessions/'), { name: RouteName.SESSIONS });
});

test('canonicalizes /sessions/ with replaceState without losing history state', () => {
  const browser = fakeBrowser('/sessions/', { unrelated: 'keep' });
  const router = createHistoryRouter(browser);

  assert.equal(browser.location.pathname, '/');
  assert.equal(browser.history.length, 1);
  assert.deepEqual(browser.history.state, { unrelated: 'keep' });
  router.destroy();
});

test('decodes IDs and re-encodes them in canonical URLs', () => {
  const route = parseRoute('/sessions/project%2Fsession%20one');
  assert.equal(route.name, RouteName.SESSION);
  assert.equal(route.sessionId, 'project/session one');
  assert.equal(routeToPath(route), '/sessions/project%2Fsession%20one');
});

test('reports malformed encoding and unknown paths distinctly', () => {
  assert.deepEqual(parseRoute('/sessions/bad%2'), { name: RouteName.NOT_FOUND });
  assert.deepEqual(parseRoute('/settings'), { name: RouteName.NOT_FOUND });
  assert.deepEqual(parseRoute('/sessions/a/b'), { name: RouteName.NOT_FOUND });
});

test('history navigation merges state and avoids duplicate pushes', () => {
  const browser = fakeBrowser('/', { unrelated: 'keep', count: 1 });
  const router = createHistoryRouter(browser);
  const seen = [];
  router.subscribe(route => seen.push(route));

  router.navigate({ name: RouteName.SESSION, sessionId: 'a/b' }, {
    state: { routeOwned: true },
  });
  assert.equal(browser.history.length, 2);
  assert.deepEqual(browser.history.state, {
    unrelated: 'keep',
    count: 1,
    routeOwned: true,
  });

  router.navigate({ name: RouteName.SESSION, sessionId: 'a/b' });
  assert.equal(browser.history.length, 2);
  assert.equal(browser.location.pathname, '/sessions/a%2Fb');
  assert.equal(seen.length, 2); // initial subscription plus the real transition

  router.destroy();
});

test('popstate emits the resolved route and canonicalizes tolerated URL noise', () => {
  const browser = fakeBrowser('/sessions/example?from=share#chat', { preserved: 42 });
  const router = createHistoryRouter(browser);
  const seen = [];
  router.subscribe(route => seen.push(route));

  browser.eventTarget.emit('popstate');

  assert.equal(browser.location.pathname, '/sessions/example');
  assert.equal(browser.location.search, '');
  assert.equal(browser.location.hash, '');
  assert.deepEqual(browser.history.state, { preserved: 42 });
  assert.equal(seen.at(-1).sessionId, 'example');
  assert.equal(browser.history.length, 1);

  router.destroy();
});
