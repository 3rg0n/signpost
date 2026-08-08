package extract

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func sfcFile(path string, lang discover.Lang, src string) discover.File {
	return discover.File{
		Path: path, Lang: lang, Class: discover.ClassSource, Content: src,
	}
}

func extractSFC(t *testing.T, path string, lang discover.Lang, src string) Facts {
	t.Helper()
	fa, err := SFCExtractor{}.Extract(sfcFile(path, lang, src))
	if err != nil {
		t.Fatalf("Extract(%s): %v", path, err)
	}
	fa.Normalize()
	return fa
}

// The scored Vue corpus. Hand-labeled against real components, including the shape
// that makes "keep every script block" necessary rather than tidy — a `<script setup>`
// and a legacy options-API `<script>` in one file, each with its own imports.
func vueCorpus() []Fixture {
	return []Fixture{
		{
			File: sfcFile("web/src/components/UserCard.vue", discover.LangVue, `<template>
  <div class="card">
    <Avatar :src="user.avatar" />
    <h3>{{ user.name }}</h3>
    <button @click="refresh">Reload</button>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import Avatar from './Avatar.vue'
import { fetchUser } from '@/lib/api'
import type { User } from '../types'

const props = defineProps<{ userId: string }>()

const user = ref<User | null>(null)
const initials = computed(() => user.value?.name.slice(0, 2) ?? '')

async function refresh() {
  user.value = await fetchUser(props.userId)
}

onMounted(refresh)
</script>

<style scoped>
.card { display: flex; }
</style>
`),
			Expected: Expected{
				Path: "web/src/components/UserCard.vue",
				// `vue` is a dependency like any other, and the two relative
				// specifiers carry their extension or lack it exactly as written.
				Imports: []string{"vue", "./Avatar.vue", "@/lib/api", "../types"},
				// `props`, `user` and `initials` are const declarations; `refresh`
				// is a function. None is exported — a `<script setup>` block has no
				// export statement at all, because the compiler turns the whole
				// block into the component's default export.
				Symbols:  []string{"props", "user", "initials", "refresh"},
				Exported: []string{},
				// A component is not an entrypoint. Its script runs when the
				// framework mounts it.
				Entrypoints: []string{},
			},
		},
		{
			// Two script blocks in one file, the shape Vue's migration path produces:
			// a `<script setup>` for new code beside a plain `<script>` holding the
			// options object. Keeping only the first would drop half the imports, and
			// which half depends on which block the author happened to write first.
			File: sfcFile("web/src/views/Dashboard.vue", discover.LangVue, `<script setup>
import { useStore } from '../store'
const store = useStore()
</script>

<script>
import { defineComponent } from 'vue'
import { track } from '@/lib/telemetry'

export default defineComponent({
  name: 'Dashboard',
  beforeRouteEnter() {
    track('dashboard.enter')
  },
})
</script>

<template>
  <section>{{ store.title }}</section>
</template>
`),
			Expected: Expected{
				Path:    "web/src/views/Dashboard.vue",
				Imports: []string{"../store", "vue", "@/lib/telemetry"},
				// `store` from the setup block, and the options object as the
				// module's default export.
				Symbols:     []string{"store", "default"},
				Exported:    []string{"default"},
				Entrypoints: []string{},
			},
		},
		{
			// The negative fixture, and every line of it is a thing that must *not*
			// become a fact. Both near misses are real: a custom element whose name
			// starts with the word script, and a code sample shown to the user as
			// markup. A template tag is the third — `<Avatar />` is a use of a
			// component, and the import that supplied it is elsewhere.
			File: sfcFile("web/src/components/Docs.vue", discover.LangVue, `<template>
  <script-loader url="/vendor/analytics.js" />
  <Avatar />
  <pre class="sample">
import { ref } from 'vue'
export const answer = 42
const shown = require('./nowhere')
  </pre>
</template>

<script src="./docs-logic.ts"></script>

<style>
@import './theme.css';
</style>
`),
			Expected: Expected{
				Path: "web/src/components/Docs.vue",
				// Nothing. `<script src=>` is the documented gap: the referenced file
				// is discovered on its own, so its imports still reach the graph, and
				// no wrong edge is invented here. The style block's `@import` names a
				// stylesheet, which has no page.
				Imports:     []string{},
				Symbols:     []string{},
				Exported:    []string{},
				Entrypoints: []string{},
			},
		},
	}
}

