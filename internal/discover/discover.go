// Package discover walks a repository and decides what exists: which files
// signpost will analyse, what kind of thing each one is, and how much of each is
// safe to read.
//
// It is the first stage of the pipeline (design §4.0) and every later stage
// consumes its output, so two properties are load-bearing:
//
//   - Deterministic. Files come back in sorted order with no dependence on
//     filesystem enumeration order, because the bundle is committed and any
//     instability here becomes commit churn (design §8.1).
//   - Bounded. A repository is untrusted input: it can contain a 4 GB generated
//     file, a symlink loop, or a file that is one 200 MB line. None of those may
//     turn a build into an OOM or a hang.
package discover

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Size caps, mirroring codeatlas (design §4.0). A file within both caps is
// ingested whole; a file over either is recorded with metadata plus a head/tail
// slice so it still contributes structure without being fully read.
const (
	MaxFullBytes = 2 << 20 // 2 MiB
	MaxFullLines = 50_000
	// HeadTailBytes is how much is read from each end of an oversized file.
	HeadTailBytes = 32 << 10 // 32 KiB
	// sniffBytes is how much is read to decide text vs binary.
	sniffBytes = 8000
	// DefaultMaxTotalBytes caps the whole walk. A repo that exceeds it is still
	// processed, but remaining files are recorded as skipped rather than read,
	// so a pathological tree cannot exhaust memory.
	//
	// A default rather than the limit, and exported, because no single number is right
	// for every tree: file contents are held in memory for the whole analysis, so this
	// is the ceiling on how much of a tree gets read at all. Options.MaxTotalBytes
	// overrides it in either direction. Exported so the flag that does so can state
	// the default it is replacing rather than restating the number.
	//
	// Raised from 512 MiB, which truncated the case this tool is for. A monorepo of
	// roughly 275,000 files recorded 170,530 of them as skipped and reported its own
	// first-party packages as unresolved imports, because the files defining them were
	// never opened — and a partial map is the failure that looks like success, since
	// nothing downstream can tell an absent module from one that does not exist. The
	// flag was the remedy for a truncated walk and it is still the remedy for a larger
	// one, but a default that leaves a repository this tool is aimed at three-fifths
	// unread is the wrong default. Six times the budget is chosen against that ratio
	// rather than against a measured run of the whole tree, so it is a better default
	// and not a guarantee: a tree large enough still truncates, and still says so.
	//
	// It is a memory ceiling and reads as one: the number is what a walk may hold, not
	// what it will, and every tree small enough to finish under 512 MiB allocates
	// exactly what it did before. Raising it costs nothing until a tree is large enough
	// to need it, which is the direction the trade should run.
	DefaultMaxTotalBytes = 3 << 30 // 3 GiB
)

// File is one discovered file.
type File struct {
	// Path is slash-separated and relative to the walk root, on every platform.
	// Windows backslashes are normalised here so that every downstream artifact
	// — node IDs, OKF links, manifest.json — is byte-identical across platforms.
	Path  string
	Class Class
	Lang  Lang
	Size  int64
	Lines int

	// Content is the file's text. Empty for binaries and for files skipped by the
	// total-bytes cap. For oversized files it holds head and tail separated by
	// Elision, and Truncated is set.
	Content   string
	Truncated bool

	// IsTest marks test files: kept for tested_by edges, never counted as
	// production surface.
	IsTest bool
	// Vendored marks third-party code committed into the tree. Discovered for
	// the record, excluded from analysis.
	Vendored bool
	// Fixture marks a sample project kept for tests to run against — testdata/
	// and friends. Discovered for the record, excluded from analysis: its modules
	// and dependencies belong to the sample, not to this repository. See
	// isFixture for why this is neither Vendored nor IsTest.
	Fixture bool
	// Binary marks a file whose content was not read.
	Binary bool
}

// Elision separates the head and tail of a truncated file. It is a comment in
// no language, so an extractor cannot mistake it for code.
const Elision = "\n\n<<< signpost: content elided >>>\n\n"

// Result is the outcome of a walk.
type Result struct {
	Root  string
	Files []File

	// Skipped records paths that were deliberately not read, with a reason.
	// Surfaced in manifest.json so the bundle never presents an incomplete walk
	// as a complete one (design §4.2: absence of measurement is never a clean
	// bill of health).
	Skipped []Skip

	// IncludeVendored carries Options.IncludeVendored forward, so the consumers
	// that filter on File.Vendored can honour the flag. Without it they cannot:
	// every one of them holds the walk's result and not the options that produced
	// it, which is how -include-vendored spent v0.1.0 reading vendored files that
	// nothing downstream would look at.
	IncludeVendored bool
}

