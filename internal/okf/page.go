package okf

import "strings"

// Human-edit preservation: the mechanism design §6.1 calls the reason the bundle
// compounds rather than churns.
//
// A generator that overwrites a human's correction teaches people to stop correcting it,
// and then to stop reading it. So the page is split into regions, and signpost owns
// exactly the ones it marked:
//
//	<!-- signpost:managed:summary -->   generated; replaced on every run
//	...anything else...                 human; copied through byte-for-byte
//
// The invariant, and the reason this file has the most tests in the package: **text
// outside a managed region is never modified, reordered, or reflowed.** Not trimmed, not
// re-indented, not normalised. A human's trailing whitespace survives, because deciding
// their whitespace was wrong is the same class of act as deciding their sentence was.
//
// # The failure mode this is built against
//
// The dangerous bug here is not "the merge lost a human edit" — that is visible in a
// diff and someone reports it. The dangerous bug is a *partial* match: a marker that
// looks like a marker but is not, an unterminated region, a region name that appears
// twice. Each of those has a plausible-looking wrong behaviour, and the wrong behaviour
// silently eats human text. So every one of them is handled explicitly and named in a
// test, and the fallback everywhere is to preserve rather than to guess.

const (
	markerPrefix = "<!-- signpost:managed:"
	markerSuffix = " -->"
	markerEnd    = "<!-- /signpost:managed:"
	// bom is the UTF-8 byte order mark, spelled as an escape because Go rejects a
	// literal one anywhere but the first byte of a source file.
	bom = "\ufeff"
)

// openMarker and closeMarker render a region's delimiters.
func openMarker(name string) string  { return markerPrefix + name + markerSuffix }
func closeMarker(name string) string { return markerEnd + name + markerSuffix }

