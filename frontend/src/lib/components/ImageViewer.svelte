<script>
  import { Dialog } from 'bits-ui';
  import X from '~icons/lucide/x';
  import ChevronLeft from '~icons/lucide/chevron-left';
  import ChevronRight from '~icons/lucide/chevron-right';

  let { images, startIndex = 0, onClose } = $props();
  let currentIndex = $state(0);
  let open = $state(true);

  $effect(() => {
    currentIndex = startIndex;
  });

  let currentSrc = $derived(images[currentIndex]?.src || '');
  let currentAlt = $derived(images[currentIndex]?.alt || '');
  let total = $derived(images.length);

  function onOpenChange(v) {
    open = v;
    if (!v) {
      onClose?.();
    }
  }

  function prev(e) {
    e.stopPropagation();
    currentIndex = (currentIndex - 1 + total) % total;
  }

  function next(e) {
    e.stopPropagation();
    currentIndex = (currentIndex + 1) % total;
  }

  function handleKeydown(e) {
    if (e.key === 'ArrowLeft') prev(e);
    else if (e.key === 'ArrowRight') next(e);
  }
</script>

<Dialog.Root {open} {onOpenChange}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-[9999] animate-fadeIn" style="background: rgba(0, 0, 0, 0.85);" />
    <Dialog.Content
      class="fixed inset-0 z-[9999] flex items-center justify-center"
      onkeydown={handleKeydown}
    >
      <Dialog.Title class="sr-only">Image Viewer — {currentAlt || 'Image'} ({currentIndex + 1}/{total})</Dialog.Title>

      <!-- Close button -->
      <Dialog.Close class="absolute top-4 right-4 z-10 w-9 h-9 flex items-center justify-center rounded-full bg-white/10 hover:bg-white/20 text-white transition-colors cursor-pointer">
        <X class="w-4 h-4" />
      </Dialog.Close>

      <!-- Counter -->
      {#if total > 1}
        <div class="absolute top-4 left-4 z-10 px-3 py-1 rounded-full text-xs text-white/70 bg-white/10">
          {currentIndex + 1} / {total}
        </div>
      {/if}

      <!-- Previous button -->
      {#if total > 1}
        <button
          class="absolute left-4 z-10 w-12 h-12 flex items-center justify-center rounded-full bg-white/10 hover:bg-white/20 text-white transition-colors cursor-pointer"
          onclick={prev}
        >
          <ChevronLeft class="w-6 h-6" />
        </button>
      {/if}

      <!-- Next button -->
      {#if total > 1}
        <button
          class="absolute right-4 z-10 w-12 h-12 flex items-center justify-center rounded-full bg-white/10 hover:bg-white/20 text-white transition-colors cursor-pointer"
          onclick={next}
        >
          <ChevronRight class="w-6 h-6" />
        </button>
      {/if}

      <!-- Image -->
      <img
        src={currentSrc}
        alt={currentAlt}
        class="max-w-[90vw] max-h-[85vh] object-contain rounded-lg shadow-2xl"
        style="animation: zoomIn 0.2s ease-out;"
      />
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<style>
  @keyframes zoomIn {
    from { transform: scale(0.9); opacity: 0; }
    to { transform: scale(1); opacity: 1; }
  }
</style>
