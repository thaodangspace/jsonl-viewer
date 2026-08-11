<script>
  import AlertCircle from '~icons/lucide/alert-circle';
  import ArrowLeft from '~icons/lucide/arrow-left';
  import { navigateHome } from '$lib/actions/session.js';

  let { message = 'This route is not available.', onHome = navigateHome } = $props();
</script>

<section class="flex-1 flex items-center justify-center p-6 bg-ctp-crust" aria-live="polite">
  <div class="w-full max-w-md rounded-2xl border border-ctp-surface0 bg-ctp-mantle p-8 text-center shadow-xl">
    <AlertCircle class="mx-auto mb-4 h-10 w-10 text-ctp-red" aria-hidden="true" />
    <h1 class="text-lg font-semibold text-ctp-text">Unable to open this session</h1>
    <p class="mt-2 text-sm text-ctp-subtext0">{message}</p>
    <button
      type="button"
      class="mt-6 inline-flex min-h-11 items-center justify-center gap-2 rounded-lg bg-ctp-blue px-5 py-2.5 text-sm font-semibold text-ctp-crust hover:bg-ctp-blue/80 focus:outline-none focus:ring-2 focus:ring-ctp-blue/60"
      onclick={onHome}
    >
      <ArrowLeft class="h-4 w-4" aria-hidden="true" />
      Back to Sessions
    </button>
  </div>
</section>
