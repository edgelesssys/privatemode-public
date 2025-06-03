<script lang="ts">
  import { toasts, removeToast } from './stores/ToastStore.svelte'
  import Toast from './Toast.svelte'
</script>

<div class="toast-container">
  {#each $toasts as toast (toast.id)}
    <Toast 
      id={toast.id}
      type={toast.type}
      message={toast.message}
      duration={toast.duration}
      onRemove={removeToast}
    />
  {/each}
</div>

<style>
  .toast-container {
    position: fixed;
    top: 20px;
    right: 20px;
    z-index: 9999;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    pointer-events: none;
  }
</style> 
