// Package semantic is design §4.5: the one pass in signpost that asks a model, and
// the rules that make its output safe to commit.
//
// Everything else reads code and reports what is provably there. This pass asks what a
// module is *for*, which no amount of AST parsing yields, and it is therefore the only
// place in the tool where the output is a guess. Four properties bound that guess, and
// each of them is a test in this package:
//
//   - **Grounded.** The model names the files its summary rests on, and every name is
//     checked against the files that were actually sent. A summary citing a file it was
//     not given is dropped whole rather than trimmed to its resolvable citations —
//     "dropped, not softened" is §4.5's wording, and a model inventing a filename has
//     told us something about the rest of its answer.
//   - **Attributed.** The rendered region names the model and the files it read. That
//     line travels with the prose rather than living in frontmatter, for the reason
//     Regions gives below: frontmatter is regenerated wholesale on every run, and this
//     fact has to survive a run with no backend.
//   - **Cached by content hash.** An unchanged module is never re-summarised, which is
//     what makes a scheduled semantic run byte-stable (§8.1) rather than merely similar.
//   - **Fails open.** An unreachable backend, a refused request, a response that will
//     not parse — none of them fail the build. The deterministic bundle is the product;
//     this pass improves it. What is not allowed is failing open *quietly*, so every
//     node this pass did not summarise is named in Skipped.
//
// # What this pass does not do
//
// §4.5 lists four outputs. This is the first: role summaries for module pages.
// Invariant extraction from ADRs, doc-to-code linking, and cluster labels are separate
// work and are not attempted here — a pass that half-did four things would make it
// impossible to say which of them is trustworthy.
package semantic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
	"github.com/3rg0n/signpost/internal/graph"
	"github.com/3rg0n/signpost/internal/model"
)

// Input is everything the pass reads.
type Input struct {
	// Graph selects what to summarise: module nodes, in the order the graph holds
	// them, which is sorted by ID.
	Graph *graph.Graph

	// Discovered supplies file content. The graph carries paths; the walk carries what
	// is in them.
	Discovered *discover.Result

	// Backend is the model. Required — a nil backend is the deterministic-only mode and
	// the caller is expected not to call Run at all, per model.New's contract.
	Backend model.Backend

	// CacheDir is where content-hash keyed summaries live. Passed in rather than derived
	// from the bundle path so this package does not import internal/okf: the emitter is
	// the deterministic core, and a dependency edge from it to the model path — even a
	// reversed one — is how a package that must never need a network ends up needing one.
	// Empty disables the cache, which costs money and breaks nothing.
	CacheDir string

	// Budget bounds the run. Zero fields take the defaults.
	Budget Budget
}

// Budget bounds what one pass may spend, per design §5's config.
//
// Two limits rather than one, because they fail differently: a call cap bounds a run on
// a large repository to a predictable number of round trips, and a token cap bounds the
// cost of a repository whose modules are unusually large. Either alone leaves the other
// unbounded.
type Budget struct {
	MaxCalls  int
	MaxTokens int
}

// Budget defaults, from design §5.
const (
	DefaultMaxCalls  = 400
	DefaultMaxTokens = 2_000_000
)

func (b Budget) calls() int {
	if b.MaxCalls > 0 {
		return b.MaxCalls
	}
	return DefaultMaxCalls
}

func (b Budget) tokens() int {
	if b.MaxTokens > 0 {
		return b.MaxTokens
	}
	return DefaultMaxTokens
}

// Summary is one grounded claim about one node.
type Summary struct {
	// Text is the prose, normalised to a single paragraph and length-capped.
	Text string
	// Cites are the repo-relative paths the summary rests on. Every one was sent to the
	// model and every one exists in the tree.
	Cites []string
	// Actor is the model that produced it, e.g. "signpost/0.1.0+google.gemma-3-12b".
	Actor string
}