// Analyses reports whether f is content this walk was asked to analyse.
//
// The one place the vendored decision is made, for the reason issue #11 records: it
// was previously made independently at six call sites, each spelled `!f.Vendored`
// with no reference to the option, so the flag that exists to overrule it overruled
// nothing. Consumers keep their own other conditions — a test file, a class, a
// binary — and defer only this one.
//
// Two of the six decide something; the rest are belt and braces. Sources() gates
// extraction and manifest.Registry.Run gates the manifest readers, and reverting
// either one alone is observable in the bundle. The others — ByClass, practice's two
// counts, semantic's source picker — only ever see a vendored file when one of those
// two let it through, because with the flag off the walk prunes vendored directories
// entirely and nothing vendored reaches a consumer at all. They are kept honest
// anyway: a filter that contradicted this one would be a hole waiting for the day the
// walk changes.
//
// Deliberately not a question about fixtures as well. A fixture is pruned during the
// walk and never reaches a consumer, so there is no downstream filter to satisfy and
// nothing here to ask.
func (r *Result) Analyses(f File) bool { return !f.Vendored || r.IncludeVendored }

// Skip is one path that was not read, and why.
type Skip struct {
	Path   string
	Reason string
}

// Options configures a walk.
type Options struct {
	// IncludeVendored analyses vendored code instead of only recording it.
	IncludeVendored bool
	// IncludeFixtures analyses sample projects under testdata/ instead of only
	// recording them.
	//
	// The escape hatch for isFixture guessing wrong about a directory genuinely
	// named `fixtures`, and the counterpart to IncludeVendored. Notably *not* what
	// the corpus harness uses: it copies testdata/corpus to a root of its own, so
	// those files arrive as `go/greeter/...` with no `testdata` segment to match.
	// Analysing a fixture in place and analysing it as its own repository are
	// different things, and only the second gives it correct module paths.
	IncludeFixtures bool
	// ExtraIgnores are additional .gitignore-syntax patterns applied at the root.
	ExtraIgnores []string

	// MaxTotalBytes overrides DefaultMaxTotalBytes for this walk. Zero or negative
	// means the default.
	//
	// Not an "unlimited" option, deliberately: contents are held in memory, so an
	// uncapped walk of an arbitrarily large tree is an out-of-memory kill rather than
	// a slow success, and the caller is better placed to know how much memory it has
	// than this package is. Raising the cap is a number the caller states; removing it
	// is not offered.
	MaxTotalBytes int64
}

// maxTotal is the byte budget in effect. A method rather than a value normalised in
// Walk, because readFile is where the budget is consulted and it holds the Options —
// so a zero-value Options used directly in a test gets the default too, rather than a
// budget of nothing that skips every file in the tree.
func (o Options) maxTotal() int64 {
	if o.MaxTotalBytes > 0 {
		return o.MaxTotalBytes
	}
	return DefaultMaxTotalBytes
}

// Walk discovers files under root.
//
// It honours .gitignore at every level, skips binaries, applies the size caps,
// and returns files sorted by path. Symlinks are recorded but never followed:
// following them invites both cycles and escapes outside the root, and a symlink
// target inside the repo is discovered on its own anyway.
//
// Every read goes through an os.Root scoped to the tree. The symlink skip above
// already prevents an escape, but that is an argument about the code being right,
// and this is a guarantee from the kernel handle instead. It is worth the
// difference here because signpost reads a tree it does not control and commits
// what it found: a path that escaped the root would put content from outside the
// repository into a file that gets pushed.
func Walk(root string, opts Options) (*Result, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root is not a directory: %s", absRoot)
	}
	rt, err := os.OpenRoot(absRoot)
	if err != nil {
		return nil, fmt.Errorf("open root: %w", err)
	}
	defer func() { _ = rt.Close() }()

	res := &Result{Root: absRoot, IncludeVendored: opts.IncludeVendored}

	base := &ignoreSet{}
	// .git is never interesting as content; git signals come from `git log`.
	base.add(mustCompile("/.git/"))
	// The bundle is signpost's own output, and reading it back makes the tool describe
	// itself rather than the repository. It is committed (ADR 0005), so it is not caught
	// by .gitignore the way a build directory is — which is what let this through.
	//
	// The graph was never affected: a bundle page produces no node, because a markdown
	// file only becomes one by being a document signpost decided to record, and these are
	// pages *about* those documents. What it did affect is the census on stderr, which is
	// the one number a user has for judging whether the map covers their repository:
	// `analysed 223 files` on a repository with 143 of them, the difference being the 82
	// pages of the previous run. That number growing every time the bundle grows is worse
	// than wrong, because it moves in the direction that reads as better coverage.
	//
	// Spelled literally rather than as okf.BundleDir: this package imports nothing else in
	// the module, and that is worth keeping — discovery is the layer everything else is
	// built on. The name is fixed by ADR 0005 and asserted against okf.BundleDir in a test.
	base.add(mustCompile("/.signpost/"))
	for _, raw := range opts.ExtraIgnores {
		if p, ok := compilePattern(raw, ""); ok {
			base.add(p)
		}
	}

	var totalBytes int64
	if err := walkDir(rt, "", base, opts, res, &totalBytes); err != nil {
		return nil, err
	}

	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })
	sort.Slice(res.Skipped, func(i, j int) bool { return res.Skipped[i].Path < res.Skipped[j].Path })
	return res, nil
}