// The scored Svelte corpus. Svelte's second block is `context="module"` rather than
// `setup`, and its props are `export let` — a real exported binding, which is why
// Svelte needs no macro vocabulary here where Vue does.
func svelteCorpus() []Fixture {
	return []Fixture{
		{
			File: sfcFile("web/src/lib/Counter.svelte", discover.LangSvelte, `<script context="module">
  import { writable } from 'svelte/store'
  import { logEvent } from '$lib/telemetry'

  export const total = writable(0)

  export function reset() {
    total.set(0)
  }
</script>

<script>
  import { onMount } from 'svelte'
  import Badge from './Badge.svelte'

  export let start = 0
  export let label = 'clicks'

  let count = start

  const bump = () => {
    count += 1
    total.update((n) => n + 1)
    logEvent('counter.bump')
  }

  onMount(() => logEvent('counter.mount'))
</script>

<button on:click={bump}>
  <Badge {count} /> {label}
</button>
`),
			Expected: Expected{
				Path: "web/src/lib/Counter.svelte",
				Imports: []string{
					"svelte/store", "$lib/telemetry", "svelte", "./Badge.svelte",
				},
				Symbols: []string{"total", "reset", "start", "label", "count", "bump"},
				// The module block's exports and both props. `count` and `bump` are
				// component-local, and a caller cannot reach them.
				Exported:    []string{"total", "reset", "start", "label"},
				Entrypoints: []string{},
			},
		},
		{
			// A Storybook story written as a component. Classified as a test by
			// isTestPath, but the extractor still reads it — the fixture exists to
			// hold the shape a story has, which is an import of the component under
			// demonstration and nothing else.
			File: sfcFile("web/src/lib/Counter.stories.svelte", discover.LangSvelte, `<script>
  import Counter from './Counter.svelte'
  import { Story } from '@storybook/addon-svelte-csf'
</script>

<Story name="default">
  <Counter start={3} />
</Story>
`),
			Expected: Expected{
				Path:        "web/src/lib/Counter.stories.svelte",
				Imports:     []string{"./Counter.svelte", "@storybook/addon-svelte-csf"},
				Symbols:     []string{},
				Exported:    []string{},
				Entrypoints: []string{},
			},
		},
	}
}

// The scored Astro corpus. Astro's script region is a `---` fence rather than a tag,
// and the two fixtures are the fence read and the fence absent — which is the whole
// of the distinction, since a `---` that is not the first substantive line is an
// `<hr>` in the template.
func astroCorpus() []Fixture {
	return []Fixture{
		{
			File: sfcFile("web/src/pages/index.astro", discover.LangAstro, `---
import Layout from '../layouts/Base.astro'
import Card from '../components/Card.svelte'
import { getPosts } from '@/lib/content'

const posts = await getPosts()
const { title } = Astro.props
---

<Layout title={title}>
  <hr />
  {posts.map((p) => <Card post={p} />)}
</Layout>

<style>
  hr { border: 0; }
</style>
`),
			Expected: Expected{
				Path: "web/src/pages/index.astro",
				Imports: []string{
					"../layouts/Base.astro", "../components/Card.svelte", "@/lib/content",
				},
				// `posts` only. `const { title } = Astro.props` destructures rather
				// than declaring a named binding, and no name in it is the
				// declaration's own.
				Symbols:     []string{"posts"},
				Exported:    []string{},
				Entrypoints: []string{},
			},
		},
		{
			// No frontmatter, so nothing is script — and the `---` two thirds of the
			// way down is a horizontal rule. This is the fixture that would break if
			// the fence were located by searching the whole file: it would open a
			// region at that `<hr>`, run to end of file, and read the markup below as
			// code, inventing an import and an exported const from a documentation
			// sample.
			File: sfcFile("web/src/pages/about.astro", discover.LangAstro, `<section class="about">
  <h1>About</h1>
  <p>Static page, no frontmatter.</p>

---

  <pre>
import Layout from '../layouts/Base.astro'
export const answer = 42
  </pre>
</section>
`),
			Expected: Expected{
				Path:        "web/src/pages/about.astro",
				Imports:     []string{},
				Symbols:     []string{},
				Exported:    []string{},
				Entrypoints: []string{},
			},
		},
	}
}

