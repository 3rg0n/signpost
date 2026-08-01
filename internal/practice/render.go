package practice

import "strings"

// Rendering findings as the practices page's body.
//
// Here rather than in internal/okf for the reason Options.Practices states: the emitter is
// handed finished text so that it cannot compose a claim. That puts the wording in the
// package that made the claim, which is also the package that knows whether a citation is a
// path or an absence.
//
// Nothing here escapes markdown. The text goes through okf.managedRegion, which is that
// package's single chokepoint for marker syntax and is where the escaping belongs — a second
// implementation here would be a second thing to keep in sync, and the two could disagree
// silently. What this file must not do is emit anything whose *meaning* depends on not being
// escaped, which is why paths are rendered as inline code and never as links.

// Render writes the findings as markdown, grouped by topic.
//
// Empty for an empty result, which is the signal okf.renderAll uses to skip the page
// entirely: a heading with no findings under it would say measurement happened when it did
// not.
func (r *Result) Render() string {
	if len(r.Findings) == 0 {
		return ""
	}
	var b strings.Builder
	var current Topic
	first := true
	for _, f := range r.Findings {
		if first || f.Topic != current {
			if !first {
				b.WriteString("\n")
			}
			b.WriteString("### " + topicHeading(f.Topic) + "\n\n")
			current = f.Topic
			first = false
		}
		b.WriteString(renderFinding(f))
	}
	return b.String()
}

// topicHeading titles a section.
//
// Written out rather than derived from the Topic string, so a heading reads as a heading
// ("Build" not "build") without a title-casing helper that would have to decide what to do
// with "agent rules".
func topicHeading(t Topic) string {
	switch t {
	case TopicBuild:
		return "Building"
	case TopicTest:
		return "Testing"
	case TopicGates:
		return "What blocks a merge"
	case TopicDependencies:
		return "Dependencies"
	case TopicOwnership:
		return "Ownership and policy"
	case TopicDocumentation:
		return "Documentation"
	case TopicObservability:
		return "Observability"
	case TopicAgentRules:
		return "Instructions for agents"
	}
	return string(t)
}

// renderFinding writes one bullet, with its citation or its search as a nested line.
//
// The marker distinguishes the two kinds at a glance and does it with a word rather than a
// symbol: an agent reads this as text, and "not declared" survives being summarised where a
// bare "✗" does not.
func renderFinding(f Finding) string {
	var b strings.Builder
	b.WriteString("- ")
	if !f.Declared {
		b.WriteString("**Not declared.** ")
	}
	b.WriteString(f.Text)
	b.WriteString("\n")

	if f.Declared {
		if len(f.Sources) > 0 {
			b.WriteString("  - Stated in " + renderSources(f.Sources) + "\n")
		}
		return b.String()
	}
	if len(f.Looked) > 0 {
		// Where signpost looked, on an absence. Without it the reader cannot tell a
		// repository that does not declare a test command from a repository whose build
		// system signpost does not read — and those call for opposite actions.
		b.WriteString("  - Looked in " + renderLooked(f.Looked) + "\n")
	}
	return b.String()
}

// maxRenderedSources bounds a citation list.
//
// A finding backed by forty files does not become more credible with forty paths after it,
// and the page is read by something with a context window. The overflow is counted out loud:
// a list that silently stopped at six would read as complete.
const maxRenderedSources = 6

func renderSources(srcs []Source) string {
	shown := srcs
	extra := 0
	if len(shown) > maxRenderedSources {
		extra = len(shown) - maxRenderedSources
		shown = shown[:maxRenderedSources]
	}
	parts := make([]string, 0, len(shown))
	for _, s := range shown {
		// Inline code, not a link. A bundle-absolute link would have to name a page, and
		// these are repository paths — verify resolves bundle links and would fail on one
		// pointing at a source file. Code span also makes a path with markdown characters in
		// it read as itself.
		p := "`" + s.Path + "`"
		if s.Line > 0 {
			p += " line " + itoa(s.Line)
		}
		parts = append(parts, p)
	}
	s := joinProse(parts)
	if extra > 0 {
		s += ", and " + plural(extra, "other file")
	}
	return s + "."
}

func renderLooked(names []string) string {
	shown := names
	extra := 0
	if len(shown) > maxRenderedSources {
		extra = len(shown) - maxRenderedSources
		shown = shown[:maxRenderedSources]
	}
	parts := make([]string, 0, len(shown))
	for _, n := range shown {
		// Some entries are filenames and some are descriptions of where signpost looked
		// ("Makefile targets"). A filename gets a code span; a description does not, because
		// code-spanning a phrase makes it read as something to type.
		if looksLikePath(n) {
			parts = append(parts, "`"+n+"`")
			continue
		}
		parts = append(parts, n)
	}
	s := joinProse(parts)
	if extra > 0 {
		s += ", and " + plural(extra, "other place")
	}
	return s + "."
}

// looksLikePath distinguishes a filename from a phrase, for code-span purposes only.
//
// A space is the discriminator: every filename in this package's Looked lists is
// space-free, and every prose description has one. Wrong on a filename containing a space,
// which renders as prose instead of code — a cosmetic miss in a hint, not a correctness
// problem, and worth accepting over a rule that has to know what a path looks like.
func looksLikePath(s string) bool { return !strings.Contains(s, " ") }

// joinProse renders a list with a final "and", or a plain join beyond three items.
//
// Serial "and" only up to three, because past that the sentence stops reading as a sentence
// and a comma list is easier to scan.
func joinProse(parts []string) string {
	switch len(parts) {
	case 0:
		return "nowhere"
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	}
	if len(parts) == 3 {
		return parts[0] + ", " + parts[1] + ", and " + parts[2]
	}
	return strings.Join(parts, ", ")
}