// walkDir recurses one directory. ig is the ignore set in effect, already
// including any parent .gitignore files. relDir is the directory's path relative
// to the root, "" for the root itself; every read is addressed through rt by that
// relative path, so no name the walk constructs can leave the tree.
func walkDir(rt *os.Root, relDir string, ig *ignoreSet, opts Options, res *Result, totalBytes *int64) error {
	entries, err := readDirIn(rt, relDir)
	if err != nil {
		// An unreadable directory is recorded, not fatal: a permission-denied
		// subtree should not fail a whole build.
		res.Skipped = append(res.Skipped, Skip{Path: relDir, Reason: "unreadable directory: " + err.Error()})
		return nil
	}

	// A .gitignore here applies to this directory and below, layered after the
	// parents so the deepest file wins.
	local := ig
	for _, e := range entries {
		if e.Name() == ".gitignore" && !e.IsDir() {
			f, err := rt.Open(joinIn(relDir, ".gitignore"))
			if err != nil {
				break
			}
			pats := parseIgnore(f, relDir)
			_ = f.Close()
			if len(pats) > 0 {
				local = ig.clone()
				local.add(pats...)
			}
			break
		}
	}

	// Sort entries so recursion order is fixed. This is load-bearing, not
	// belt-and-braces: reading through an os.Root handle means File.ReadDir, which
	// returns entries in directory order and does not sort them the way os.ReadDir
	// does. Without this the walk order follows the filesystem and the bundle stops
	// being byte-reproducible.
	names := make([]fs.DirEntry, len(entries))
	copy(names, entries)
	sort.Slice(names, func(i, j int) bool { return names[i].Name() < names[j].Name() })

	for _, e := range names {
		name := e.Name()
		rel := name
		if relDir != "" {
			rel = relDir + "/" + name
		}

		// A symlink is never followed, in either the file or directory case.
		if e.Type()&fs.ModeSymlink != 0 {
			res.Skipped = append(res.Skipped, Skip{Path: rel, Reason: "symlink not followed"})
			continue
		}

		if e.IsDir() {
			if local.match(rel, true) {
				continue
			}
			// isVendored and isFixture inspect directory segments, so append a
			// dummy filename to test rel itself as a directory.
			if !opts.IncludeVendored && isVendored(rel+"/x") {
				res.Skipped = append(res.Skipped, Skip{Path: rel, Reason: "vendored directory"})
				continue
			}
			if !opts.IncludeFixtures && isFixture(rel+"/x") {
				// A distinct reason, not folded into "vendored". Skipped paths are
				// surfaced in manifest.json so the bundle never presents an
				// incomplete walk as a complete one, and "vendored" would tell a
				// reader their own reviewed fixture is somebody else's code.
				res.Skipped = append(res.Skipped, Skip{Path: rel, Reason: "test fixture directory"})
				continue
			}
			if err := walkDir(rt, rel, local, opts, res, totalBytes); err != nil {
				return err
			}
			continue
		}

		// Irregular files (devices, sockets, FIFOs) are not content. Reading a
		// FIFO would block forever.
		if !e.Type().IsRegular() {
			res.Skipped = append(res.Skipped, Skip{Path: rel, Reason: "not a regular file"})
			continue
		}
		if local.match(rel, false) {
			continue
		}

		f, skip := readFile(rt, rel, opts, totalBytes)
		if skip != nil {
			res.Skipped = append(res.Skipped, *skip)
			continue
		}
		res.Files = append(res.Files, f)
	}
	return nil
}

