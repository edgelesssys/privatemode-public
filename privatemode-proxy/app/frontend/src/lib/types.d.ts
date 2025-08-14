declare module 'stacking-order' {
  export function compare (a: HTMLElement, b: HTMLElement): number
}

interface Window {
  go?: any
}

declare module 'svelte-fa/src/fa.svelte' {
  import Fa from 'svelte-fa'
  export default Fa
}
