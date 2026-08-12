<script>
  import { onMount } from 'svelte';
  import { wsConnected } from '$lib/stores/ws.svelte.js';
  import { activeSession } from '$lib/stores/session.svelte.js';
  import { navigateHome } from '$lib/actions/session.js';
  import { fetchSession } from '$lib/api/sessions.js';
  import ArrowLeft from '~icons/lucide/arrow-left';
  import Info from '~icons/lucide/info';
  import SessionInfoModal from './SessionInfoModal.svelte';

  let sessionInfo = $state(null);
  let infoModalOpen = $state(false);

  function fetchSessionInfo(id) {
    if (id) {
      fetchSession(id)
        .then(data => { sessionInfo = data; })
        .catch(() => { sessionInfo = null; });
    } else {
      sessionInfo = null;
    }
  }

  onMount(() => {
    // Subscribe to active session changes
    const unsub = activeSession.subscribe(id => {
      fetchSessionInfo(id);
    });
    return unsub;
  });

  function escapeHTML(str) {
    if (typeof str !== 'string') str = str == null ? '' : String(str);
    return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }
</script>

<div class="px-5 py-2.5 border-b border-ctp-crust flex items-center gap-3 bg-ctp-mantle flex-wrap">
  <button
    type="button"
    class="min-h-11 px-3 rounded-md text-xs font-semibold text-ctp-blue hover:bg-ctp-blue/10 focus:outline-none focus:ring-2 focus:ring-ctp-blue/60 inline-flex items-center gap-1.5"
    onclick={navigateHome}
    aria-label="Back to sessions"
  >
    <ArrowLeft class="h-4 w-4" aria-hidden="true" />
    <span>Sessions</span>
  </button>

  <div
    class="w-2 h-2 rounded-full transition-colors duration-300"
    style="background: {$wsConnected ? '#65b73b' : '#e95f59'}"
  ></div>

  <div class="flex-1 min-w-0 overflow-hidden flex items-center gap-2">
    {#if sessionInfo}
      {#if sessionInfo.project}
        <span
          class="text-[11px] px-2 py-0.5 rounded-full whitespace-nowrap"
          style="background:color-mix(in srgb, #135ce0 12%, transparent); color:#135ce0"
        >
          {escapeHTML(sessionInfo.project)}
        </span>
      {/if}
      <button
        type="button"
        class="min-h-11 min-w-11 p-1 rounded-md text-ctp-overlay0 hover:text-ctp-blue hover:bg-ctp-blue/10 transition-all cursor-pointer flex items-center justify-center shrink-0 animate-fadeIn focus:outline-none focus-visible:ring-2 focus-visible:ring-ctp-blue"
        onclick={() => infoModalOpen = true}
        title="Show Session Info"
        aria-label="Show session info"
      >
        <Info size={14} />
      </button>

    {:else if $activeSession}
      <span
        class="text-[11px] px-2 py-0.5 rounded-full whitespace-nowrap"
        style="background:color-mix(in srgb, #135ce0 12%, transparent); color:#135ce0"
      >
        {$activeSession.substring(0, 12)}...
      </span>
    {/if}
  </div>

  <span class="text-[11px] text-ctp-overlay0">Read-only</span>

</div>

<SessionInfoModal show={infoModalOpen} sessionInfo={sessionInfo} onClose={() => infoModalOpen = false} />