// readDirIn lists a directory addressed by its path relative to rt.
//
// os.Root has no ReadDir, so the directory is opened through the root — which is
// where the confinement is enforced — and read from the resulting handle.
func readDirIn(rt *os.Root, relDir string) ([]fs.DirEntry, error) {
	name := relDir
	if name == "" {
		name = "."
	}
	d, err := rt.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = d.Close() }()
	return d.ReadDir(-1)
}

// joinIn builds a root-relative path. os.Root takes slash-separated names on
// every platform, so this is deliberately not filepath.Join: on Windows that
// would produce a backslash and change what the name means.
func joinIn(relDir, name string) string {
	if relDir == "" {
		return name
	}
	return relDir + "/" + name
}

// readFile stats, sniffs, and reads one file subject to the caps. rel is the
// path relative to the root rt, which is how the read is confined to the tree.
func readFile(rt *os.Root, rel string, opts Options, totalBytes *int64) (File, *Skip) {
	info, err := rt.Stat(rel)
	if err != nil {
		return File{}, &Skip{Path: rel, Reason: "stat failed: " + err.Error()}
	}

	class, lang := classify(rel)
	f := File{
		Path:     rel,
		Class:    class,
		Lang:     lang,
		Size:     info.Size(),
		IsTest:   isTestPath(rel, lang),
		Vendored: isVendored(rel),
		Fixture:  isFixture(rel),
	}

	// Vendored files are recorded with metadata only. Reading them would cost
	// the majority of the walk's IO for nodes nobody can act on.
	if f.Vendored && !opts.IncludeVendored {
		return f, nil
	}
	// A fixture is recorded the same way, for a different reason: reading it is
	// cheap, but analysing it puts the sample project's modules and dependencies
	// on this repository's pages as though they were its own.
	if f.Fixture && !opts.IncludeFixtures {
		return f, nil
	}
	if info.Size() == 0 {
		return f, nil
	}
	if *totalBytes >= opts.maxTotal() {
		return File{}, &Skip{Path: rel, Reason: "walk byte budget exhausted"}
	}

	fh, err := rt.Open(rel)
	if err != nil {
		return File{}, &Skip{Path: rel, Reason: "open failed: " + err.Error()}
	}
	defer func() { _ = fh.Close() }()

	head := make([]byte, min64(sniffBytes, info.Size()))
	n, err := io.ReadFull(fh, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return File{}, &Skip{Path: rel, Reason: "read failed: " + err.Error()}
	}
	head = head[:n]

	if isBinary(head) {
		f.Binary = true
		return f, nil
	}

	oversized := info.Size() > MaxFullBytes
	if !oversized {
		// Read the remainder. The file is at most MaxFullBytes, so this is
		// bounded by construction.
		rest, err := io.ReadAll(fh)
		if err != nil {
			return File{}, &Skip{Path: rel, Reason: "read failed: " + err.Error()}
		}
		content := string(append(head, rest...))
		lines := countLines(content)
		if lines > MaxFullLines {
			// Within the byte cap but over the line cap: a generated file, a
			// minified bundle, or a data dump. Truncate by lines.
			f.Content = headTailLines(content, MaxFullLines)
			f.Truncated = true
			f.Lines = lines
			*totalBytes += int64(len(f.Content))
			return f, nil
		}
		f.Content = normalizeNewlines(content)
		f.Lines = lines
		*totalBytes += int64(len(f.Content))
		return f, nil
	}

	// Oversized: head plus tail, so an extractor still sees the imports at the
	// top and any trailing structure, without holding the whole file.
	headPart := make([]byte, HeadTailBytes)
	copy(headPart, head)
	if int64(len(head)) < HeadTailBytes {
		extra := headPart[len(head):]
		hn, _ := io.ReadFull(fh, extra)
		headPart = headPart[:len(head)+hn]
	}
	tail := make([]byte, HeadTailBytes)
	tn, err := readTail(fh, info.Size(), tail)
	if err != nil {
		tn = 0
	}
	f.Content = normalizeNewlines(string(headPart)) + Elision + normalizeNewlines(string(tail[:tn]))
	f.Truncated = true
	f.Lines = -1 // unknown: the middle was never read
	*totalBytes += int64(len(f.Content))
	return f, nil
}

// readTail reads the last len(buf) bytes of a file, trimming any leading partial
// UTF-8 sequence so the result is always valid text.
func readTail(fh *os.File, size int64, buf []byte) (int, error) {
	off := size - int64(len(buf))
	if off < 0 {
		off = 0
	}
	n, err := fh.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return 0, err
	}
	b := buf[:n]
	// Drop bytes until the slice starts on a rune boundary.
	for len(b) > 0 && !utf8.RuneStart(b[0]) {
		b = b[1:]
	}
	copy(buf, b)
	return len(b), nil
}

