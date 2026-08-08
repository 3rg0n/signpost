<template>
  <img class="avatar" :src="src" :alt="alt" />
</template>

<script setup lang="ts">
// Corpus fixture: not compiled, not run.
//
// The leaf of the component tree, and the file that makes the import in
// Dashboard.vue a cross-module edge rather than a self-edge. `vue` is a declared
// dependency of web/package.json, so this import is the half of the standard pattern
// that must resolve to a reference page — the near-miss beside it is in Dashboard.vue.
import { computed } from 'vue'

const props = defineProps<{ src: string; name: string }>()

const alt = computed(() => `${props.name}'s avatar`)
</script>

<style scoped>
/* The `@import` is the negative half of the style boundary: it names a stylesheet, and no
 * stylesheet has a page in this graph. So it must appear in neither gap count — a
 * specifier reported as unresolved here would be an instruction to go and declare a
 * dependency that is a file in this directory, and one reported as unlinked would claim a
 * missing edge onto a node that should never exist. */
@import '../styles/card.css';

.avatar {
  border-radius: 50%;
}
</style>
