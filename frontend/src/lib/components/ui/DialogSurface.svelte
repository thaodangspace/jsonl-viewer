<script>
  import { Dialog } from 'bits-ui';

  let {
    open = false,
    onOpenChange,
    children,
    ...restProps
  } = $props();
</script>

<Dialog.Root {open} {onOpenChange}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 bg-black/60 backdrop-blur-sm z-50 animate-fadeIn" />
    <Dialog.Content
      class="fixed inset-0 z-50 flex items-center justify-center"
      {...restProps}
    >
      {@render children?.()}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