// normalizeRead converts a page read from disk to the line endings the emitter writes.
//
// Every comparison signpost makes is a byte comparison against freshly generated content,
// and the emitter always writes LF. So a checkout that materialised the bundle with CRLF
// differs from a rebuild on every single line, and the three things that compare bytes all
// draw the wrong conclusion at once:
//
//   - `verify` reports "a build would change this page" for every page in the bundle. The
//     bundle is byte-identical to what a build would produce, so the message is false and
//     the fix it names does not help — a rebuild writes LF, git converts it back on the
//     next checkout, and the gate stays red forever.
//   - `build` reports every page as `updated` and rewrites all of them, so a filesystem
//     watcher fires on the whole bundle each run.
//   - `build` reports "N page(s) had human notes, carried across" on a bundle with no
//     human notes at all, because HumanText() differs by its line endings. That one is
//     the worst of the three: the count exists to tell a user their writing was kept, so
//     inventing it teaches them the number means nothing.
//
// This is git's `core.autocrlf=true` on Windows, which is a default many Windows installs
// select, and it needs no unusual setup to hit: commit a bundle, clone the repository,
// verify. A `.gitattributes` carrying `* text=auto eol=lf` prevents it, and this
// repository ships one, which is exactly why signpost's own CI could never catch this.
// Normalising on read fixes it for repositories that have not been configured, which is
// every repository on the first day signpost runs in it.
//
// The narrow scope is the point, and it is why this does not violate the invariant at the
// top of this file. Human text is never *modified* — it is read through a decoding step,
// the same way the UTF-8 BOM that a Windows editor leaves is stripped on read. What a
// human wrote is their text; the bytes their platform's git chose to store it in are not,
// and treating a transport encoding as content is what produces all three bugs above. A
// lone CR is left alone: it is a classic Mac line ending no git conversion produces, so a
// CR that reaches here is a byte someone put in a file on purpose.
func normalizeRead(s string) string {
	if !strings.Contains(s, "\r\n") {
		return s
	}
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// Page is a parsed bundle page: its frontmatter, and its body split into regions.
type Page struct {
	// Frontmatter is the raw text between the `---` fences, excluding them, exactly as
	// read. Kept raw because merging frontmatter is a different problem from merging
	// body text and is handled by mergeFrontmatter, which needs the original to
	// preserve keys it does not understand.
	Frontmatter string
	// Body is the page after the closing fence, as a region list in file order.
	Body []Region
	// HasFrontmatter distinguishes a page with empty frontmatter from one with none,
	// which matters because the first is malformed and the second is a plain markdown
	// file someone dropped in the directory.
	HasFrontmatter bool
}

// Region is one span of a page body.
type Region struct {
	// Name is the managed region's name, or "" for human text.
	Name string
	// Text is the region's content. For a managed region this excludes the markers; for
	// human text it is everything, verbatim.
	Text string
}

// Managed reports whether this region is signpost's to rewrite.
func (r Region) Managed() bool { return r.Name != "" }

// ParsePage splits a page into frontmatter and body regions.
//
// It never fails. Every malformation — no fences, an unterminated region, a stray close
// marker — resolves to "treat the ambiguous text as human", because the cost of that
// choice is a managed region that stops being regenerated (visible: the page goes stale
// and `verify` says so) and the cost of the opposite is deleting someone's writing
// (invisible until they look).
func ParsePage(src string) *Page {
	p := &Page{}
	body := src
	if fm, rest, ok := splitFrontmatter(src); ok {
		p.Frontmatter = fm
		p.HasFrontmatter = true
		body = rest
	}
	p.Body = splitRegions(body)
	return p
}

// splitFrontmatter separates a leading `---` fenced block from the rest.
//
// The opening fence must be the very first line. A `---` further down a markdown file is
// a horizontal rule, and treating one as a frontmatter opener would swallow the document
// above it.
func splitFrontmatter(src string) (fm, rest string, ok bool) {
	// A UTF-8 BOM before the fence is what a Windows editor leaves behind. Tolerated on
	// read so a hand-edited page is not silently treated as having no frontmatter; the
	// emitter never writes one.
	src = strings.TrimPrefix(src, bom)
	if !strings.HasPrefix(src, "---\n") && !strings.HasPrefix(src, "---\r\n") {
		return "", src, false
	}
	nl := strings.IndexByte(src, '\n')
	after := src[nl+1:]

	// The closing fence is a line that is exactly `---`. Searched line-wise rather than
	// with a substring scan, because a frontmatter value could legitimately contain
	// `---` inside a quoted scalar and a substring match would close the block early.
	for off := 0; off <= len(after); {
		line, next := nextLine(after, off)
		if strings.TrimRight(line, "\r") == "---" {
			return after[:off], after[next:], true
		}
		if next == off {
			break
		}
		off = next
	}
	// Unterminated. The whole file is body: an unclosed fence means we cannot tell where
	// frontmatter ends, and guessing would either lose body text or parse prose as YAML.
	return "", src, false
}

// nextLine returns the line starting at off and the offset of the next one.
func nextLine(s string, off int) (line string, next int) {
	if off >= len(s) {
		return "", off
	}
	i := strings.IndexByte(s[off:], '\n')
	if i < 0 {
		return s[off:], len(s)
	}
	return s[off : off+i], off + i + 1
}

// splitRegions walks the body, emitting human spans and managed regions in order.
func splitRegions(body string) []Region {
	var out []Region
	// pending accumulates human text until a managed region interrupts it, so that
	// consecutive human lines stay one region rather than one per line.
	pending := 0

	flush := func(upto int) {
		if upto > pending {
			out = append(out, Region{Text: body[pending:upto]})
		}
	}

	for off := 0; off < len(body); {
		line, next := nextLine(body, off)
		name, isOpen := parseMarker(line)
		if !isOpen {
			off = next
			continue
		}
		// Find this region's close marker. Only an exact matching name closes it: a
		// region opened as `summary` is not closed by `</signpost:managed:notes>`, because
		// mismatched names mean the page was hand-edited into a state we cannot interpret
		// and the safe reading is that none of it is ours.
		endStart, endNext, found := findClose(body, next, name)
		if !found {
			// Unterminated region. Everything from the open marker on is human text: we
			// do not know where the generated content was meant to stop, so replacing it
			// would replace an unknown amount of someone's writing.
			off = next
			continue
		}
		flush(off)
		out = append(out, Region{Name: name, Text: body[next:endStart]})
		pending = endNext
		off = endNext
	}
	flush(len(body))
	return out
}

// parseMarker reports whether a line is an opening managed marker, and its name.
//
// The whole line must be the marker, allowing surrounding whitespace. A marker with text
// after it on the same line is not treated as a marker at all — it is prose that mentions
// one, which is exactly what this file's own documentation contains.
func parseMarker(line string) (name string, ok bool) {
	s := strings.TrimSpace(strings.TrimRight(line, "\r"))
	if !strings.HasPrefix(s, markerPrefix) || !strings.HasSuffix(s, markerSuffix) {
		return "", false
	}
	name = s[len(markerPrefix) : len(s)-len(markerSuffix)]
	if !validRegionName(name) {
		return "", false
	}
	return name, true
}

// parseCloseMarker is parseMarker for a closing marker.
func parseCloseMarker(line string) (name string, ok bool) {
	s := strings.TrimSpace(strings.TrimRight(line, "\r"))
	if !strings.HasPrefix(s, markerEnd) || !strings.HasSuffix(s, markerSuffix) {
		return "", false
	}
	name = s[len(markerEnd) : len(s)-len(markerSuffix)]
	if !validRegionName(name) {
		return "", false
	}
	return name, true
}

// findClose locates the close marker for name, returning the offset the region's text
// ends at and the offset after the marker line.
func findClose(body string, from int, name string) (endStart, endNext int, ok bool) {
	for off := from; off < len(body); {
		line, next := nextLine(body, off)
		if got, isClose := parseCloseMarker(line); isClose && got == name {
			return off, next, true
		}
		off = next
	}
	return 0, 0, false
}

// validRegionName restricts a name to what the emitter itself writes.
//
// Narrow on purpose. The name is matched textually against a close marker, and it comes
// out of a file in a repository — which is untrusted input. Restricting it to lowercase
// letters, digits, dash, and underscore means a name cannot contain the marker syntax,
// cannot be empty, and cannot vary by case in a way that makes an open and close look
// mismatched to us but matched to the person who typed them.
func validRegionName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// Managed returns the text of the named region, and whether it was present.
//
// An empty name is never present, even though every human region carries one. Without the
// guard, asking for a region a malformed marker failed to produce would return the first
// paragraph of someone's notes and report it as generated content — the exact confusion
// this file's parse rules exist to prevent, arriving through the lookup instead.
func (p *Page) Managed(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	for _, r := range p.Body {
		if r.Name == name {
			return r.Text, true
		}
	}
	return "", false
}

// HumanText returns everything outside managed regions, concatenated. Used by tests and
// by verify to assert nothing was lost; not used to rebuild a page, which needs the
// region order.
func (p *Page) HumanText() string {
	var b strings.Builder
	for _, r := range p.Body {
		if !r.Managed() {
			b.WriteString(r.Text)
		}
	}
	return b.String()
}

// Render writes a page back out.
func (p *Page) Render() string {
	var b strings.Builder
	if p.HasFrontmatter {
		b.WriteString("---\n")
		b.WriteString(p.Frontmatter)
		if !strings.HasSuffix(p.Frontmatter, "\n") && p.Frontmatter != "" {
			b.WriteString("\n")
		}
		b.WriteString("---\n")
	}
	for _, r := range p.Body {
		if !r.Managed() {
			b.WriteString(r.Text)
			continue
		}
		b.WriteString(openMarker(r.Name) + "\n")
		b.WriteString(r.Text)
		b.WriteString(closeMarker(r.Name) + "\n")
	}
	return b.String()
}

// Merge produces the page to write: generated content in the managed regions, everything
// else from the existing page.
//
// `next` is the page signpost would write for a repository with no existing bundle. `p`
// is what is on disk. The result takes frontmatter per mergeFrontmatter, managed region
// text from next, and every human region from p — including human regions that sit
// between managed ones, in their original positions.
//
// A managed region present in next but absent from p is appended. That is the growth
// path: a later signpost version emitting a new region must not require the page to be
// deleted first. It is appended rather than inserted at next's position, because next's
// position is relative to *its* regions and the human text around p's regions is the
// thing being preserved — inserting into that would mean deciding a human's paragraph
// belongs after rather than before the new content.
func (p *Page) Merge(next *Page) *Page {
	out := &Page{
		Frontmatter:    mergeFrontmatter(p.Frontmatter, next.Frontmatter),
		HasFrontmatter: true,
	}

	emitted := make(map[string]bool, len(next.Body))
	for _, r := range p.Body {
		if !r.Managed() {
			out.Body = append(out.Body, r)
			continue
		}
		gen, ok := next.Managed(r.Name)
		if !ok {
			// A region on disk that this version no longer generates. Kept, with its
			// content, rather than dropped: it may be a region an older signpost wrote
			// and a rollback would want back, and the text is at worst stale rather than
			// wrong. Dropping it would also silently delete content on a downgrade.
			out.Body = append(out.Body, r)
			continue
		}
		out.Body = append(out.Body, Region{Name: r.Name, Text: gen})
		emitted[r.Name] = true
	}
	for _, r := range next.Body {
		if r.Managed() && !emitted[r.Name] {
			out.Body = append(out.Body, r)
			emitted[r.Name] = true
		}
	}
	return out
}

// NewPage builds a page from frontmatter text and an alternating region list.
func NewPage(frontmatter string, body ...Region) *Page {
	return &Page{Frontmatter: frontmatter, HasFrontmatter: true, Body: body}
}

// managedRegion builds a managed region, ensuring its text ends in exactly one newline
// so the close marker lands on its own line — and that nothing inside it can be read as a
// marker.
func managedRegion(name, text string) Region {
	return Region{Name: name, Text: ensureTrailingNewline(escapeMarkers(text))}
}

// escapeMarkers makes generated text unable to alter the region it lands in.
//
// This is the invariant that makes the rest of this file's guarantees hold, and it belongs
// here because this is the one function every generated managed region passes through.
// Almost everything the emitter writes into a region is assembled from counted facts, but
// some of it is repository content — a file path, a directory-derived title — and a path on
// POSIX may contain a newline. So a filename can put a line of its own choosing inside a
// managed region, and a line that reads as a close marker ends the region early. Everything
// after it becomes human text, which signpost then refuses to overwrite: a permanent
// foothold in the bundle, from a filename, needing no model in the loop at all.
//
// Every `<!--` is escaped rather than only the ones that would parse as markers. An
// allowlist would be a second copy of parseMarker's rules to keep in sync — the failure
// this file's parse rules are written against — and no generated region has a legitimate
// use for an HTML comment. `&lt;` renders as `<` in prose, so a reader sees the text that
// was intended; inside a code span it shows literally, which for a filename containing
// marker syntax is the more honest reading anyway. The replacement contains no `<!--`, so
// this cannot recurse.
func escapeMarkers(text string) string {
	return strings.ReplaceAll(text, "<!--", "&lt;!--")
}

// ensureTrailingNewline collapses trailing blank lines to exactly one line break.
//
// Only ever called on text this package generated, which is why trimming `\n` alone is
// enough: a CRLF run would leave one blank line before the close marker rather than none,
// and the marker would still be on its own line — correct, just not tidy. Handling it
// properly would mean detecting and reproducing the region's line-ending style, which is
// work for a case that cannot arise, since text read from disk is copied through Merge
// rather than passed here.
func ensureTrailingNewline(s string) string {
	if s == "" {
		return "\n"
	}
	return strings.TrimRight(s, "\n") + "\n"
}

// humanRegion builds a human region, which the emitter uses only when creating a page
// for the first time — to seed the sections a reader is invited to write in.
func humanRegion(text string) Region { return Region{Text: text} }

// heading renders a markdown heading with the blank line that must follow it.
//
// The text is folded to a single line and stripped of marker syntax. Both are needed, and
// for a reason escapeMarkers does not cover: a heading is emitted as *human* text — a title
// belongs to whoever wrote the directory name and a reader may rewrite it — so it does not
// pass through managedRegion. A node's title is derived from a directory name, and a
// directory name may contain a newline, which lets a title put an *opening* marker on a
// line of human text. The parser then reads a region starting there, and the real region
// below it becomes that region's content: the placeholder stops regenerating and the page
// silently goes stale, which is the failure page.go's parse rules are written against
// arriving through the heading instead of through the markers.
func heading(level int, text string) string {
	return strings.Repeat("#", level) + " " + escapeMarkers(foldWhitespace(text)) + "\n\n"
}
