package extract

import (
	"strings"
	"testing"

	"github.com/cisco-sbg-emu/signpost/internal/discover"
)

// goFile wraps source as a discovered file.
func goFile(path, src string) discover.File {
	return discover.File{
		Path: path, Lang: discover.LangGo, Class: discover.ClassSource, Content: src,
	}
}

func extractGo(t *testing.T, path, src string) Facts {
	t.Helper()
	fa, err := GoExtractor{}.Extract(goFile(path, src))
	if err != nil {
		t.Fatalf("Extract(%s): %v", path, err)
	}
	fa.Normalize()
	return fa
}

// The scored corpus. Every fixture is hand-labeled, and the labels are the
// contract: design §4.2 requires the score to be measured, and this is the
// measurement for Go. Because go/parser is the real parser, this corpus doubles
// as the definition of correct that the other languages are held to.
func goCorpus() []Fixture {
	return []Fixture{
		{
			File: goFile("simple.go", `
// Package simple does one thing.
package simple

import (
	"fmt"
	"os"
)

// Run does the work.
func Run() error {
	fmt.Println(os.Args)
	return nil
}

func helper() {}
`),
			Expected: Expected{
				Package:     "simple",
				Imports:     []string{"fmt", "os"},
				Symbols:     []string{"Run", "helper"},
				Exported:    []string{"Run"},
				Entrypoints: []string{},
			},
		},
		{
			File: goFile("cmd/main.go", `
package main

import (
	"flag"
	_ "net/http/pprof"
	spr "github.com/cisco-sbg-emu/signpost/internal/graph"
)

func main() {
	flag.Parse()
	_ = spr.New()
}

func init() {
	flag.Bool("v", false, "verbose")
}
`),
			Expected: Expected{
				Package:     "main",
				Imports:     []string{"flag", "github.com/cisco-sbg-emu/signpost/internal/graph", "net/http/pprof"},
				Symbols:     []string{"init", "main"},
				Entrypoints: []string{"init", "main"},
			},
		},
		{
			File: goFile("types.go", `
package types

// Store persists things.
type Store interface {
	Get(k string) ([]byte, error)
	Put(k string, v []byte) error
}

// Client talks to a Store.
type Client struct {
	store Store
}

type internalCache struct{}

// Get implements Store.
func (c *Client) Get(k string) ([]byte, error) { return nil, nil }

// Put implements Store.
func (c Client) Put(k string, v []byte) error { return nil }

// unexportedMethod is not surface.
func (c *Client) unexportedMethod() {}

// Warm is exported but its receiver is not, so it is not public surface.
func (i *internalCache) Warm() {}

const (
	// MaxSize caps a value.
	MaxSize = 1024
	minSize = 1
)

var (
	ErrMissing = errorString("missing")
	debugMode  = false
	_          = MaxSize // blank: not a declaration
)

type errorString string

func (e errorString) Error() string { return string(e) }
`),
			Expected: Expected{
				Package: "types",
				Imports: []string{},
				Symbols: []string{
					"Client", "Client.Get", "Client.Put", "Client.unexportedMethod",
					"ErrMissing", "MaxSize", "Store", "debugMode", "errorString",
					"errorString.Error", "internalCache", "internalCache.Warm", "minSize",
				},
				Exported:    []string{"Client", "Client.Get", "Client.Put", "ErrMissing", "MaxSize", "Store"},
				Entrypoints: []string{},
			},
		},
		{
			File: goFile("generic.go", `
package generic

import "sort"

// List is generic.
type List[T any] struct{ items []T }

// Add appends.
func (l *List[T]) Add(v T) { l.items = append(l.items, v) }

// Sort orders in place.
func (l List[T]) Sort(less func(a, b T) bool) {
	sort.Slice(l.items, func(i, j int) bool { return less(l.items[i], l.items[j]) })
}

// Map transforms.
func Map[T, U any](in []T, f func(T) U) []U { return nil }
`),
			Expected: Expected{
				Package:     "generic",
				Imports:     []string{"sort"},
				Symbols:     []string{"List", "List.Add", "List.Sort", "Map"},
				Exported:    []string{"List", "List.Add", "List.Sort", "Map"},
				Entrypoints: []string{},
			},
		},
		{
			File: goFile("aliased.go", `
package aliased

import (
	"context"
	yaml "gopkg.in/yaml.v3"
	. "math"
)

// Parse reads config.
func Parse(ctx context.Context, b []byte) (map[string]any, error) {
	_ = Pi
	var m map[string]any
	return m, yaml.Unmarshal(b, &m)
}
`),
			Expected: Expected{
				Package:     "aliased",
				Imports:     []string{"context", "gopkg.in/yaml.v3", "math"},
				Symbols:     []string{"Parse"},
				Exported:    []string{"Parse"},
				Entrypoints: []string{},
			},
		},
	}
}

