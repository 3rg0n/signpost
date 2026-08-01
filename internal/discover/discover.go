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
	// maxTotalBytes caps the whole walk. A repo that exceeds it is still
	// processed, but remaining files are recorded as skipped rather than read,
	// so a pathological tree cannot exhaust memory.
	maxTotalBytes = 512 << 20 // 512 MiB
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
}

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

	res := &Result{Root: absRoot}

	base := &ignoreSet{}
	// .git is never interesting as content; git signals come from `git log`.
	base.add(mustCompile("/.git/"))
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
	if *totalBytes >= maxTotalBytes {
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

// Sources returns only the analysable source files: not vendored, not binary,
// with content. This is what the language extractors consume.
func (r *Result) Sources() []File {
	var out []File
	for _, f := range r.Files {
		if f.Class == ClassSource && !f.Vendored && !f.Binary && f.Content != "" {
			out = append(out, f)
		}
	}
	return out
}

// ByClass returns files of a given class, excluding vendored and binary ones.
func (r *Result) ByClass(c Class) []File {
	var out []File
	for _, f := range r.Files {
		if f.Class == c && !f.Vendored && !f.Binary {
			out = append(out, f)
		}
	}
	return out
}
