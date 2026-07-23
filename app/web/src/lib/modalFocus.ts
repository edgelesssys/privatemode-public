/**
 * Svelte action implementing the focus lifecycle of a modal dialog: moves
 * focus into the dialog when it opens, keeps Tab/Shift+Tab cycling within it,
 * and restores focus to the previously focused element when it closes.
 */
export function modalFocus(node: HTMLElement) {
  const previouslyFocused =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;

  const focusables = () =>
    Array.from(
      node.querySelectorAll<HTMLElement>(
        'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
      ),
    ).filter((el) => !el.hasAttribute('disabled'));

  (focusables()[0] ?? node).focus();

  function handleKeydown(event: KeyboardEvent) {
    if (event.key !== 'Tab') return;
    const els = focusables();
    if (els.length === 0) {
      event.preventDefault();
      node.focus();
      return;
    }
    const first = els[0];
    const last = els[els.length - 1];
    const active = document.activeElement;
    if (event.shiftKey && (active === first || active === node)) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  }

  node.addEventListener('keydown', handleKeydown);

  return {
    destroy() {
      node.removeEventListener('keydown', handleKeydown);
      previouslyFocused?.focus();
    },
  };
}
