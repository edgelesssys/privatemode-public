<script lang="ts">
  import { MessageSquare } from 'lucide-svelte';

  export let title: string;
  export let subtitle: string;
</script>

<div class="empty-state">
  <MessageSquare
    size={64}
    aria-hidden="true"
  />
  <h2>{title}</h2>
  <p>{subtitle}</p>
</div>

<style>
  .empty-state {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    color: var(--color-text-tertiary);
    gap: 1rem;
  }

  .empty-state h2 {
    margin: 0;
    font-size: 1.5rem;
    font-weight: 600;
    color: var(--color-text-secondary);
  }

  .empty-state p {
    margin: 0;
    font-size: 1rem;
  }
</style>