// The measurement design §4.2 promises. Go must be perfect: it uses the real
// parser, so anything less than 1.0 is a bug in the adapter, not an
// approximation to be tolerated.
func TestGoExtractorScoresPerfectOnCorpus(t *testing.T) {
	ls := ScoreExtractor(GoExtractor{}, discover.LangGo, goCorpus())
	if !ls.MeetsTarget() {
		t.Errorf("Go extractor below target:\n%s", ls.Report())
	}
	if ls.Imports.F1() != 1.0 {
		t.Errorf("imports F1 = %v, want exactly 1.0 (go/parser is the ground truth):\n%s",
			ls.Imports.F1(), ls.Report())
	}
	if ls.Symbols.F1() != 1.0 {
		t.Errorf("symbols F1 = %v, want exactly 1.0:\n%s", ls.Symbols.F1(), ls.Report())
	}
	t.Logf("Go extractor score:\n%s", ls.Report())
}

func TestGoExtractorPackageAndImports(t *testing.T) {
	fa := extractGo(t, "a.go", `
package auth

import (
	"errors"
	jwt "github.com/example/jwt"
)

func Verify(t string) error { return errors.New("x") }
`)
	if fa.Package != "auth" {
		t.Errorf("Package = %q, want auth", fa.Package)
	}
	if got := strings.Join(fa.ImportPaths(), ","); got != "errors,github.com/example/jwt" {
		t.Errorf("imports = %q", got)
	}
	// The alias is recorded, because it is how the code refers to the package.
	for _, im := range fa.Imports {
		if im.Raw == "github.com/example/jwt" && im.Alias != "jwt" {
			t.Errorf("alias = %q, want jwt", im.Alias)
		}
	}
}

// A blank import exists purely for its init side effects, which is exactly the
// invisible coupling the bundle should surface. It must not be dropped.
func TestGoExtractorKeepsBlankImports(t *testing.T) {
	fa := extractGo(t, "a.go", `
package main
import (
	_ "github.com/lib/pq"
	_ "embed"
)
func main() {}
`)
	got := strings.Join(fa.ImportPaths(), ",")
	if !strings.Contains(got, "github.com/lib/pq") || !strings.Contains(got, "embed") {
		t.Errorf("blank imports must be retained, got %q", got)
	}
	for _, im := range fa.Imports {
		if im.Alias != "_" {
			t.Errorf("blank import %q should record alias _, got %q", im.Raw, im.Alias)
		}
	}
}

// An exported method on an unexported type is not public surface: nothing
// outside the package can obtain a receiver to call it on.
func TestGoExtractorMethodOnUnexportedTypeIsNotExported(t *testing.T) {
	fa := extractGo(t, "a.go", `
package a
type hidden struct{}
func (h *hidden) Exported() {}
type Shown struct{}
func (s *Shown) Exported() {}
`)
	var hiddenMethod, shownMethod *Symbol
	for i := range fa.Symbols {
		s := &fa.Symbols[i]
		if s.Recv == "hidden" && s.Name == "Exported" {
			hiddenMethod = s
		}
		if s.Recv == "Shown" && s.Name == "Exported" {
			shownMethod = s
		}
	}
	if hiddenMethod == nil || shownMethod == nil {
		t.Fatalf("expected both methods to be found, got %+v", fa.Symbols)
	}
	if hiddenMethod.Exported {
		t.Error("a method on an unexported type must not be reported as exported")
	}
	if !shownMethod.Exported {
		t.Error("a method on an exported type should be exported")
	}
}

