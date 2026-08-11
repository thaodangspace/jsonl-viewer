<script>
  import { onMount } from 'svelte';
  import { Tooltip } from 'bits-ui';
  import { connectWS } from '$lib/api/websocket.js';
  import { fetchSessions, fetchUnreadIds } from '$lib/api/sessions.js';
  import { getRPCStatus } from '$lib/api/rpc.js';
  import { sessions, activeSession, unreadSessionIds } from '$lib/stores/session.svelte.js';
  import { setRpcRunning } from '$lib/stores/rpc.svelte.js';
  import { newSessionModalOpen, sortBy, groupByProject } from '$lib/stores/ui.svelte.js';
  import { loadSession, clearCurrentSession } from '$lib/actions/session.js';
  import { getBrowserRouter, RouteName } from '$lib/routing/router.js';
  import SessionLanding from '$lib/components/SessionLanding.svelte';
  import RouteError from '$lib/components/RouteError.svelte';
  import HeaderBar from '$lib/components/HeaderBar.svelte';
  import ChatArea from '$lib/components/ChatArea.svelte';
  import NewSessionModal from '$lib/components/NewSessionModal.svelte';
  import ToastContainer from '$lib/components/ToastContainer.svelte';
  import TmuxSessionPicker from '$lib/components/TmuxSessionPicker.svelte';
  import TmuxTerminalModal from '$lib/components/TmuxTerminalModal.svelte';
  import TmuxWindowPicker from '$lib/components/TmuxWindowPicker.svelte';
  import { handleTmuxPopState } from '$lib/stores/tmux.svelte.js';

  let route = $state(null);
  let routeError = $state(null);

  function showNewSessionModal() {
    newSessionModalOpen.set(true);
  }

  onMount(() => {
    const router = getBrowserRouter();
    let routeGeneration = 0;
    let currentSortBy = 'last_updated';
    let currentGroupBy = false;

    function refreshSessions() {
      fetchSessions(currentSortBy, currentGroupBy)
        .then(list => sessions.set(list))
        .catch(e => console.error('Failed to fetch sessions:', e));
      fetchUnreadIds()
        .then(ids => {
          let activeId = null;
          activeSession.subscribe(value => { activeId = value; })();
          if (activeId) ids.delete(activeId);
          unreadSessionIds.set(ids);
        })
        .catch(() => {});
    }

    async function applyRoute(nextRoute) {
      const generation = ++routeGeneration;
      route = nextRoute;
      routeError = null;

      if (nextRoute.name === RouteName.SESSIONS) {
        clearCurrentSession();
        return;
      }

      if (nextRoute.name === RouteName.NOT_FOUND) {
        clearCurrentSession();
        routeError = 'The requested page was not found.';
        return;
      }

      let currentID = null;
      activeSession.subscribe(value => { currentID = value; })();
      // Idempotent route notifications (including tmux modal popstate entries)
      // must not reload history for the already displayed session.
      if (currentID === nextRoute.sessionId) return;

      const result = await loadSession(nextRoute.sessionId);
      if (generation !== routeGeneration || result.stale) return;
      if (!result.ok) {
        routeError = result.error?.status === 404 || result.error?.message === 'Session not found'
          ? 'Session not found.'
          : 'This session is unavailable.';
      }
    }

    connectWS();
    const unsubscribeRoute = router.subscribe(applyRoute);
    window.addEventListener('popstate', handleTmuxPopState);

    const unsubscribeSort = sortBy.subscribe(value => {
      currentSortBy = value;
      refreshSessions();
    });
    const unsubscribeGroup = groupByProject.subscribe(value => {
      currentGroupBy = value;
      refreshSessions();
    });

    getRPCStatus()
      .then(data => {
        if (data.sessions) {
          for (const [sessionId, running] of Object.entries(data.sessions)) {
            if (running) setRpcRunning(sessionId, true);
          }
        }
      })
      .catch(() => {});

    const interval = setInterval(refreshSessions, 5000);

    return () => {
      clearInterval(interval);
      unsubscribeRoute();
      router.destroy();
      window.removeEventListener('popstate', handleTmuxPopState);
      unsubscribeSort();
      unsubscribeGroup();
    };
  });
</script>

<Tooltip.Provider delayDuration={500}>
  <div class="app-shell flex min-h-[100dvh] h-[100dvh] w-full min-w-0 bg-ctp-mantle pb-[env(safe-area-inset-bottom)]">
    {#if route?.name === RouteName.SESSIONS}
      <SessionLanding onNewSession={showNewSessionModal} />
    {:else if route?.name === RouteName.SESSION && !routeError}
      <div class="flex min-w-0 flex-1 flex-col">
        <HeaderBar />
        <ChatArea />
      </div>
    {:else if route?.name === RouteName.NOT_FOUND || routeError}
      <RouteError message={routeError || 'The requested page was not found.'} />
    {/if}

    <NewSessionModal />
    <ToastContainer />
    <TmuxSessionPicker />
    <TmuxTerminalModal />
    <TmuxWindowPicker />
  </div>
</Tooltip.Provider>
