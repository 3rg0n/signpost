package extract

import (
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// SFCExtractor reads a single-file component: Vue, Svelte, or Astro.
//
// A single-file component is not a language. It is one file holding three
// languages — a template, a script, and a stylesheet — and only the script declares
// anything this graph can hold. So this extractor does no parsing of its own. It
// locates the script region, blanks everything outside it, and hands the result to
// TSExtractor, which already reads every import and declaration form the script can
// contain.
//
// That reuse is the whole design, and it is worth stating why it is safe rather than
// merely convenient. The script block of a `.vue`, `.svelte` or `.astro` file *is*
// TypeScript or JavaScript — not a dialect of it, not a superset with different module
// syntax. `import { ref } from 'vue'` inside a `<script setup>` is the same statement as
// the same line in a `.ts` file, resolved by the same bundler rules. A second extractor
// would restate all of it and drift from the first at the next import form somebody adds.
//
// Blanking rather than slicing is what makes the reuse honest. Every symbol and import
// TSExtractor reports carries a line number, and the bundle links to it; extracting the
// script into a fresh string would number its first line 1 and point every reader at the
// wrong line of the file they open. So the region outside the script keeps its newlines
// and loses only its content.
//
// What this deliberately does not read:
//
//   - The template. A `<Widget />` in the markup is a use of a component the script
//     already imported, so reading the markup would add no edge the script did not
//     supply — and where a component is auto-imported by a framework convention (Nuxt,
//     SvelteKit) there is no import statement to attribute it to and no file the
//     convention names in the source. An edge invented from a tag name would be a guess.
//   - The style block. A `@import` there names a stylesheet, and no stylesheet has a page
//     in this graph.
//   - A component's props as symbols. A prop is declared by `defineProps` in Vue and by
//     `export let` in Svelte, and only the second is a declaration TSExtractor can see —
//     which it reports already, since `export let x` is an exported binding in any TS
//     file. Reporting Vue's props as symbols and Svelte's the same way would need Vue's
//     macro vocabulary here, and a macro is a call rather than a declaration.
type SFCExtractor struct{}

// Langs implements Extractor.
func (SFCExtractor) Langs() []discover.Lang {
	return []discover.Lang{discover.LangVue, discover.LangSvelte, discover.LangAstro}
}

// Extract implements Extractor.
func (SFCExtractor) Extract(f discover.File) (Facts, error) {
	inner := f
	inner.Content = sfcScript(f.Content, f.Lang)

	facts, err := TSExtractor{}.Extract(inner)
	if err != nil {
		return facts, err
	}
	// The language stays the component's own rather than becoming TypeScript. A page
	// saying `vue` is what a reader needs: the file is a `.vue`, its module resolution
	// goes through a Vue-aware bundler, and calling it TypeScript would make the
	// per-language extractor scores in manifest.json report on a language nobody wrote.
	facts.Lang = f.Lang
	facts.Path = f.Path

	// A component has no shebang and no `main`, and its script is not an entrypoint: it
	// runs when the component mounts, which the framework decides. TSExtractor's shebang
	// rule cannot fire here anyway — the first line of an SFC is markup — but a `.svelte`
	// file whose first line was `#!/usr/bin/env node` would otherwise be reported as one,
	// and blanking does not remove a first line that is already outside the script.
	facts.Entrypoints = nil
	return facts, nil
}

// sfcScript blanks everything outside the script region, preserving line numbering.
//
// Two region syntaxes, and which one a file uses is decided by its language rather than
// by looking for both. A `.vue` and a `.svelte` file delimit script with an HTML tag; an
// `.astro` file delimits it with a `---` fence at the top of the file, the frontmatter
// form Markdown made conventional. Guessing between them would let a `<script>` tag in an
// Astro *template* — which is a browser script the framework passes through untouched, not
// the component's module — be read as the component's own module scope.
func sfcScript(src string, lang discover.Lang) string {
	if lang == discover.LangAstro {
		return astroFrontmatter(src)
	}
	return htmlScriptBlocks(src)
}

// htmlScriptBlocks keeps the contents of every `<script>` element and blanks the rest.
//
// Every block is kept, not the first, because both frameworks use more than one and each
// one holds real imports:
//
//	<script setup lang="ts">     Vue's component module
//	<script>                     Vue's options-API block, which may coexist with setup
//	<script context="module">    Svelte's module-level block, run once per module
//
// A `src` attribute is the one case where the element declares a dependency rather than
// holding one — `<script src="./widget.ts">` — and it is left unread. The referenced file
// is discovered and read on its own, so its imports reach the graph anyway; what is lost
// is the edge from this component to that file, which is a gap rather than a wrong answer.
func htmlScriptBlocks(src string) string {
	out := []byte(src)
	blankOutside := func(from, to int) {
		for i := from; i < to && i < len(out); i++ {
			if out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
			}
		}
	}

	kept := 0
	for {
		open := scriptOpenTag(src, kept)
		if open < 0 {
			break
		}
		// The tag's own text is blanked along with the markup: an attribute like
		// `lang="ts"` is not code, and leaving it would put a stray token on the line.
		bodyStart := strings.IndexByte(src[open:], '>')
		if bodyStart < 0 {
			break
		}
		bodyStart += open + 1
		end := indexFoldFrom(src, "</script", bodyStart)
		if end < 0 {
			// An unclosed `<script>` is a broken component. The rest of the file is read
			// as script rather than discarded, which is the reading that loses least:
			// the imports are at the top of the block and they are still there.
			end = len(src)
		}
		blankOutside(kept, bodyStart)
		kept = end
	}
	blankOutside(kept, len(src))
	return string(out)
}

