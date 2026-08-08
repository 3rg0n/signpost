# web

Single-file components: Vue, Svelte, and Astro, plus the two negative boundaries that
only a directory of them can carry.

Each of the three languages puts source in a `<script>` block inside a file that is not
otherwise a program. So each is read by blanking every region that is not script and
handing what remains to the TypeScript reader — which means the boundaries worth
asserting here are not "does it find the import" but "does it find it at the right line,
and does it leave the rest of the file alone."

## What each file is here for

| File | Boundary |
|---|---|
| `src/components/Avatar.vue` | A `<script setup lang="ts">` block whose import of `vue` resolves to a declared dependency. Its `<style scoped>` block names `../styles/card.css`, which must land in no gap count at all. |
| `src/views/Dashboard.vue` | Two script blocks, the shape Vue's migration path produces. The second holds `vue-router` — the unresolved near-miss, which opens with every character of the declared `vue`. Its import of `Avatar.vue` is a cross-directory edge, not a self-edge. Its template comes first and holds `İstanbul`, which is the Unicode-fold regression below. |
| `src/lib/Badge.svelte` | The resolving half of the Svelte pair. Imported relatively by `Counter.svelte` and across directories by `index.astro`, both times with its extension spelled out. |
| `src/lib/Counter.svelte` | A `context="module"` block and an instance block. Holds `./Badges.svelte` — the unlinked near-miss, one letter from the `./Badge.svelte` imported on the line above. |
| `src/pages/index.astro` | Three imports and three resolutions: two relative component specifiers carrying their extension, and the declared `@astrojs/rss`. The `<hr />` in its template is the fence-location boundary. |
| `src/layouts/Base.astro` | Imports nothing. Its page exists on the strength of the extractor having run, not on the strength of an edge. |
| `src/styles/base.css`, `src/styles/card.css` | A pair, so the `no recognised kind` line has to count files rather than extensions. |
| `package.json` | Declares `vue`, `svelte`, `astro`, `@astrojs/rss` — the four names that make the resolving imports resolve, and that make the near-misses near. |
| `README.md` | This file. |

## Why the extension resolves literally

`./Badge.svelte` and `../layouts/Base.astro` name their extension. They resolve only
because the resolver tries the specifier as written before appending the extensions it
knows; an implementation that appended first would look for `./Badge.svelte.ts` and
report an unlinked import. That is asserted in `cmd/signpost/corpus_test.go` as a
negative — those specifiers must not appear on the unlinked line.

## Why a Turkish city is in the template

Locating a `<script>` fence means a case-insensitive search, and folding case with
`strings.ToLower` folds *Unicode*: `İ` (U+0130) is two bytes and lowercases to three, so
every byte offset past one comes back short. Those offsets decide where the script region
starts, so `Dashboard.vue`'s two of them shifted the open-tag index, the tag-name check
failed at the shifted position, and the whole file was read as markup — taking the
`vue-router` import with it and dropping the corpus's unresolved count from 26 to 25 while
every other assertion here still passed. Measured both ways: 26 with the ASCII-only fold,
25 with `strings.ToLower`.

It sits in the template, above the first fence, and that placement is the assertion. A shift
*inside* a block lands in the closing tag and changes nothing — which was measured, not
assumed, and is why the first version of this fixture proved nothing. A template is also
where such a character actually appears, since it is the part of a component holding
user-facing text.

## Why this README is not unclassified

This file is a document: classified, routed to a reader, and read. It must **not** appear
in the `no recognised kind` line, even though two files in this tree do. An
implementation that counted by directory — or that counted anything no reader produced
facts for — would report it, and a coverage line that fires on every README is a line
nobody reads.
