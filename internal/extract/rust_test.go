package extract

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func rsFile(path, src string) discover.File {
	return discover.File{
		Path: path, Lang: discover.LangRust, Class: discover.ClassSource, Content: src,
	}
}

func extractRust(t *testing.T, path, src string) Facts {
	t.Helper()
	fa, err := RustExtractor{}.Extract(rsFile(path, src))
	if err != nil {
		t.Fatalf("Extract(%s): %v", path, err)
	}
	fa.Normalize()
	return fa
}

// The scored corpus. Hand-labeled against real Rust, including the nested use
// trees, trait impls, and lifetime syntax that are the ways a line matcher goes
// wrong here.
func rustCorpus() []Fixture {
	return []Fixture{
		{
			File: rsFile("src/lib.rs", `
//! Crate documentation.

use std::collections::{BTreeMap, HashMap};
use std::fmt;
use crate::graph::Node;
use super::shared::helper;
use serde::{Serialize as Ser, Deserialize};

pub mod graph;
mod internal;

/// The maximum size.
pub const MAX_SIZE: usize = 1024;
static COUNTER: usize = 0;

/// A client.
#[derive(Debug, Clone)]
pub struct Client {
    inner: HashMap<String, Node>,
}

pub enum Mode {
    Fast,
    Slow,
}

/// Persists things.
pub trait Store {
    fn get(&self, k: &str) -> Option<Vec<u8>>;
    fn put(&mut self, k: &str, v: Vec<u8>);
}

impl Client {
    /// Creates a client.
    pub fn new() -> Self {
        Self { inner: HashMap::new() }
    }

    fn private_helper(&self) -> usize {
        0
    }
}

impl fmt::Display for Client {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "client")
    }
}

pub type Result<T> = std::result::Result<T, Error>;

pub struct Error;

fn crate_private() {}
`),
			Expected: Expected{
				Package: "",
				Imports: []string{
					"crate::graph::Node", "self::graph", "self::internal",
					"serde::Deserialize", "serde::Serialize", "std::collections::BTreeMap",
					"std::collections::HashMap", "std::fmt", "super::shared::helper",
				},
				Symbols: []string{
					"COUNTER", "Client", "Client.fmt", "Client.new",
					"Client.private_helper", "Error", "MAX_SIZE", "Mode", "Result",
					"Store", "Store.get", "Store.put", "crate_private", "graph", "internal",
				},
				Exported: []string{
					"Client", "Client.new", "Error", "MAX_SIZE", "Mode", "Result",
					"Store", "Store.get", "Store.put", "graph",
				},
				Entrypoints: []string{},
			},
		},
		{
			// Lifetimes and generics, the syntax a naive scanner reads as an
			// unterminated string and then blanks the declaration behind.
			File: rsFile("src/parser.rs", `
use std::str::Chars;

pub struct Lexer<'a> {
    input: &'a str,
    chars: Chars<'a>,
}

impl<'a> Lexer<'a> {
    pub fn new(input: &'a str) -> Self {
        Self { input, chars: input.chars() }
    }

    pub fn peek(&mut self) -> Option<char> {
        self.chars.clone().next()
    }
}

impl<'a> Iterator for Lexer<'a> {
    type Item = char;

    fn next(&mut self) -> Option<char> {
        self.chars.next()
    }
}

pub fn longest<'a>(a: &'a str, b: &'a str) -> &'a str {
    if a.len() > b.len() { a } else { b }
}

pub fn boxed() -> Box<dyn std::error::Error + 'static> {
    todo!()
}
`),
			Expected: Expected{
				Package:     "parser",
				Imports:     []string{"std::str::Chars"},
				Symbols:     []string{"Lexer", "Lexer.new", "Lexer.next", "Lexer.peek", "boxed", "longest"},
				Exported:    []string{"Lexer", "Lexer.new", "Lexer.peek", "boxed", "longest"},
				Entrypoints: []string{},
			},
		},
		{
			// A binary crate root, and the modifier stacks that precede fn.
			File: rsFile("src/main.rs", `
use clap::Parser;
use tokio::runtime::Runtime;

#[derive(Parser)]
struct Args {
    verbose: bool,
}

pub async fn serve() -> anyhow::Result<()> {
    Ok(())
}

pub unsafe fn raw() {}

pub const fn compile_time() -> usize { 1 }

pub extern "C" fn exported_c() {}

fn main() {
    let rt = Runtime::new().unwrap();
    rt.block_on(serve()).unwrap();
}
`),
			Expected: Expected{
				Package: "",
				Imports: []string{"clap::Parser", "tokio::runtime::Runtime"},
				Symbols: []string{
					"Args", "compile_time", "exported_c", "main", "raw", "serve",
				},
				Exported:    []string{"compile_time", "exported_c", "raw", "serve"},
				Entrypoints: []string{"main"},
			},
		},
		{
			// Visibility restrictions, which are not public API.
			File: rsFile("src/visibility.rs", `
pub(crate) struct CrateOnly;
pub(super) fn parent_only() {}
pub(in crate::graph) fn scoped() {}
pub struct ReallyPublic;
pub fn really_public() {}
struct Private;
fn private() {}
`),
			Expected: Expected{
				Package: "visibility",
				Symbols: []string{
					"CrateOnly", "Private", "ReallyPublic", "parent_only", "private",
					"really_public", "scoped",
				},
				Imports:     []string{},
				Exported:    []string{"ReallyPublic", "really_public"},
				Entrypoints: []string{},
			},
		},
		{
			// The adversarial fixture.
			File: rsFile("src/tricky.rs", `
use real::thing;

const SNIPPET: &str = "
use fake::thing;
pub fn ghost() {}
pub struct Phantom;
";

const RAW: &str = r#"
use nowhere::nothing;
pub fn raw_ghost() {}
"#;

// use commented::out;
/* pub fn block_ghost() {} */
/* nested /* deeper */ still comment
   pub struct DeepGhost; */

/// Documented item.
#[derive(Debug)]
pub struct Documented;

pub fn outer() {
    fn nested_fn() {}
    struct NestedStruct;
    let c = 'x';
    let lifetime_looking = "it's fine";
}

pub fn implementation_detail() {}
`),
			Expected: Expected{
				Imports:     []string{"real::thing"},
				Symbols:     []string{"Documented", "RAW", "SNIPPET", "implementation_detail", "outer"},
				Exported:    []string{"Documented", "implementation_detail", "outer"},
				Entrypoints: []string{},
			},
		},
	}
}