// scriptOpenTag returns the index of the next `<script` open tag at or after from, or -1.
//
// Case-insensitive, because HTML is, and `<SCRIPT>` in a hand-written component is legal.
// The character after the tag name must be whitespace or `>`, so `<scriptlet>` — and, more
// realistically, a custom element named `<script-loader>` — is not mistaken for one.
func scriptOpenTag(src string, from int) int {
	for i := from; ; {
		idx := indexFoldFrom(src, "<script", i)
		if idx < 0 {
			return -1
		}
		next := idx + len("<script")
		if next >= len(src) {
			return -1
		}
		switch src[next] {
		case ' ', '\t', '\r', '\n', '>':
			return idx
		}
		i = next
	}
}

// indexFoldFrom is a case-insensitive strings.Index starting at from, folding ASCII only.
//
// ASCII rather than Unicode, and that is the correctness requirement rather than a
// shortcut. Every index this returns is a byte offset into the original string, used to
// blank a region of it — so a fold that changes byte length shifts every offset after it.
// `strings.ToLower` does: `İ` (U+0130) is two bytes and lowercases to three, and a
// template is exactly where a character like that appears, since it is the part of a
// component holding user-facing text. The result would be a `<script>` boundary located
// past where it sits, blanking the first line of real code and leaving markup beside it.
// The needles here are HTML tag names, so nothing outside ASCII needs folding at all.
func indexFoldFrom(s, substr string, from int) int {
	if from < 0 {
		from = 0
	}
	if len(substr) == 0 || from+len(substr) > len(s) {
		return -1
	}
	for i := from; i+len(substr) <= len(s); i++ {
		if hasPrefixFold(s[i:], substr) {
			return i
		}
	}
	return -1
}

// hasPrefixFold reports whether s begins with prefix, comparing ASCII letters case-blind.
func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if lowerASCII(s[i]) != lowerASCII(prefix[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// astroFrontmatter keeps the fenced frontmatter at the top of an Astro component.
//
// The fence is `---` on a line of its own, and it must be the file's first substantive
// line: an Astro component either opens with frontmatter or has none. That is what makes
// this safe to distinguish from a `---` appearing later, which in an Astro file is an
// `<hr>` written in the template.
//
// A file with no frontmatter is a pure template and declares nothing, so everything is
// blanked and the extractor reports no facts — which is the honest answer for a component
// that imports nothing.
func astroFrontmatter(src string) string {
	lines := strings.Split(src, "\n")
	open := -1
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		if t == "---" {
			open = i
		}
		break
	}
	if open < 0 {
		return blankAll(src)
	}
	close := -1
	for i := open + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			close = i
			break
		}
	}
	if close < 0 {
		// An unterminated fence. Everything below it is read as script, for the reason
		// htmlScriptBlocks gives for an unclosed `<script>`.
		close = len(lines)
	}
	for i := range lines {
		if i > open && i < close {
			continue
		}
		lines[i] = blankAll(lines[i])
	}
	return strings.Join(lines, "\n")
}

// blankAll replaces every character except a line ending with a space.
func blankAll(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] != '\n' && b[i] != '\r' {
			b[i] = ' '
		}
	}
	return string(b)
}
