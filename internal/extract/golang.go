package extract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// GoExtractor reads Go source with the standard library's own parser.
//
// This is the precision baseline for the whole extraction layer (design §4.1):
// go/parser is the real thing, so whatever it reports is correct by definition,
// and the hand-written extractors for the other languages are scored against the
// standard this sets. It is also the case that costs nothing — the parser ships
// with the toolchain, so full fidelity here is free.
type GoExtractor struct{}

// Langs implements Extractor.
func (GoExtractor) Langs() []discover.Lang { return []discover.Lang{discover.LangGo} }

// Extract implements Extractor.
//
// Parsing is best-effort: go/parser returns a usable partial AST alongside an
// error for a file that does not compile, and partial facts from a broken file
// are strictly better than none. A file mid-refactor should still contribute its
// imports to the module graph, so the error becomes Incomplete rather than a
// failure that discards everything.
func (GoExtractor) Extract(f discover.File) (Facts, error) {
	fs := token.NewFileSet()
	// SkipObjectResolution: signpost needs declarations and imports, not
	// resolved identifier scopes, and skipping resolution is measurably faster
	// on a large repo.
	mode := parser.ParseComments | parser.SkipObjectResolution
	file, err := parser.ParseFile(fs, f.Path, f.Content, mode)
	// A failed parse still returns a non-nil *ast.File, with an empty package
	// name, so a nil check alone does not distinguish "recovered partially" from
	// "this is not Go". The package clause is the discriminator: no Go file
	// compiles without one, so its absence means nothing usable was recovered
	// and the caller should record a failure rather than empty facts.
	if file == nil || file.Name == nil || file.Name.Name == "" {
		return Facts{}, fmt.Errorf("parse %s: no package clause recovered: %w", f.Path, err)
	}

	facts := Facts{Path: f.Path, Lang: discover.LangGo}
	if err != nil {
		facts.Incomplete = true
		facts.Note = "parse errors; extraction is partial: " + firstErrorLine(err)
	}
	facts.Package = file.Name.Name

	for _, spec := range file.Imports {
		facts.Imports = append(facts.Imports, goImport(fs, spec))
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			facts.Symbols = append(facts.Symbols, goFuncSymbol(fs, d)...)
			if isGoEntrypoint(d, facts.Package) {
				facts.Entrypoints = append(facts.Entrypoints, d.Name.Name)
			}
		case *ast.GenDecl:
			facts.Symbols = append(facts.Symbols, goGenSymbols(fs, d)...)
		}
	}
	facts.addQueries(goStringLiterals(fs, file))
	return facts, nil
}

// goStringLiterals collects every string literal in a file, as the SQL reader wants them.
//
// The AST rather than the line scanner, for the same reason the rest of this extractor uses
// it: the parser already knows what a literal is, including a raw string spanning forty
// lines, and its position is exact. The concatenated form is the one case worth naming —
// `"DELETE FROM " + table` arrives here as its literal half alone, which is precisely what
// the reader reports as a gap rather than resolving, since resolving it needs the call graph
// ADR 0022 says this project does not have.
func goStringLiterals(fs *token.FileSet, file *ast.File) []sqlLiteral {
	var out []sqlLiteral
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		val, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		out = append(out, sqlLiteral{text: val, line: fs.Position(lit.Pos()).Line})
		return true
	})
	return out
}

// goImport converts one import spec.
func goImport(fs *token.FileSet, spec *ast.ImportSpec) Import {
	im := Import{Line: fs.Position(spec.Pos()).Line}
	// Path.Value carries the quotes; Unquote also handles the escape sequences
	// that a raw string comparison would get wrong.
	if p, err := strconv.Unquote(spec.Path.Value); err == nil {
		im.Raw = p
	} else {
		im.Raw = strings.Trim(spec.Path.Value, "`\"")
	}
	if spec.Name != nil {
		im.Alias = spec.Name.Name
	}
	// Whether an import is external depends on the module path, which lives in
	// go.mod and is not available here. Resolution fills that in later; leaving
	// it false keeps this extractor a pure function of one file.
	return im
}

