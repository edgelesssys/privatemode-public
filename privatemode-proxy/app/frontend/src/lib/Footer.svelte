<script lang="ts">
  let classes = ''
  export { classes as class }
  export let pin: boolean = false
  export let strongMask: boolean = false
</script>

<div class="lower-mask section-footer-mask" class:pin-footer={pin}/>
<div class="lower-mask2" class:strong-mask={strongMask} />
<div class="section-footer {classes}" class:pin-footer={pin}>
  <slot />
</div>