// The measurement design §4.2 promises, one language at a time.
//
// Three calls rather than one merged corpus, because the score is reported per
// language in manifest.json and a merged number would let a format that reads badly
// hide behind two that read well — which is the specific failure the per-language
// reporting exists to prevent.
func TestSFCExtractorMeetsTarget(t *testing.T) {
	for _, tc := range []struct {
		lang     discover.Lang
		fixtures []Fixture
	}{
		{discover.LangVue, vueCorpus()},
		{discover.LangSvelte, svelteCorpus()},
		{discover.LangAstro, astroCorpus()},
	} {
		ls := ScoreExtractor(SFCExtractor{}, tc.lang, tc.fixtures)
		if !ls.MeetsTarget() {
			t.Errorf("%s extractor below target:\n%s", tc.lang, ls.Report())
		}
		t.Logf("%s extractor score:\n%s", tc.lang, ls.Report())
	}
}

// Blanking rather than slicing is the one design decision in sfc.go that a reader
// could reasonably think is arbitrary, and this is what makes it not. Every Symbol and
// Import carries a line number the bundle links to; a slice would renumber the script
// from 1 and point every link at the wrong line of the file somebody opens.
//
// The assertion is against the line numbers in the source below, counted from 1 as an
// editor counts them, for a script block that starts well down the file.
func TestSFCPreservesLineNumbers(t *testing.T) {
	src := `<template>
  <div>
    <span>filler</span>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'

const value = ref(0)
</script>
`
	fa := extractSFC(t, "Widget.vue", discover.LangVue, src)

	wantImport := map[string]int{"vue": 8}
	for _, im := range fa.Imports {
		want, ok := wantImport[im.Raw]
		if !ok {
			t.Errorf("unexpected import %q", im.Raw)
			continue
		}
		if im.Line != want {
			t.Errorf("import %q at line %d, want %d", im.Raw, im.Line, want)
		}
		delete(wantImport, im.Raw)
	}
	for raw := range wantImport {
		t.Errorf("import %q not found", raw)
	}

	wantSym := map[string]int{"value": 10}
	for _, s := range fa.Symbols {
		want, ok := wantSym[s.Name]
		if !ok {
			t.Errorf("unexpected symbol %q", s.Name)
			continue
		}
		if s.Line != want {
			t.Errorf("symbol %q at line %d, want %d", s.Name, s.Line, want)
		}
		delete(wantSym, s.Name)
	}
	for name := range wantSym {
		t.Errorf("symbol %q not found", name)
	}

	// The same file, with the same script pushed further down by two more lines of
	// markup, must report line numbers two higher. A slice-then-extract
	// implementation passes the assertion above by coincidence whenever the script
	// happens to start where it does; it cannot pass this one.
	shifted := strings.Replace(src, "</template>", "  <hr />\n  <hr />\n</template>", 1)
	fb := extractSFC(t, "Widget.vue", discover.LangVue, shifted)
	for _, im := range fb.Imports {
		if im.Raw == "vue" && im.Line != 10 {
			t.Errorf("shifted import vue at line %d, want 10", im.Line)
		}
	}
}

// The blanking must leave a file the scanner reads as the same number of lines, or a
// line number is preserved by accident rather than by construction. Asserted directly
// on the transform, for all three region syntaxes, because it is the invariant the
// whole extractor rests on.
func TestSFCScriptPreservesLineCount(t *testing.T) {
	cases := []struct {
		name string
		lang discover.Lang
		src  string
	}{
		{"vue", discover.LangVue, "<template>\n  <p>a</p>\n</template>\n\n<script>\nimport x from 'y'\n</script>\n"},
		{"svelte crlf", discover.LangSvelte, "<script>\r\n  import x from 'y'\r\n</script>\r\n\r\n<p>a</p>\r\n"},
		{"astro", discover.LangAstro, "---\nimport x from 'y'\n---\n<p>a</p>\n"},
		{"astro no fence", discover.LangAstro, "<p>a</p>\n<p>b</p>\n"},
		{"vue unclosed script", discover.LangVue, "<template>\n  <p>a</p>\n</template>\n<script>\nimport x from 'y'\n"},
	}
	for _, c := range cases {
		got := sfcScript(c.src, c.lang)
		if a, b := strings.Count(got, "\n"), strings.Count(c.src, "\n"); a != b {
			t.Errorf("%s: %d newlines after blanking, want %d", c.name, a, b)
		}
		if len(got) != len(c.src) {
			t.Errorf("%s: length %d after blanking, want %d", c.name, len(got), len(c.src))
		}
	}
}

// A `<script-loader>` custom element is the near miss that a plain substring search for
// "<script" gets wrong, and getting it wrong is not a missed fact but an invented one:
// the whole template after the tag would be read as module scope.
func TestSFCCustomElementIsNotAScriptTag(t *testing.T) {
	src := `<template>
  <script-loader src="/a.js" />
  <scriptlet />
  <p>import { ref } from 'vue'</p>
</template>
`
	fa := extractSFC(t, "Loader.vue", discover.LangVue, src)
	if len(fa.Imports) != 0 {
		t.Errorf("read %d imports from a template with no script block: %v", len(fa.Imports), fa.ImportPaths())
	}
	if idx := scriptOpenTag(src, 0); idx != -1 {
		t.Errorf("scriptOpenTag matched at %d, want no match", idx)
	}
}

