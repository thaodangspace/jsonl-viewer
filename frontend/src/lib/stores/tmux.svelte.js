import { get, writable } from 'svelte/store';

const TMUX_HISTORY_KEY = '__agentReaderTmuxModal';

export const tmuxSessionPickerOpen = writable(false);
export const tmuxWindowPickerOpen = writable(false); // false | string (session name)
export const tmuxTerminalTarget = writable(null); // { session: string, window: number } | null

function canUseHistory() {
  return typeof window !== 'undefined' && window.history && window.location;
}

function currentTmuxHistoryState() {
  if (!canUseHistory()) return null;
  return window.history.state?.[TMUX_HISTORY_KEY] || null;
}

function writeTmuxHistoryState(tmuxState, replace = false) {
  if (!canUseHistory()) return;
  window.history[replace ? 'replaceState' : 'pushState'](
    {
      ...(window.history.state || {}),
      [TMUX_HISTORY_KEY]: tmuxState,
    },
    '',
    window.location.href
  );
}

function stripTmuxHistoryState() {
  if (!canUseHistory() || !window.history.state?.[TMUX_HISTORY_KEY]) return;
  const nextState = { ...window.history.state };
  delete nextState[TMUX_HISTORY_KEY];
  window.history.replaceState(nextState, '', window.location.href);
}

function setTmuxModalState(tmuxState) {
  if (!tmuxState) {
    tmuxSessionPickerOpen.set(false);
    tmuxWindowPickerOpen.set(false);
    tmuxTerminalTarget.set(null);
    return;
  }

  if (tmuxState.kind === 'session') {
    tmuxTerminalTarget.set(null);
    tmuxWindowPickerOpen.set(false);
    tmuxSessionPickerOpen.set(true);
    return;
  }

  if (tmuxState.kind === 'window' && tmuxState.session) {
    tmuxTerminalTarget.set(null);
    tmuxSessionPickerOpen.set(false);
    tmuxWindowPickerOpen.set(tmuxState.session);
    return;
  }

  if (tmuxState.kind === 'terminal' && tmuxState.target?.session) {
    tmuxSessionPickerOpen.set(false);
    tmuxWindowPickerOpen.set(false);
    tmuxTerminalTarget.set(tmuxState.target);
    return;
  }

  setTmuxModalState(null);
}

export function openTmuxSessionPicker() {
  const state = { kind: 'session', depth: 1 };
  setTmuxModalState(state);
  if (currentTmuxHistoryState()) {
    writeTmuxHistoryState(state, true);
  } else {
    writeTmuxHistoryState(state);
  }
}

export function openTmuxWindowPicker(session) {
  if (!session) return;
  const current = currentTmuxHistoryState();
  const state = { kind: 'window', session, depth: current ? (current.depth || 1) + 1 : 1 };
  setTmuxModalState(state);
  writeTmuxHistoryState(state);
}

export function openTmuxTerminal(target) {
  if (!target?.session) return;
  const current = currentTmuxHistoryState();
  const state = { kind: 'terminal', target, depth: current?.depth || 1 };
  setTmuxModalState(state);
  writeTmuxHistoryState(state, Boolean(current));
}

export function backToTmuxSessionPicker() {
  const current = currentTmuxHistoryState();
  if (current?.kind === 'window' && canUseHistory()) {
    window.history.back();
    return;
  }
  openTmuxSessionPicker();
}

export function closeTmuxModals() {
  if (currentTmuxHistoryState() && canUseHistory()) {
    window.history.back();
    return;
  }
  setTmuxModalState(null);
  stripTmuxHistoryState();
}

export function dismissTmuxModals() {
  const current = currentTmuxHistoryState();
  if (current && canUseHistory()) {
    window.history.go(-Math.max(1, current.depth || 1));
    return;
  }
  setTmuxModalState(null);
  stripTmuxHistoryState();
}

export function handleTmuxPopState(event) {
  const nextTmuxState = event.state?.[TMUX_HISTORY_KEY] || null;

  // Terminal attach is the end of the tmux dialog flow. Back should dismiss it,
  // not reopen the previous picker entry that led to the attach.
  if (get(tmuxTerminalTarget)) {
    setTmuxModalState(null);
    if (nextTmuxState) stripTmuxHistoryState();
    return;
  }

  setTmuxModalState(nextTmuxState);
}
