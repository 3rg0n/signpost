<script>
  // Corpus fixture: not compiled, not run.
  //
  // The Svelte leaf, imported by Counter.svelte beside it and by index.astro from
  // another directory — which is the cross-framework import a real Astro site writes,
  // and the case that only resolves because tsTarget tries the specifier as written
  // before appending an extension of its own.
  export let count = 0
</script>

<span class="badge">{count}</span>