// isBinary reports whether a byte prefix looks like binary content.
//
// A NUL byte is the decisive signal — git uses the same heuristic — and invalid
// UTF-8 past a threshold catches the rest. Source code in any encoding signpost
// can analyse is valid UTF-8.
func isBinary(head []byte) bool {
	if len(head) == 0 {
		return false
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	// A truncated final rune is expected when sniffing a prefix, so validate
	// only the part up to the last rune start.
	b := head
	for i := len(b) - 1; i >= 0 && i > len(b)-utf8.UTFMax-1; i-- {
		if utf8.RuneStart(b[i]) {
			b = b[:i]
			break
		}
	}
	return !utf8.Valid(b)
}

// countLines counts lines, counting a final unterminated line.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// headTailLines keeps the first and last half of maxLines lines.
//
// The elision marker carries newlines of its own, so its cost comes out of the
// budget rather than being added on top: the point of the cap is that the result
// is never larger than maxLines, and a cap that overshoots by a constant is a cap
// that was not measured.
func headTailLines(s string, maxLines int) string {
	lines := strings.Split(normalizeNewlines(s), "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	budget := maxLines - strings.Count(Elision, "\n")
	if budget < 2 {
		budget = 2
	}
	half := budget / 2
	head := lines[:half]
	tail := lines[len(lines)-half:]
	return strings.Join(head, "\n") + Elision + strings.Join(tail, "\n")
}

// normalizeNewlines converts CRLF and lone CR to LF.
//
// Not cosmetic: a Windows checkout with core.autocrlf produces CRLF on disk, and
// without this the same commit yields different extracted content — and so a
// different bundle — than a Linux runner. That is the determinism requirement in
// design §8.1, enforced at the point content enters the pipeline.
func normalizeNewlines(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func min64(a int64, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// mustCompile compiles a pattern known to be valid at build time.
func mustCompile(raw string) pattern {
	p, ok := compilePattern(raw, "")
	if !ok {
		panic("discover: invalid built-in ignore pattern: " + raw)
	}
	return p
}

// Sources returns only the analysable source files: not vendored unless the walk
// was asked for vendored code, not binary, with content. This is what the language
// extractors consume, which makes it the method -include-vendored has to reach —
// extraction is driven from here, so a vendored file excluded at this point is a
// vendored file no extractor ever sees whatever the flag said.
func (r *Result) Sources() []File {
	var out []File
	for _, f := range r.Files {
		if f.Class == ClassSource && r.Analyses(f) && !f.Binary && f.Content != "" {
			out = append(out, f)
		}
	}
	return out
}

// Unclassified counts the files this walk could not name, keyed by extension and
// by basename for the extensionless, highest count first being the caller's job.
//
// ClassOther is the one classification that means "signpost does not know what this
// is": every other class routes to an extractor or a manifest reader, and the two
// that can still come back empty-handed report it themselves — extract.RunResult and
// manifest.RunResult both carry an Unhandled map. ClassOther had no such counterpart,
// so a file landing here left the pipeline with nothing recording that it had. On a
// repository whose only frontend source was two `.astro` files, that made the coverage
// report name `.sh` and `.sql` while the pages it did not read went unmentioned — the
// silence design §4.2 exists to forbid.
//
// Binaries are excluded. A `.png` is not a gap in coverage: it was classified
// correctly and there is nothing in it to read, and counting it would bury the
// extensions that are gaps under the ones that never could be.
func (r *Result) Unclassified() map[string]int {
	out := map[string]int{}
	for _, f := range r.Files {
		if f.Class != ClassOther || f.Binary || !r.Analyses(f) {
			continue
		}
		out[unclassifiedKey(f.Path)]++
	}
	return out
}

// unclassifiedKey names a file's shape the way manifest.unhandledKey does: an
// extension groups the actionable case, and a basename covers the files whose name is
// the only thing identifying them — `.gitignore`, `.helmignore`.
// Slash-separated throughout, per File.Path, so this is path and not path/filepath:
// on Linux filepath would read a backslash as an ordinary character in a name.
func unclassifiedKey(rel string) string {
	base := path.Base(rel)
	if e := path.Ext(base); e != "" {
		return strings.ToLower(e)
	}
	return strings.ToLower(base)
}

// ByClass returns files of a given class, excluding binaries and — unless the walk
// was asked for them — vendored ones.
func (r *Result) ByClass(c Class) []File {
	var out []File
	for _, f := range r.Files {
		if f.Class == c && r.Analyses(f) && !f.Binary {
			out = append(out, f)
		}
	}
	return out
}
