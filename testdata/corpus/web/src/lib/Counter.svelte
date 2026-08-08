<script context="module">
  // Corpus fixture: not compiled, not run.
  //
  // Svelte's module-level block, which runs once per module rather than once per
  // instance. Two blocks for the reason Dashboard.vue gives, and this is the language
  // where keeping both is least optional: the store below is declared here and used in
  // the instance block, so a reader that kept one block would report a component using
  // a name nothing in the file declares.
  import { writable } from 'svelte/store'

  export const total = writable(0)
</script>

<script>
  import { onMount } from 'svelte'
  import Badge from './Badge.svelte'
  // `./Badges.svelte` is Svelte's negative boundary, and it is the unlinked kind rather
  // than the unresolved kind: a relative specifier names a file, so a path reaching
  // nothing cannot be a package somebody forgot to declare. One letter from the
  // `./Badge.svelte` imported on the line above, through the same relative anchor — so
  // a resolver too eager to match a sibling erases the difference and turns every
  // mistyped component import into a satisfied edge.
  import Badges from './Badges.svelte'

  export let start = 0

  let count = start

  onMount(() => total.set(start))
</script>

<button on:click={() => (count += 1)}>
  <Badge {count} />
</button>