// goFuncSymbol converts a function or method declaration.
//
// Interface satisfaction is deliberately not computed here. Go has no `implements`
// keyword, so establishing it requires type-checking the whole package against
// every interface in the repo — a different tool with a different cost profile.
// The graph gets implements edges from the interface's method set matched against
// receiver method sets during assembly, where all packages are visible at once.
func goFuncSymbol(fs *token.FileSet, d *ast.FuncDecl) []Symbol {
	if d.Name == nil {
		return nil
	}
	s := Symbol{
		Name:     d.Name.Name,
		Kind:     SymFunc,
		Exported: d.Name.IsExported(),
		Doc:      FirstSentence(docText(d.Doc)),
		Line:     fs.Position(d.Pos()).Line,
	}
	if d.Recv != nil && len(d.Recv.List) > 0 {
		s.Kind = SymMethod
		s.Recv = receiverTypeName(d.Recv.List[0].Type)
		// A method on an unexported type is not public surface even when the
		// method name is capitalised: nothing outside the package can obtain a
		// value to call it on. Getting this backwards would list half a package's
		// internals as its API.
		if !isExportedName(s.Recv) {
			s.Exported = false
		}
	}
	return []Symbol{s}
}

// goGenSymbols converts a type, const, or var declaration group.
func goGenSymbols(fs *token.FileSet, d *ast.GenDecl) []Symbol {
	var out []Symbol
	for _, spec := range d.Specs {
		switch sp := spec.(type) {
		case *ast.TypeSpec:
			s := Symbol{
				Name:     sp.Name.Name,
				Kind:     SymType,
				Exported: sp.Name.IsExported(),
				Line:     fs.Position(sp.Pos()).Line,
			}
			if _, ok := sp.Type.(*ast.InterfaceType); ok {
				s.Kind = SymInterface
			}
			// A single-spec group carries its doc on the GenDecl, not the spec:
			// `// Doc\ntype T struct{}`. A grouped declaration carries it per
			// spec. Checking both is what makes doc extraction work for both forms.
			s.Doc = FirstSentence(firstNonEmpty(docText(sp.Doc), docText(d.Doc)))
			out = append(out, s)

		case *ast.ValueSpec:
			kind := SymVar
			if d.Tok == token.CONST {
				kind = SymConst
			}
			for _, name := range sp.Names {
				if name.Name == "_" {
					// The blank identifier is a compile-time assertion or an
					// import side effect, not a declaration anyone can reference.
					continue
				}
				out = append(out, Symbol{
					Name:     name.Name,
					Kind:     kind,
					Exported: name.IsExported(),
					Doc:      FirstSentence(firstNonEmpty(docText(sp.Doc), docText(d.Doc))),
					Line:     fs.Position(name.Pos()).Line,
				})
			}
		}
	}
	return out
}

// isGoEntrypoint reports whether a declaration starts execution.
//
// `func main` in package main is the program entrypoint. `func init` is an
// entrypoint of a different and more interesting kind: it runs before main
// without being called from anywhere, so it is exactly the kind of invisible
// control flow an agent needs pointed out.
func isGoEntrypoint(d *ast.FuncDecl, pkg string) bool {
	if d.Recv != nil || d.Name == nil {
		return false
	}
	switch d.Name.Name {
	case "main":
		return pkg == "main"
	case "init":
		// init takes no parameters and returns nothing; anything else is an
		// ordinary function that happens to be called init.
		return d.Type.Params.NumFields() == 0 && d.Type.Results == nil
	}
	return false
}

// receiverTypeName extracts the base type name from a receiver, unwrapping
// pointers and generic type parameters: *Client, Client[T], *Client[T, U] all
// yield "Client".
func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return receiverTypeName(t.X)
	case *ast.IndexExpr: // generic with one type param
		return receiverTypeName(t.X)
	case *ast.IndexListExpr: // generic with several
		return receiverTypeName(t.X)
	case *ast.SelectorExpr:
		// A qualified receiver is not legal Go, but a malformed file can produce
		// one and it should not panic.
		return t.Sel.Name
	}
	return ""
}

// isExportedName reports whether a Go identifier is exported. ast.IsExported
// wants a valid identifier; an empty name from a malformed receiver is not.
func isExportedName(name string) bool {
	if name == "" {
		return false
	}
	return ast.IsExported(name)
}

// docText flattens a comment group, dropping the markers.
func docText(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	// ast.CommentGroup.Text already strips //, /* */, and directive comments
	// such as //go:generate — which is what we want, since a directive is not
	// documentation.
	return g.Text()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// firstErrorLine reduces a parser error list to its first entry, so a Note stays
// readable when a file has hundreds of cascading errors.
func firstErrorLine(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
