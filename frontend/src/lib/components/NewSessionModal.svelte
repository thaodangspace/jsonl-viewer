<script>
  import { Dialog } from 'bits-ui';
  import { newSessionModalOpen } from '$lib/stores/ui.svelte.js';
  import { sessions } from '$lib/stores/session.svelte.js';
  import { createSession, fetchSessions } from '$lib/api/sessions.js';
  import { selectSession } from '$lib/actions/session.js';
  import PathPicker from './PathPicker.svelte';
  import Zap from '~icons/lucide/zap';
  import X from '~icons/lucide/x';
  import AlertCircle from '~icons/lucide/alert-circle';

  let cwd = $state('');
  let error = $state('');
  let loading = $state(false);
  let pickerRef = $state(null);

  let open = $derived($newSessionModalOpen);
  let inputRef = $state(null);

  function onOpenChange(v) {
    newSessionModalOpen.set(v);
    if (!v) {
      cwd = '';
      error = '';
    }
  }

  async function handleCreate() {
    if (!cwd.trim()) {
      error = 'Please enter a working directory';
      return;
    }
    loading = true;
    error = '';
    try {
      const data = await createSession(cwd.trim());
      newSessionModalOpen.set(false);
      cwd = '';
      error = '';
      if (data.session_id) {
        selectSession(data.session_id);
      }
      fetchSessions().then(list => sessions.set(list)).catch(() => {});
    } catch (e) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  function handlePickerSelect(path) {
    cwd = path;
    const input = document.querySelector('.path-picker-input');
    if (input) input.focus();
  }

  function handleInputKeydown(e) {
    if (pickerRef) {
      const handled = pickerRef.handleKeydown(e);
      if (handled) return;
    }
    if (e.key === 'Enter') {
      handleCreate();
    }
  }

  // Focus the input when dialog opens
  $effect(() => {
    if (open && inputRef) {
      setTimeout(() => inputRef.focus(), 50);
    }
  });
</script>

<Dialog.Root {open} {onOpenChange}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 bg-black/60 backdrop-blur-sm z-50 animate-fadeIn" />
    <Dialog.Content
      class="fixed inset-0 z-50 flex items-center justify-center"
      onOpenAutoFocus={(e) => { e.preventDefault(); inputRef?.focus(); }}
    >
      <div class="relative bg-ctp-mantle border border-ctp-surface0 rounded-2xl shadow-2xl w-[440px] max-w-[90vw] animate-fadeIn overflow-hidden">
        <!-- Header -->
        <div class="px-6 pt-5 pb-4 border-b border-ctp-surface0">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-3">
              <div class="w-8 h-8 rounded-lg bg-ctp-blue/20 flex items-center justify-center text-ctp-blue">
                <Zap size={16} />
              </div>
              <div>
                <Dialog.Title class="text-sm font-semibold text-ctp-text m-0 p-0">New Session</Dialog.Title>
                <Dialog.Description class="text-[11px] text-ctp-overlay0 mt-0.5 m-0 p-0">Create a new agent session</Dialog.Description>
              </div>
            </div>
            <Dialog.Close class="min-h-11 min-w-11 text-ctp-overlay0 hover:text-ctp-text transition-colors p-1 rounded-md hover:bg-ctp-surface0 flex items-center justify-center cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-ctp-blue" aria-label="Close new session dialog">
              <X class="h-4 w-4" />
            </Dialog.Close>
          </div>
        </div>

        <!-- Body -->
        <div class="px-6 py-5">
          <label for="new-session-cwd" class="text-xs font-medium text-ctp-text block mb-2">Working Directory</label>
          <div class="relative">
            <input
              id="new-session-cwd"
              type="text"
              bind:value={cwd}
              bind:this={inputRef}
              class="path-picker-input w-full px-3.5 py-2.5 bg-ctp-crust border border-ctp-surface0 rounded-lg text-ctp-text text-sm font-mono focus:outline-none focus:border-ctp-blue focus:ring-2 focus:ring-ctp-blue/20 placeholder:text-ctp-overlay0 transition-all"
              onkeydown={handleInputKeydown}
              placeholder="e.g. /Users/dt/code/my-project"
            />
            <PathPicker
              bind:this={pickerRef}
              value={cwd}
              onSelect={handlePickerSelect}
              onClose={() => {}}
            />
          </div>
          <p class="text-[11px] text-ctp-overlay0 mt-2">Enter the project directory path to start a new agent session.</p>
        </div>

        <!-- Footer -->
        <div class="px-6 py-4 border-t border-ctp-surface0 flex justify-end gap-2">
          <Dialog.Close class="min-h-11 px-4 py-2 rounded-lg text-xs font-medium text-ctp-overlay0 bg-ctp-surface0 hover:bg-ctp-surface1 hover:text-ctp-text transition-all focus:outline-none focus-visible:ring-2 focus-visible:ring-ctp-blue">
            Cancel
          </Dialog.Close>
          <button
            class="min-h-11 px-4 py-2 rounded-lg text-xs font-semibold bg-ctp-blue text-ctp-crust hover:bg-ctp-blue/80 transition-all shadow-lg shadow-ctp-blue/20 focus:outline-none focus-visible:ring-2 focus-visible:ring-ctp-blue"
            disabled={loading}
            onclick={handleCreate}
          >
            {loading ? 'Creating...' : 'Create & Start'}
          </button>
        </div>

        <!-- Error -->
        {#if error}
          <div class="px-6 pb-4">
            <div
              class="flex items-center gap-2 px-3 py-2 rounded-lg text-xs text-ctp-red"
              style="background:color-mix(in srgb, #e95f59 10%, #ffffff)"
            >
              <AlertCircle size={14} class="shrink-0" />
              <span>{error}</span>
            </div>
          </div>
        {/if}
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
