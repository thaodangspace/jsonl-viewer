<script>
  import { Collapsible } from 'bits-ui';
  import { escapeHTML } from '$lib/utils/markdown.js';
  import { applyCollapseDefault, toggleCollapseState } from '$lib/stores/collapseState.js';
  import ChevronRight from '~icons/lucide/chevron-right';
  import ChevronDown from '~icons/lucide/chevron-down';
  import Brain from '~icons/lucide/brain';

  let { content, stateKey } = $props();
  let collapsed = $state(true);
  let collapseKey = $derived(stateKey || 'thinking');

  $effect(() => {
    collapsed = applyCollapseDefault(collapseKey, true);
  });

  let open = $derived(!collapsed);

  function onOpenChange(v) {
    collapsed = toggleCollapseState(collapseKey, !v);
  }
</script>

<Collapsible.Root {open} {onOpenChange}
  class="rounded-lg overflow-hidden border border-ctp-surface0 mb-2"
  style="background:color-mix(in srgb, #135ce0 6%, #ffffff)"
>
  <Collapsible.Trigger
    class="w-full flex items-center gap-2 px-2.5 py-1.5 text-xs cursor-pointer"
  >
    <span class="flex items-center">
      {#if collapsed}
        <ChevronRight size={12} />
      {:else}
        <ChevronDown size={12} />
      {/if}
    </span>
    <Brain size={14} class="text-ctp-blue" />
    <span class="font-semibold text-ctp-blue">Thinking</span>
    <span class="text-ctp-overlay0 text-[10px] ml-auto">{escapeHTML(content.substring(0, 60))}…</span>
  </Collapsible.Trigger>
  <Collapsible.Content class="border-t border-ctp-surface0">
    <div class="p-3 text-xs" style="background:#f6f6f6;">
      <pre class="font-mono text-[11px] whitespace-pre-wrap break-words max-h-[300px] overflow-y-auto text-ctp-blue/70">{content}</pre>
    </div>
  </Collapsible.Content>
</Collapsible.Root>
