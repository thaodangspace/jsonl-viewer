<script>
  import RefreshCw from '~icons/lucide/refresh-cw';
  import Button from '$lib/components/ui/Button.svelte';

  let { error = null, onRetry = () => {} } = $props();

  const message = $derived(error?.message || 'Failed to load older messages');
</script>

<div class="flex flex-col items-center gap-2 py-3 px-4">
  <div class="flex items-center gap-2 text-ctp-red/80 text-xs">
    <span class="text-ctp-red">⚠</span>
    <span>{message}</span>
  </div>
  <Button
    class="inline-flex items-center gap-1.5 px-3 py-1 rounded-lg text-xs text-ctp-blue bg-ctp-blue/10 hover:bg-ctp-blue/15 transition-colors"
    onclick={onRetry}
  >
    <RefreshCw size={12} />
    <span>Retry</span>
  </Button>
</div>