// Result is what one pass produced, and what it did not.
type Result struct {
	// Summaries is keyed by graph node ID.
	Summaries map[string]Summary

	// Skipped names every node this pass did not summarise, and why, plus the reasons a
	// whole run stopped early. Design §4.2: the absence of a measurement is never a
	// clean bill of health, and a fail-open pass that fails silently is the one failure
	// mode that looks like success.
	Skipped []string

	// Truncated counts nodes summarised from capped input. Reported because the summary
	// describes part of the module rather than all of it, and nothing else on the page
	// would say so.
	Truncated int

	// Calls, Cached, InputTokens and OutputTokens are what the run spent. Cached is the
	// number of nodes answered without a round trip, which is the number that says the
	// cache is working.
	Calls, Cached             int
	InputTokens, OutputTokens int
}

// Input caps. A module page is a summary, not a code review, so the useful question is
// how much of a module a small model needs to see to say what it is for — and the answer
// is the declarations, which cluster at the top of a file. These caps put roughly 6k
// tokens in front of the model, which fits every context window signpost targets while
// leaving room for a schema and a system prompt.
//
// The alternative — send everything and let the endpoint reject it — fails at the worst
// possible moment: a 400 on the largest module in the repository, after the pass has
// already spent its budget on the small ones.
const (
	maxFilesPerNode = 8
	maxCharsPerFile = 6000
	maxCharsPerNode = 24000
)

// Run summarises every module node it can, within budget.
//
// It never returns an error. Every failure is a named entry in Skipped, because §5 makes
// fail-open the required behaviour and an error return here would make a broken backend
// break a merge — the one thing that must not happen.
func Run(ctx context.Context, in Input) *Result {
	res := &Result{Summaries: map[string]Summary{}}
	if in.Graph == nil || in.Discovered == nil || in.Backend == nil {
		return res
	}

	content := contentByPath(in.Discovered)
	c := &cache{dir: in.CacheDir}
	actor := in.Backend.Actor()

	maxCalls, maxTokens := in.Budget.calls(), in.Budget.tokens()
	// Counted rather than tracked per node: a node the budget stopped is not a node that
	// failed, and naming eighty of them individually would bury the one line that
	// explains all of them.
	overBudget := 0

	for _, n := range in.Graph.Nodes() {
		if n.Kind != graph.KindModule {
			continue
		}
		srcs, truncated := sourcesFor(n, in.Discovered, content)
		if len(srcs) == 0 {
			// A module node with no readable source: a directory of binaries, of vendored
			// code, or of files the size caps skipped. Not summarisable and not a failure.
			continue
		}
		key := cacheKey(actor, n.ID, srcs)
		if s, ok := c.get(key); ok {
			// Re-grounded on the way out of the cache rather than trusted. The cache is a
			// file in a working tree, so an entry whose citations no longer name files that
			// exist is exactly as untrustworthy as a fresh answer that never did.
			if s2, err := ground(s, srcs); err == nil {
				res.Summaries[n.ID] = s2
				res.Cached++
				if truncated {
					res.Truncated++
				}
				continue
			}
		}
		if res.Calls >= maxCalls || res.InputTokens+res.OutputTokens >= maxTokens {
			overBudget++
			continue
		}

		s, spent, err := ask(ctx, in.Backend, actor, n, srcs)
		res.Calls++
		res.InputTokens += spent.InputTokens
		res.OutputTokens += spent.OutputTokens
		if err != nil {
			// An unreachable or refusing backend stops the pass. Every remaining node
			// would fail the same way, and eighty identical round trips against an
			// endpoint that is down is a way to turn a fail-open into a hang.
			if errors.Is(err, model.ErrUnavailable) || isFault(err) {
				res.Skipped = append(res.Skipped, fmt.Sprintf(
					"the semantic pass stopped at %s: %v (run `signpost model check`)", n.ID, err))
				res.stoppedEarly(in.Graph, n.ID)
				return res
			}
			res.Skipped = append(res.Skipped, fmt.Sprintf("%s not summarised: %v", n.ID, err))
			continue
		}
		res.Summaries[n.ID] = s
		if truncated {
			res.Truncated++
		}
		if err := c.put(key, s); err != nil {
			// One line for the whole run, not one per node: a cache directory that cannot
			// be written fails the same way every time, and the consequence — every run
			// pays for every summary again — is one fact.
			res.noteOnce(fmt.Sprintf(
				"the summary cache could not be written, so the next run will re-ask: %v", err))
		}
	}

	if overBudget > 0 {
		res.Skipped = append(res.Skipped, fmt.Sprintf(
			"%d module(s) not summarised: the budget of %d call(s) and %d token(s) was reached",
			overBudget, maxCalls, maxTokens))
	}
	return res
}

