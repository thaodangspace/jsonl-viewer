<script>
  import { Collapsible } from 'bits-ui';
  import ToolResultBlock from './ToolResultBlock.svelte';
  import { applyCollapseDefault, toggleCollapseState } from '$lib/stores/collapseState.js';

  let { results, stateKey } = $props();
  let collapsed = $state(false);
  let collapseKey = $derived(stateKey || `tool-result-group:${results.map(r => r.toolCallId || r.id || r.toolName || 'unknown').join(':')}`);
  let open = $derived(!collapsed);

  $effect(() => {
    collapsed = applyCollapseDefault(collapseKey, false);
  });

  function onOpenChange(v) {
    collapsed = toggleCollapseState(collapseKey, !v);
  }
</script>

<div class="flex flex-col items-start animate-fadeIn w-full">
  <div class="w-full max-w-[85%] rounded-xl overflow-hidden border border-ctp-crust"
       style="background:color-mix(in srgb, #135ce0 4%, #ffffff)">
    <Collapsible.Root {open} {onOpenChange}>
      <Collapsible.Trigger
        class="w-full flex items-center gap-2 px-3 py-2 text-xs cursor-pointer"
      >
        <span
          class="transition-transform duration-200 text-[10px]"
          style="transform: {collapsed ? '' : 'rotate(90deg)'}"
        >▶</span>
        <span>📎</span>
        <span class="font-semibold" style="color:#135ce0">Tool Results</span>
        <span class="text-ctp-overlay0 text-[10px] ml-auto">{results.length} results</span>
      </Collapsible.Trigger>

      <Collapsible.Content>
        {#each results as result, i (result.toolCallId || result.id || `${result.toolName || 'result'}-${i}`)}
          {#if i > 0}
            <div class="border-t border-ctp-surface0/30"></div>
          {/if}
          {@const resultKey = `tool-result:${result.toolCallId || result.id || `${result.toolName || 'result'}-${i}`}`}
          <ToolResultBlock msg={result} standalone={false} stateKey={resultKey} />
        {/each}
      </Collapsible.Content>
    </Collapsible.Root>
  </div>
</div>