func TestGoExtractorDistinguishesInterfaces(t *testing.T) {
	fa := extractGo(t, "a.go", `
package a
type Reader interface { Read() }
type Impl struct{}
`)
	kinds := map[string]SymbolKind{}
	for _, s := range fa.Symbols {
		kinds[s.Name] = s.Kind
	}
	if kinds["Reader"] != SymInterface {
		t.Errorf("Reader kind = %q, want interface", kinds["Reader"])
	}
	if kinds["Impl"] != SymType {
		t.Errorf("Impl kind = %q, want type", kinds["Impl"])
	}
}

// Doc comments attach differently for a single declaration versus a group, and
// both forms must yield the doc.
func TestGoExtractorDocComments(t *testing.T) {
	fa := extractGo(t, "a.go", `
package a

// Single documents one type.
type Single struct{}

type (
	// Grouped documents a type inside a group.
	Grouped struct{}
)

// Multi has more than one sentence. This part is dropped.
func Multi() {}

//go:generate stringer -type=Directive
type Directive int
`)
	docs := map[string]string{}
	for _, s := range fa.Symbols {
		docs[s.Name] = s.Doc
	}
	if docs["Single"] != "Single documents one type." {
		t.Errorf("Single doc = %q", docs["Single"])
	}
	if docs["Grouped"] != "Grouped documents a type inside a group." {
		t.Errorf("Grouped doc = %q", docs["Grouped"])
	}
	if docs["Multi"] != "Multi has more than one sentence." {
		t.Errorf("Multi doc = %q, want only the first sentence", docs["Multi"])
	}
	// A //go:generate directive is machinery, not documentation.
	if docs["Directive"] != "" {
		t.Errorf("a directive comment must not become a doc string, got %q", docs["Directive"])
	}
}

func TestGoExtractorEntrypoints(t *testing.T) {
	cases := []struct {
		name, src string
		want      string
	}{
		{"main in package main", "package main\nfunc main() {}", "main"},
		// A func named main in a library package is not an entrypoint.
		{"main in library", "package lib\nfunc main() {}", ""},
		{"init anywhere", "package lib\nfunc init() {}", "init"},
		// init with a signature is an ordinary function.
		{"init with params", "package lib\nfunc init(x int) {}", ""},
		{"init with results", "package lib\nfunc init() error { return nil }", ""},
		// A method named main is not an entrypoint.
		{"method main", "package main\ntype T struct{}\nfunc (T) main() {}", ""},
	}
	for _, c := range cases {
		fa := extractGo(t, "a.go", c.src)
		got := strings.Join(fa.Entrypoints, ",")
		if got != c.want {
			t.Errorf("%s: entrypoints = %q, want %q", c.name, got, c.want)
		}
	}
}

// A generic receiver must reduce to its base type name, or every method on a
// generic type lands under a different node than the type itself.
func TestGoExtractorGenericReceivers(t *testing.T) {
	fa := extractGo(t, "a.go", `
package a
type Pair[K comparable, V any] struct{}
func (p *Pair[K, V]) Get(k K) V { var v V; return v }
func (p Pair[K, V]) Len() int { return 0 }
`)
	for _, s := range fa.Symbols {
		if s.Kind == SymMethod && s.Recv != "Pair" {
			t.Errorf("method %s has receiver %q, want Pair", s.Name, s.Recv)
		}
	}
}

// The blank identifier is a compile-time assertion, not a declaration.
func TestGoExtractorSkipsBlankIdentifier(t *testing.T) {
	fa := extractGo(t, "a.go", `
package a
type Store interface{ Get() }
type impl struct{}
func (impl) Get() {}
var _ Store = impl{}
`)
	for _, s := range fa.Symbols {
		if s.Name == "_" {
			t.Error("the blank identifier must not be reported as a symbol")
		}
	}
}

