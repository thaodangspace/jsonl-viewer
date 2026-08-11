<script>
  import { Button } from 'bits-ui';

  let {
    loading = false,
    children,
    ...restProps
  } = $props();
</script>

<Button.Root type="button" {...restProps}>
  {#if loading}
    <span class="ui-spinner" aria-hidden="true"></span>
  {/if}
  {@render children?.()}
</Button.Root>
