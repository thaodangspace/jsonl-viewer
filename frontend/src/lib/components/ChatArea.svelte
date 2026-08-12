<script>
  import { onMount, tick } from 'svelte';
  import { messages, userScrolledUp, newMessageCount } from '$lib/stores/messages.svelte.js';
  import { activeSession } from '$lib/stores/session.svelte.js';
  import MessageBubble from './MessageBubble.svelte';
  import AssistantBubble from './AssistantBubble.svelte';
  import ToolResultBlock from './ToolResultBlock.svelte';
  import ToolResultGroup from './ToolResultGroup.svelte';
  import LoadingHistory from './LoadingHistory.svelte';
  import EndOfHistory from './EndOfHistory.svelte';
  import HistoryError from './HistoryError.svelte';
  import ScrollDownButton from './ScrollDownButton.svelte';
  import { isAtBottom, syncHorizontalScroll } from '$lib/utils/scroll.js';
  import { computeDisplayGroups } from '$lib/utils/displayGroups.js';
  import {
    getHasMore, getOlderLoading, getInitialLoading, getHistoryError,
    loadOlderHistory
  } from '$lib/history/state.svelte.js';

  let chatContainer = $state(null);
  let topScrollbarEl = $state(null);
  let bottomScrollbarEl = $state(null);
  let showTopScroll = $state(false);
  let showBottomScroll = $state(false);
  let showScrollBtn = $state(false);
  let olderPageLoading = false;

  let displayGroups = $derived(computeDisplayGroups($messages));

  onMount(() => {
    checkHorizontalScroll();
    const resizeObserver = new ResizeObserver(() => checkHorizontalScroll());
    if (chatContainer) resizeObserver.observe(chatContainer);

    const unsubscribe = messages.subscribe(async msgs => {
      if (!chatContainer || msgs.length === 0) return;
      await tick();
      if (getInitialLoading()) return;

      let scrolledUp = false;
      userScrolledUp.subscribe(value => { scrolledUp = value; })();
      if (!scrolledUp) {
        chatContainer.scrollTop = chatContainer.scrollHeight;
        showScrollBtn = false;
        newMessageCount.set(0);
      } else {
        newMessageCount.update(count => count + 1);
        showScrollBtn = true;
      }
    });

    return () => {
      unsubscribe();
      resizeObserver.disconnect();
    };
  });

  $effect(() => {
    const loading = getInitialLoading();
    if (!loading && chatContainer) {
      Promise.resolve().then(async () => {
        await tick();
        if (chatContainer) chatContainer.scrollTop = chatContainer.scrollHeight;
      });
    }
  });

  $effect(() => {
    if (!chatContainer) return;
    const cleanups = [];
    if (topScrollbarEl) cleanups.push(syncHorizontalScroll(topScrollbarEl, chatContainer));
    if (bottomScrollbarEl) cleanups.push(syncHorizontalScroll(bottomScrollbarEl, chatContainer));
    return () => cleanups.forEach(cleanup => cleanup());
  });

  function handleScroll() {
    if (!chatContainer) return;
    const atBottom = isAtBottom(chatContainer);
    userScrolledUp.set(!atBottom);
    if (atBottom) {
      showScrollBtn = false;
      newMessageCount.set(0);
    } else {
      showScrollBtn = true;
    }

    const sessionId = $activeSession;
    if (sessionId && chatContainer.scrollTop <= 200 && getHasMore() && !getOlderLoading()) {
      loadOlderPage(sessionId);
    }
  }

  async function loadOlderPage(sessionID) {
    if (olderPageLoading || !chatContainer) return;
    olderPageLoading = true;
    try {
      const previousHeight = chatContainer.scrollHeight;
      const newModels = await loadOlderHistory(sessionID, updater => {
        if (typeof updater === 'function') messages.update(updater);
        else messages.set(updater);
      });

      if (newModels?.length > 0) {
        messages.update(previous => [...newModels, ...previous]);
        await tick();
        await new Promise(resolve => requestAnimationFrame(resolve));
        chatContainer.scrollTop += chatContainer.scrollHeight - previousHeight;
      }
    } finally {
      olderPageLoading = false;
    }
  }

  function checkHorizontalScroll() {
    if (!chatContainer) return;
    const hasHorizontalScroll = chatContainer.scrollWidth > chatContainer.clientWidth;
    showTopScroll = hasHorizontalScroll;
    showBottomScroll = hasHorizontalScroll;
  }

  function scrollToBottomNow() {
    if (!chatContainer) return;
    chatContainer.scrollTop = chatContainer.scrollHeight;
    userScrolledUp.set(false);
    newMessageCount.set(0);
    showScrollBtn = false;
  }
</script>

<div class="flex-1 flex flex-col min-h-0">
  {#if showTopScroll}
    <div bind:this={topScrollbarEl} class="overflow-x-auto overflow-y-hidden scrollbar-thin" style="scrollbar-width: thin;">
      <div class="h-2" style="width: {chatContainer?.scrollWidth || '100%'};"></div>
    </div>
  {/if}

  <div
    class="flex-1 overflow-y-auto overflow-x-auto p-4 flex flex-col gap-3"
    bind:this={chatContainer}
    onscroll={handleScroll}
    style="background-image:linear-gradient(90deg,rgba(60,10,30,.04) 3%,transparent 0),linear-gradient(1turn,rgba(60,10,30,.04) 3%,transparent 0);background-size:20px 20px;background-position:50%;"
  >
    {#if getInitialLoading()}
      <LoadingHistory label="Loading session..." centered={true} />
    {:else if $messages.length === 0}
      <div class="flex-1 flex items-center justify-center text-center text-sm text-ctp-overlay0">
        No messages in this session.
      </div>
    {:else}
      {#if getOlderLoading()}
        <LoadingHistory />
      {:else if getHistoryError()}
        <HistoryError error={getHistoryError()} onRetry={() => $activeSession && loadOlderPage($activeSession)} />
      {:else if !getHasMore()}
        <EndOfHistory />
      {/if}

      {#each displayGroups as group (group.msg?.id || group.groupId)}
        {#if group.type === 'message'}
          {#if group.msg.role === 'user'}
            <MessageBubble msg={group.msg} />
          {:else if group.msg.role === 'assistant'}
            <AssistantBubble msg={group.msg} />
          {:else if group.msg.role === 'system'}
            <div class="flex items-center justify-center animate-fadeIn">
              <div class="px-3 py-1.5 rounded-lg text-xs text-ctp-red" style="background:color-mix(in srgb, #e95f59 10%, #ffffff)">
                {group.msg.content}
              </div>
            </div>
          {/if}
        {:else if group.type === 'toolGroup'}
          {#if group.results.length === 1}
            {@const result = group.results[0]}
            <ToolResultBlock msg={result} stateKey={`tool-result:${result.toolCallId || result.id || result.toolName || 'unknown'}`} />
          {:else}
            <ToolResultGroup results={group.results} stateKey={`tool-result-group:${group.groupId}`} />
          {/if}
        {/if}
      {/each}
    {/if}
  </div>

  {#if showBottomScroll}
    <div bind:this={bottomScrollbarEl} class="overflow-x-auto overflow-y-hidden scrollbar-thin" style="scrollbar-width: thin;">
      <div class="h-2" style="width: {chatContainer?.scrollWidth || '100%'};"></div>
    </div>
  {/if}

  {#if showScrollBtn}
    <ScrollDownButton onScrollToBottom={scrollToBottomNow} />
  {/if}
</div>