// Uppercase and mixed-case tags are legal HTML and appear in hand-written components.
// Case folding is asserted separately from the corpus because a fixture written in the
// conventional lowercase would never exercise it.
func TestSFCScriptTagIsCaseInsensitive(t *testing.T) {
	fa := extractSFC(t, "Shout.vue", discover.LangVue, `<template><p>x</p></template>
<SCRIPT setup>
import { ref } from 'vue'
</Script>
`)
	if got := fa.ImportPaths(); len(got) != 1 || got[0] != "vue" {
		t.Errorf("imports = %v, want [vue]", got)
	}
}

// A non-ASCII character above a script fence must not move the script boundary, and this
// is a regression: the tag search folded case with strings.ToLower, which is a Unicode fold
// and changes byte length. `İ` (U+0130) is two bytes and lowercases to three, so every
// offset past one came back short — and those offsets decide where a script region starts,
// so the two below shifted the open-tag index, the tag-name check failed on the shifted
// position, and the whole file was read as markup with no imports at all.
//
// Above the fence rather than inside a block, which was measured rather than assumed: a
// shift *inside* a block lands in the closing tag and changes nothing. The template is also
// where such a character really appears, since it holds the user-facing text. Turkish is
// the ordinary case rather than a contrived one — `İstanbul` is spelled with that letter.
func TestSFCTemplateTextDoesNotMoveTheScriptBoundary(t *testing.T) {
	fa := extractSFC(t, "City.vue", discover.LangVue, `<template>
  <h1>İstanbul</h1>
  <p>İzmir</p>
</template>

<script setup lang="ts">
import { ref } from 'vue'
const city = ref('İstanbul')
</script>
`)
	if got := fa.ImportPaths(); len(got) != 1 || got[0] != "vue" {
		t.Errorf("imports = %v, want [vue]", got)
	}
	if got := fa.Imports; len(got) == 1 && got[0].Line != 7 {
		t.Errorf("import reported at line %d, want 7", got[0].Line)
	}
}

// An Astro component whose only content is a template declares nothing, and reporting
// nothing is the correct answer rather than a failure. Distinguished from the negative
// fixture above by having no near miss in it at all: this asserts the empty read is
// clean, not merely that a trap was avoided.
func TestAstroWithoutFrontmatterReadsNothing(t *testing.T) {
	fa := extractSFC(t, "web/src/components/Hr.astro", discover.LangAstro, "<hr />\n")
	if len(fa.Imports) != 0 || len(fa.Symbols) != 0 {
		t.Errorf("imports=%v symbols=%v, want both empty", fa.ImportPaths(), fa.SymbolNames())
	}
}

// A shebang on a component's first line is markup's problem, not an entrypoint. The
// blanking cannot remove it — it is outside every script region and stays as spaces —
// so Extract clears Entrypoints explicitly, and this is the case that would otherwise
// put a component in the bundle's entrypoint list.
func TestSFCNeverReportsAnEntrypoint(t *testing.T) {
	fa := extractSFC(t, "Odd.svelte", discover.LangSvelte, `#!/usr/bin/env node
<script>
import x from './y'
</script>
`)
	if len(fa.Entrypoints) != 0 {
		t.Errorf("entrypoints = %v, want none", fa.Entrypoints)
	}
	if got := fa.ImportPaths(); len(got) != 1 || got[0] != "./y" {
		t.Errorf("imports = %v, want [./y]", got)
	}
}

// The language on the facts stays the component's own. A page saying `typescript` for a
// file called `.vue` would misreport the file a reader opens, and the per-language
// extractor scores in manifest.json would be attributed to a language nobody wrote.
func TestSFCKeepsComponentLanguage(t *testing.T) {
	for _, lang := range []discover.Lang{discover.LangVue, discover.LangSvelte, discover.LangAstro} {
		fa, err := SFCExtractor{}.Extract(sfcFile("x", lang, "---\nimport a from 'b'\n---\n"))
		if err != nil {
			t.Fatalf("Extract(%s): %v", lang, err)
		}
		if fa.Lang != lang {
			t.Errorf("Lang = %q, want %q", fa.Lang, lang)
		}
	}
}
