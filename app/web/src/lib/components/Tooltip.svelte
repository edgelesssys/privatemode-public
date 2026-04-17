<script
  context="module"
  lang="ts"
>
  export function tooltip(
    node: HTMLElement,
    params: { text: string; delay: number },
  ) {
    let tooltipElement: HTMLDivElement | null = null;
    let timeoutId: number | null = null;

    function handleMouseEnter() {
      if (timeoutId) {
        clearTimeout(timeoutId);
      }
      timeoutId = window.setTimeout(() => {
        showTooltip();
      }, params.delay);
    }

    function handleMouseLeave() {
      if (timeoutId) {
        clearTimeout(timeoutId);
        timeoutId = null;
      }
      hideTooltip();
    }

    function showTooltip() {
      if (tooltipElement) return;

      tooltipElement = document.createElement('div');
      tooltipElement.className = 'custom-tooltip';
      tooltipElement.textContent = params.text;
      tooltipElement.style.whiteSpace = 'pre-line';

      document.body.appendChild(tooltipElement);

      const rect = node.getBoundingClientRect();
      const tooltipRect = tooltipElement.getBoundingClientRect();
      const margin = 8;

      const idealLeft = rect.left + rect.width / 2 - tooltipRect.width / 2;
      const clampedLeft = Math.max(
        margin,
        Math.min(idealLeft, window.innerWidth - tooltipRect.width - margin),
      );

      tooltipElement.style.left = `${clampedLeft}px`;
      tooltipElement.style.top = `${rect.top - tooltipRect.height - 8}px`;

      const arrowLeft = rect.left + rect.width / 2 - clampedLeft;
      tooltipElement.style.setProperty(
        '--arrow-left',
        `${Math.max(10, Math.min(arrowLeft, tooltipRect.width - 10))}px`,
      );
    }

    function hideTooltip() {
      if (tooltipElement) {
        tooltipElement.remove();
        tooltipElement = null;
      }
    }

    node.addEventListener('mouseenter', handleMouseEnter);
    node.addEventListener('mouseleave', handleMouseLeave);

    return {
      destroy() {
        if (timeoutId) {
          clearTimeout(timeoutId);
        }
        hideTooltip();
        node.removeEventListener('mouseenter', handleMouseEnter);
        node.removeEventListener('mouseleave', handleMouseLeave);
      },
    };
  }
</script>

<script lang="ts">
  export let text: string;
  export let delay: number = 300;
</script>

<span
  class="tooltip-wrapper"
  use:tooltip={{ text, delay }}
>
  <slot />
</span>

<style>
  .tooltip-wrapper {
    display: inline-flex;
  }

  :global(.custom-tooltip) {
    position: fixed;
    background-color: var(--color-dropdown-bg);
    color: var(--color-dropdown-text);
    padding: 0.5rem 0.75rem;
    border-radius: 0.375rem;
    font-size: 0.75rem;
    z-index: 1000;
    pointer-events: none;
    box-shadow: var(--shadow-md);
    max-width: 250px;
    text-align: center;
  }

  :global(.custom-tooltip)::after {
    content: '';
    position: absolute;
    top: 100%;
    left: var(--arrow-left, 50%);
    transform: translateX(-50%);
    border: 6px solid transparent;
    border-top-color: var(--color-dropdown-bg);
  }
</style>