// stoppedEarly names the modules a stopped run never reached.
//
// Counted from the node list rather than tracked, and reported as a count: the reader
// needs to know the bundle is partly summarised, and the reason is already on the line
// above.
func (r *Result) stoppedEarly(g *graph.Graph, from string) {
	remaining := 0
	past := false
	for _, n := range g.Nodes() {
		if n.ID == from {
			past = true
		}
		if past && n.Kind == graph.KindModule {
			if _, done := r.Summaries[n.ID]; !done {
				remaining++
			}
		}
	}
	if remaining > 0 {
		r.Skipped = append(r.Skipped, fmt.Sprintf(
			"%d further module(s) were not attempted after the backend failed", remaining))
	}
}

// noteOnce adds a skip line only if it is not already there.
func (r *Result) noteOnce(s string) {
	for _, have := range r.Skipped {
		if have == s {
			return
		}
	}
	r.Skipped = append(r.Skipped, s)
}

// isFault reports whether an error means signpost sent something wrong, rather than the
// backend being unavailable.
//
// The distinction decides whether to keep going. model.OpenAI already draws it — 401,
// 403, 404, 429 and 5xx are unavailable, and what is left is chiefly a 400 — so anything
// that is not ErrUnavailable and came from the transport layer is a request signpost
// built badly, and repeating it eighty times helps nobody.
func isFault(err error) bool {
	return strings.HasPrefix(err.Error(), "model: ")
}

// Regions renders each summary as the text of a page's managed `role` region.
//
// Keyed by node ID, which is what internal/okf indexes pages by. Rendering here rather
// than in the emitter keeps okf ignorant of this package: the emitter takes strings, so
// nothing in the deterministic path has to know a model exists.
func (r *Result) Regions() map[string]string {
	if r == nil || len(r.Summaries) == 0 {
		return nil
	}
	out := make(map[string]string, len(r.Summaries))
	for id, s := range r.Summaries {
		out[id] = s.render()
	}
	return out
}

// render writes the prose and the line that says where it came from.
//
// The attribution is in the region rather than in frontmatter, and that is the decision
// this file's design turns on. Frontmatter's generated keys are replaced wholesale on
// every run, so a `generated.by` naming a model would be overwritten by the next
// deterministic build — which is every push (§8) — while the prose it described stayed on
// the page. A reader would then see model prose attributed to the tool. Keeping the two
// together means a run that carries the region forward carries its provenance with it.
func (s Summary) render() string {
	var b strings.Builder
	b.WriteString(s.Text)
	b.WriteString("\n\n_Summary by `")
	b.WriteString(s.Actor)
	b.WriteString("`, from ")
	for i, p := range s.Cites {
		switch {
		case i == 0:
		case i == len(s.Cites)-1:
			b.WriteString(" and ")
		default:
			b.WriteString(", ")
		}
		b.WriteString("`" + p + "`")
	}
	b.WriteString(". Not reviewed by a human._\n")
	return b.String()
}

// contentByPath indexes the walk by path.
func contentByPath(d *discover.Result) map[string]discover.File {
	out := make(map[string]discover.File, len(d.Files))
	for _, f := range d.Files {
		out[f.Path] = f
	}
	return out
}