// A file that does not compile still yields its imports. Discarding them would
// mean a repo mid-refactor loses edges from the module graph.
func TestGoExtractorPartialParseYieldsFactsAndFlagsIncomplete(t *testing.T) {
	fa, err := GoExtractor{}.Extract(goFile("broken.go", `
package broken

import (
	"fmt"
	"os"
)

func Good() { fmt.Println(os.Args) }

func Broken( { this is not go
`))
	if err != nil {
		t.Fatalf("a partially parseable file should not be an error: %v", err)
	}
	fa.Normalize()
	if !fa.Incomplete {
		t.Error("a file with parse errors must be marked Incomplete")
	}
	if fa.Note == "" {
		t.Error("Incomplete should explain itself")
	}
	if got := strings.Join(fa.ImportPaths(), ","); got != "fmt,os" {
		t.Errorf("imports should survive a parse error, got %q", got)
	}
	if fa.Package != "broken" {
		t.Errorf("package should survive a parse error, got %q", fa.Package)
	}
}

// A file with no package clause at all is not Go; that is a genuine failure.
func TestGoExtractorRejectsUnparseableInput(t *testing.T) {
	_, err := GoExtractor{}.Extract(goFile("nope.go", "this is not go source at all\n{{{"))
	if err == nil {
		t.Error("input with no recoverable structure should return an error")
	}
}

func TestGoExtractorEmptyFile(t *testing.T) {
	fa, err := GoExtractor{}.Extract(goFile("empty.go", "package empty\n"))
	if err != nil {
		t.Fatalf("a package clause alone is valid Go: %v", err)
	}
	if fa.Package != "empty" {
		t.Errorf("Package = %q", fa.Package)
	}
	if len(fa.Symbols) != 0 || len(fa.Imports) != 0 {
		t.Errorf("expected no symbols or imports, got %+v", fa)
	}
}

// Extraction must be a pure function of content: same input, same output.
func TestGoExtractorIsDeterministic(t *testing.T) {
	src := `
package a
import ("os"; "fmt")
type T struct{}
func (t *T) B() {}
func (t *T) A() {}
func C() {}
const X = 1
`
	want := extractGo(t, "a.go", src)
	for i := 0; i < 20; i++ {
		got := extractGo(t, "a.go", src)
		if strings.Join(got.SymbolNames(), ",") != strings.Join(want.SymbolNames(), ",") {
			t.Fatalf("run %d: symbol order differs", i)
		}
		if strings.Join(got.ImportPaths(), ",") != strings.Join(want.ImportPaths(), ",") {
			t.Fatalf("run %d: import order differs", i)
		}
	}
}

// The extractor must handle its own source tree, which is the most realistic
// fixture available and costs nothing to test against.
func TestGoExtractorOnRealRepoFiles(t *testing.T) {
	res, err := discover.Walk("../..", discover.Options{})
	if err != nil {
		t.Skipf("cannot walk repo: %v", err)
	}
	sources := res.Sources()
	if len(sources) == 0 {
		t.Skip("no Go sources discovered")
	}

	r := NewRegistry()
	r.Register(GoExtractor{})
	run := r.Run(res)

	if len(run.Failures) > 0 {
		t.Errorf("signpost's own source should parse cleanly, got failures: %+v", run.Failures)
	}
	if len(run.Facts) == 0 {
		t.Fatal("expected facts from the repo's own Go files")
	}
	// Every fact should have a package; a Go file without one does not compile.
	for _, fa := range run.Facts {
		if fa.Package == "" {
			t.Errorf("%s: no package extracted", fa.Path)
		}
		if fa.Incomplete {
			t.Errorf("%s: unexpectedly incomplete: %s", fa.Path, fa.Note)
		}
	}
	t.Logf("extracted %d Go files from the repo itself", len(run.Facts))
}
