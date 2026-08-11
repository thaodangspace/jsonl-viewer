<script>
  import { Tooltip } from 'bits-ui';

  let {
    content,
    delayDuration = 500,
    children,
    ...restProps
  } = $props();
</script>

<Tooltip.Root {delayDuration}>
  <Tooltip.Trigger {...restProps}>
    {@render children?.()}
  </Tooltip.Trigger>
  <Tooltip.Portal>
    <Tooltip.Content class="bg-ctp-text text-ctp-base text-[11px] px-2 py-1 rounded-md shadow-lg z-60 animate-fadeIn max-w-xs">
      {content}
      <Tooltip.Arrow class="fill-ctp-text" size={6} />
    </Tooltip.Content>
  </Tooltip.Portal>
</Tooltip.Root>