// The measurement design §4.2 promises for Rust.
func TestRustExtractorMeetsTarget(t *testing.T) {
	ls := ScoreExtractor(RustExtractor{}, discover.LangRust, rustCorpus())
	if !ls.MeetsTarget() {
		t.Errorf("Rust extractor below target:\n%s", ls.Report())
	}
	t.Logf("Rust extractor score:\n%s", ls.Report())
}

// The Rust-specific case the scanner exists for: a lifetime is not a string, and
// misreading one blanks the declaration behind it.
func TestRustLifetimesDoNotHideDeclarations(t *testing.T) {
	fa := extractRust(t, "a.rs", `
pub struct Wrapper<'a> { inner: &'a [u8] }
pub fn borrow<'a>(s: &'a str) -> &'a str { s }
impl<'a> Wrapper<'a> {
    pub fn get(&self) -> &'a [u8] { self.inner }
}
`)
	names := strings.Join(fa.SymbolNames(), ",")
	for _, want := range []string{"Wrapper", "borrow", "Wrapper.get"} {
		if !strings.Contains(names, want) {
			t.Errorf("%q hidden by lifetime syntax; got %q", want, names)
		}
	}
}

func TestRustIgnoresDeclarationsInStringsAndComments(t *testing.T) {
	fa := extractRust(t, "a.rs", `
use real::thing;
const S: &str = "use fake::x; pub fn ghost() {}";
const R: &str = r#"use nowhere::y; pub struct Phantom;"#;
// use commented::z;
/* pub fn block_ghost() {} */
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "real::thing" {
		t.Errorf("imports = %q, want only real::thing", got)
	}
	for _, s := range fa.Symbols {
		switch s.Name {
		case "ghost", "Phantom", "block_ghost":
			t.Errorf("declaration inside a string or comment extracted: %+v", s)
		}
	}
}

func TestRustUseForms(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"simple", "use std::fmt;", "std::fmt"},
		{"deep", "use std::collections::hash_map::Entry;", "std::collections::hash_map::Entry"},
		{"braced", "use std::collections::{HashMap, BTreeMap};", "std::collections::BTreeMap,std::collections::HashMap"},
		{"nested braces", "use crate::a::{b::C, d::{E, F}};", "crate::a::b::C,crate::a::d::E,crate::a::d::F"},
		{"aliased", "use serde::Serialize as Ser;", "serde::Serialize"},
		{"glob", "use std::io::*;", "std::io"},
		{"self in group", "use a::b::{self, c};", "a::b,a::b::c"},
		{"pub use", "pub use crate::inner::Thing;", "crate::inner::Thing"},
		{"crate relative", "use crate::graph::Node;", "crate::graph::Node"},
		{"super relative", "use super::helper;", "super::helper"},
		{"wrapped", "use std::collections::{\n    HashMap,\n    BTreeMap,\n};", "std::collections::BTreeMap,std::collections::HashMap"},
		{"extern crate", "extern crate serde;", "serde"},
	}
	for _, c := range cases {
		fa := extractRust(t, "a.rs", c.src)
		if got := strings.Join(fa.ImportPaths(), ","); got != c.want {
			t.Errorf("%s: imports = %q, want %q", c.name, got, c.want)
		}
	}
}

// crate::, self::, and super:: stay inside the repository; anything else leaves
// it. That distinction is what separates an internal edge from a dependency.
func TestRustExternalVersusInternalUse(t *testing.T) {
	fa := extractRust(t, "a.rs", `
use crate::graph::Node;
use self::local::Thing;
use super::parent::Other;
use serde::Serialize;
use std::fmt;
`)
	external := map[string]bool{}
	for _, im := range fa.Imports {
		external[im.Raw] = im.External
	}
	internal := []string{"crate::graph::Node", "self::local::Thing", "super::parent::Other"}
	for _, p := range internal {
		if external[p] {
			t.Errorf("%s is internal to the crate but marked external", p)
		}
	}
	for _, p := range []string{"serde::Serialize", "std::fmt"} {
		if !external[p] {
			t.Errorf("%s is another crate and should be marked external", p)
		}
	}
}

// `mod x;` points at another file, so it is a dependency edge as well as a
// declaration. `mod x { }` declares the module here, so it is only a symbol.
func TestRustModDeclarationVersusInlineModule(t *testing.T) {
	fa := extractRust(t, "src/lib.rs", "pub mod external;\nmod private_external;\npub mod inline {\n    pub fn f() {}\n}\n")
	paths := strings.Join(fa.ImportPaths(), ",")
	if paths != "self::external,self::private_external" {
		t.Errorf("mod declarations = %q, want both as self:: paths", paths)
	}
	exported := map[string]bool{}
	for _, s := range fa.Symbols {
		exported[s.Name] = s.Exported
	}
	// A pub mod is reachable as crate::x, so it is part of the public surface even
	// when its body is in another file.
	for _, n := range []string{"external", "inline"} {
		if !exported[n] {
			t.Errorf("pub mod %s should be an exported symbol, got %v", n, fa.SymbolNames())
		}
	}
	if _, ok := exported["private_external"]; !ok {
		t.Errorf("a private mod is still a declaration, got %v", fa.SymbolNames())
	}
	if exported["private_external"] {
		t.Error("a mod without pub is not public API")
	}
}

// Methods belong to the type after `for`, not to the trait before it.
func TestRustImplForAttributesToTheType(t *testing.T) {
	fa := extractRust(t, "a.rs", `
pub struct Client;
impl std::fmt::Display for Client {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result { Ok(()) }
}
`)
	for _, s := range fa.Symbols {
		if s.Name == "fmt" && s.Recv != "Client" {
			t.Errorf("fmt attributed to %q, want Client (the implementing type)", s.Recv)
		}
	}
}

func TestRustImplTargets(t *testing.T) {
	cases := []struct{ src, want string }{
		{"impl Client {", "Client"},
		{"impl<T> List<T> {", "List"},
		{"impl Display for Client {", "Client"},
		{"impl<'a> Iterator for Lexer<'a> {", "Lexer"},
		{"impl<T: Clone> Trait for Wrapper<T> where T: Send {", "Wrapper"},
		{"impl crate::graph::Node {", "Node"},
		{"impl Trait for &Client {", "Client"},
	}
	for _, c := range cases {
		if got := rustImplTarget(c.src); got != c.want {
			t.Errorf("rustImplTarget(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}

// Leaving an impl block must be detected, or a later top-level fn becomes a
// method of the last type seen.
func TestRustExitsImplBlock(t *testing.T) {
	fa := extractRust(t, "a.rs", `
pub struct A;
impl A {
    pub fn inside(&self) {}
}

pub fn outside() {}
`)
	for _, s := range fa.Symbols {
		if s.Name == "outside" && s.Recv != "" {
			t.Errorf("a top-level fn after an impl must not be a method, got recv %q", s.Recv)
		}
		if s.Name == "inside" && s.Recv != "A" {
			t.Errorf("inside should be a method of A, got recv %q", s.Recv)
		}
	}
}

// A trait's methods are its declared surface and follow the trait's visibility;
// they never carry pub themselves, because it is not allowed there.
func TestRustTraitMethodsAreSurface(t *testing.T) {
	fa := extractRust(t, "a.rs", `
pub trait Store {
    fn get(&self, k: &str) -> Option<Vec<u8>>;
    fn put(&mut self, k: &str);
}
`)
	count := 0
	for _, s := range fa.Symbols {
		if s.Recv == "Store" {
			count++
			if !s.Exported {
				t.Errorf("trait method %s should be surface", s.Name)
			}
		}
	}
	if count != 2 {
		t.Errorf("expected 2 trait methods, got %d: %v", count, fa.SymbolNames())
	}
}

// pub(crate) is visible in the crate but is not the crate's public API.
func TestRustRestrictedVisibilityIsNotPublic(t *testing.T) {
	fa := extractRust(t, "a.rs", `
pub(crate) fn crate_only() {}
pub(super) fn parent_only() {}
pub(in crate::x) fn scoped() {}
pub fn public() {}
`)
	exported := map[string]bool{}
	for _, s := range fa.Symbols {
		exported[s.Name] = s.Exported
	}
	for _, n := range []string{"crate_only", "parent_only", "scoped"} {
		if exported[n] {
			t.Errorf("%s has restricted visibility and is not public API", n)
		}
	}
	if !exported["public"] {
		t.Error("a plain pub fn is public API")
	}
}

func TestRustModifierStacks(t *testing.T) {
	fa := extractRust(t, "a.rs", `
pub async fn a() {}
pub unsafe fn b() {}
pub const fn c() -> usize { 1 }
pub extern "C" fn d() {}
pub async unsafe fn e() {}
pub unsafe extern "C" fn f() {}
`)
	names := strings.Join(fa.SymbolNames(), ",")
	if names != "a,b,c,d,e,f" {
		t.Errorf("SymbolNames() = %q, want a..f — a modifier stack must not hide the fn", names)
	}
	for _, s := range fa.Symbols {
		if s.Kind != SymFunc {
			t.Errorf("%s kind = %q, want func", s.Name, s.Kind)
		}
		if !s.Exported {
			t.Errorf("%s should be exported", s.Name)
		}
	}
}

// `const X: u8` is an item; `const fn` is a modifier on a function.
func TestRustConstItemVersusConstFn(t *testing.T) {
	fa := extractRust(t, "a.rs", "pub const MAX: usize = 1;\npub const fn f() -> usize { 1 }\nstatic mut COUNTER: usize = 0;\n")
	kinds := map[string]SymbolKind{}
	for _, s := range fa.Symbols {
		kinds[s.Name] = s.Kind
	}
	if kinds["MAX"] != SymConst {
		t.Errorf("MAX kind = %q, want const", kinds["MAX"])
	}
	if kinds["f"] != SymFunc {
		t.Errorf("f kind = %q, want func — const fn is a function", kinds["f"])
	}
	if kinds["COUNTER"] != SymConst {
		t.Errorf("COUNTER kind = %q, want const", kinds["COUNTER"])
	}
}

func TestRustItemKinds(t *testing.T) {
	fa := extractRust(t, "a.rs", `
pub struct S;
pub enum E { A }
pub union U { a: u8 }
pub trait T {}
pub type Alias = u8;
pub fn f() {}
`)
	kinds := map[string]SymbolKind{}
	for _, s := range fa.Symbols {
		kinds[s.Name] = s.Kind
	}
	want := map[string]SymbolKind{
		"S": SymType, "E": SymType, "U": SymType, "T": SymInterface,
		"Alias": SymType, "f": SymFunc,
	}
	for n, w := range want {
		if kinds[n] != w {
			t.Errorf("%s kind = %q, want %q", n, kinds[n], w)
		}
	}
}

// A nested fn or struct is unreachable from outside its enclosing function.
func TestRustSkipsNestedItems(t *testing.T) {
	fa := extractRust(t, "a.rs", `
pub fn outer() {
    fn nested() {}
    struct Local;
    const INNER: u8 = 1;
}
pub fn after() {}
`)
	names := strings.Join(fa.SymbolNames(), ",")
	if names != "after,outer" {
		t.Errorf("SymbolNames() = %q, want after,outer", names)
	}
}

func TestRustDocComments(t *testing.T) {
	fa := extractRust(t, "a.rs", `
/// Does the thing. And more.
pub fn documented() {}

/// Multi-line doc
/// continuing here.
pub fn multiline() {}

/// Documented through an attribute.
#[derive(Debug)]
pub struct WithAttribute;

// Not a doc comment.
pub fn plain() {}

pub fn undocumented() {}
`)
	docs := map[string]string{}
	for _, s := range fa.Symbols {
		docs[s.Name] = s.Doc
	}
	want := map[string]string{
		"documented":    "Does the thing.",
		"multiline":     "Multi-line doc continuing here.",
		"WithAttribute": "Documented through an attribute.",
		"plain":         "",
		"undocumented":  "",
	}
	for n, w := range want {
		if docs[n] != w {
			t.Errorf("%s doc = %q, want %q", n, docs[n], w)
		}
	}
}

func TestRustModulePath(t *testing.T) {
	cases := []struct{ path, want string }{
		{"src/lib.rs", ""},
		{"src/main.rs", ""},
		{"src/graph.rs", "graph"},
		{"src/graph/mod.rs", "graph"},
		{"src/graph/node.rs", "graph::node"},
		{"src/bin/tool.rs", ""},
		{`src\graph\node.rs`, "graph::node"},
	}
	for _, c := range cases {
		if got := rustModulePath(c.path); got != c.want {
			t.Errorf("rustModulePath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// `fn main` is an entrypoint only in a crate root.
func TestRustEntrypoint(t *testing.T) {
	cases := []struct {
		path, src string
		want      bool
	}{
		{"src/main.rs", "fn main() {}", true},
		{"src/bin/tool.rs", "fn main() {}", true},
		{"examples/demo.rs", "fn main() {}", true},
		{"src/lib.rs", "fn main() {}", false},
		{"src/helper.rs", "fn main() {}", false},
		{"src/main.rs", "impl A {\n    fn main(&self) {}\n}", false},
	}
	for _, c := range cases {
		fa := extractRust(t, c.path, c.src)
		got := len(fa.Entrypoints) > 0
		if got != c.want {
			t.Errorf("%s: entrypoint = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestRustEmptyAndCommentOnlyFiles(t *testing.T) {
	for _, src := range []string{"", "\n\n", "// nothing\n", "//! Crate doc only.\n"} {
		fa, err := RustExtractor{}.Extract(rsFile("src/lib.rs", src))
		if err != nil {
			t.Errorf("Extract(%q) errored: %v", src, err)
		}
		if len(fa.Symbols) != 0 || len(fa.Imports) != 0 {
			t.Errorf("Extract(%q) found facts where there are none: %+v", src, fa)
		}
	}
}

func TestRustIsDeterministic(t *testing.T) {
	src := `
use std::collections::{HashMap, BTreeMap};
pub struct Z;
pub struct A;
impl A {
    pub fn z(&self) {}
    pub fn a(&self) {}
}
pub const X: u8 = 1;
`
	want := extractRust(t, "src/lib.rs", src)
	for i := 0; i < 20; i++ {
		got := extractRust(t, "src/lib.rs", src)
		if strings.Join(got.SymbolNames(), ",") != strings.Join(want.SymbolNames(), ",") {
			t.Fatalf("run %d: symbols differ", i)
		}
		if strings.Join(got.ImportPaths(), ",") != strings.Join(want.ImportPaths(), ",") {
			t.Fatalf("run %d: imports differ", i)
		}
	}
}
