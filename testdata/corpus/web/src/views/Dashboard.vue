<template>
  <!-- The template is first, which is legal Vue and is what carries this file's second
       boundary. Locating a script fence means a case-insensitive search, and folding case
       with `strings.ToLower` folds Unicode: `İ` (U+0130) is two bytes and lowercases to
       three, so every byte offset past one comes back short. Those offsets decide where a
       script block starts, so the two below shifted the open-tag index by two bytes, the
       tag-name check then failed on both blocks, and the whole file was read as markup —
       taking the `vue-router` import with it and dropping the unresolved count from 26 to
       25 while every other assertion in this corpus still passed.

       It has to be above a fence rather than inside a block: a shift inside one lands in
       the closing tag and changes nothing, which is how this was measured rather than
       assumed. A template is also where such a character really appears, since the
       template is the part of a component holding user-facing text. -->
  <h1>İstanbul</h1>
  <section v-if="active">
    <Avatar src="/ada.png" name="Ada" />
  </section>
  <footer>İzmir</footer>
</template>

<script setup lang="ts">
// Corpus fixture: not compiled, not run.
//
// Two script blocks in one file, which is the shape Vue's migration path produces: a
// `<script setup>` for new code beside a plain `<script>` holding the options object.
// It is here rather than only in the extractor's own fixtures because the second block
// carries the near-miss below, so an extractor that read only the first block lowers the
// unresolved count in TestCorpusResolvesExactlyWhatItShould — which is a count that
// fails in both directions, where a dropped import otherwise fails nothing.
import { ref } from 'vue'
import Avatar from '../components/Avatar.vue'

const active = ref(true)
</script>

<script>
// `vue-router` is Vue's negative boundary, and it is a real package nobody here
// declares. It opens with every character of the declared `vue`, so a dependency lookup
// that compared package names by prefix instead of by whole name folds it into the
// framework and reports an edge onto a dependency this component does not have.
import { useRouter } from 'vue-router'

export default {
  name: 'Dashboard',
  setup() {
    return { router: useRouter() }
  },
}
</script>
