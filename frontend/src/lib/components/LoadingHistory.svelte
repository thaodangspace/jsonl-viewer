<script>
  // LoadingHistory.svelte — subtle spinner shown while history is loading.
  let { label = 'Loading older messages...', centered = false } = $props();
</script>

<div class="flex items-center justify-center py-3 opacity-60" class:flex-1={centered}>
  <div class="w-4 h-4 border-2 border-ctp-blue border-t-transparent rounded-full animate-spin mr-2.5"></div>
  <span class="text-xs text-ctp-overlay0">{label}</span>
</div>