// sourcesFor picks and caps the files a node's summary may rest on.
//
// Largest first, then re-sorted by path. Both halves matter: the largest files carry most
// of a module's declarations, so they are the ones worth spending the cap on, and the
// prompt has to be in a stable order because the cache key is computed over it — a set
// that reordered between runs would miss the cache every time and produce a different
// summary each morning.
// d is passed alongside the index it was built from, for the one question the index
// cannot answer: whether this walk was asked to analyse vendored code.
func sourcesFor(n *graph.Node, d *discover.Result, content map[string]discover.File) (srcs []model.Source, truncated bool) {
	type cand struct {
		path string
		size int64
		text string
	}
	var cands []cand
	for _, p := range n.Files {
		f, ok := content[p]
		if !ok || f.Binary || !d.Analyses(f) || f.IsTest || strings.TrimSpace(f.Content) == "" {
			// Tests are excluded on purpose. A test file describes what a module must do,
			// which reads like a specification and is not one — and a summary built from
			// tests describes the tests.
			continue
		}
		cands = append(cands, cand{p, f.Size, f.Content})
		if f.Truncated {
			truncated = true
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].size != cands[j].size {
			return cands[i].size > cands[j].size
		}
		return cands[i].path < cands[j].path
	})
	if len(cands) > maxFilesPerNode {
		cands = cands[:maxFilesPerNode]
		truncated = true
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].path < cands[j].path })

	budget := maxCharsPerNode
	for _, c := range cands {
		if budget <= 0 {
			truncated = true
			break
		}
		text, cut := clip(c.text, min(maxCharsPerFile, budget))
		truncated = truncated || cut
		budget -= len(text)
		srcs = append(srcs, model.Source{Path: c.path, Content: text})
	}
	return srcs, truncated
}

// clip cuts a file to n bytes, keeping the head.
//
// The head rather than head-and-tail, which is what discover does for its own size caps:
// this is being read for what a module *is*, and in every language signpost supports the
// package clause, the imports, and the exported declarations are at the top. It ends on a
// line boundary so the model is not handed half a statement, and the elision is marked
// with discover's own marker — a comment in no language, so nothing downstream mistakes
// it for code.
func clip(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	cut := s[:n]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i+1]
	}
	return cut + discover.Elision, true
}

// cacheKey identifies a summary by everything that could change it.
//
// Actor, prompt version, node ID, and the path and content of every source. A key missing
// any of those would serve a summary of code that has changed, a summary from a different
// model, or a summary written to a prompt this version no longer sends — all three of
// which are the same bug: a committed claim about something other than what is there.
func cacheKey(actor, id string, srcs []model.Source) string {
	h := sha256.New()
	write := func(parts ...string) {
		for _, p := range parts {
			// Length-prefixed, so that two different splits of the same bytes cannot hash
			// alike. Without it a node named "ab" with a source "c" collides with a node
			// named "a" and a source "bc".
			_, _ = h.Write([]byte(strconv.Itoa(len(p)) + ":" + p))
		}
	}
	write(promptVersion, actor, id)
	for _, s := range srcs {
		write(s.Path, s.Content)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// cache is the content-hash keyed store design §4.5 and §8.1 require.
//
// Under .signpost/cache/, which is gitignored: the bundle is committed because it is
// useful without signpost installed, and a cache is useful to nothing but the next run.
// Its absence is a cost, never a correctness problem — a cold cache re-asks.
type cache struct{ dir string }

// cacheEntry is one stored summary. The parts rather than the rendered region, so a
// change to how the attribution line reads does not invalidate every entry.
//
// Field-for-field identical to Summary, so the two convert directly — and deliberately
// so: adding a field to one and not the other fails to compile rather than silently
// dropping it from every cached entry.
type cacheEntry struct {
	Text  string   `json:"text"`
	Cites []string `json:"cites"`
	Actor string   `json:"actor"`
}

func (c *cache) path(key string) string { return filepath.Join(c.dir, key+".json") }

// get reads an entry. A miss and an unreadable entry are the same answer: ask again.
func (c *cache) get(key string) (Summary, bool) {
	if c.dir == "" {
		return Summary{}, false
	}
	raw, err := os.ReadFile(c.path(key)) // #nosec G304 -- the name is a hex hash under the cache dir
	if err != nil {
		return Summary{}, false
	}
	var e cacheEntry
	if err := json.Unmarshal(raw, &e); err != nil || e.Text == "" {
		return Summary{}, false
	}
	return Summary(e), true
}

// put stores an entry.
//
// 0o700 and 0o600, unlike the bundle's 0o755 and 0o644: the bundle is committed and often
// published, and this is neither. Nothing in it is secret, but a local cache has no reason
// to be world-readable.
func (c *cache) put(key string, s Summary) error {
	if c.dir == "" {
		return nil
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(cacheEntry(s))
	if err != nil {
		return err
	}
	return os.WriteFile(c.path(key), raw, 0o600)
}
