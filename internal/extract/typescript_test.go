package extract

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func tsFile(path, src string) discover.File {
	return discover.File{
		Path: path, Lang: discover.LangTS, Class: discover.ClassSource, Content: src,
	}
}

func jsFile(path, src string) discover.File {
	return discover.File{
		Path: path, Lang: discover.LangJS, Class: discover.ClassSource, Content: src,
	}
}

func extractTS(t *testing.T, path, src string) Facts {
	t.Helper()
	fa, err := TSExtractor{}.Extract(tsFile(path, src))
	if err != nil {
		t.Fatalf("Extract(%s): %v", path, err)
	}
	fa.Normalize()
	return fa
}

// The scored corpus. Hand-labeled against real TypeScript and JavaScript,
// including the CommonJS and barrel-file forms that a naive ES-module-only
// matcher reports nothing for.
func tsCorpus() []Fixture {
	return []Fixture{
		{
			File: tsFile("src/service.ts", `
import fs from "node:fs";
import * as path from "node:path";
import { Client, type Config } from "./client";
import defaultExport, { helper } from "../shared/util";
import "./polyfill";
import type { Logger } from "./logging";

/**
 * Runs the service.
 */
export async function run(cfg: Config): Promise<void> {
  const p = path.join("a", "b");
  await fs.promises.readFile(p);
}

export const DEFAULT_PORT = 8080;

export class Service {
  private client: Client;
  start() {}
}

export interface Options {
  port: number;
}

export type Handler = (req: unknown) => void;

export enum Mode {
  Fast,
  Slow,
}

function internal() {
  return helper(defaultExport);
}

const unexported = 1;
`),
			Expected: Expected{
				Imports: []string{
					"../shared/util", "./client", "./logging", "./polyfill",
					"node:fs", "node:path",
				},
				Symbols: []string{
					"DEFAULT_PORT", "Handler", "Mode", "Options", "Service",
					"internal", "run", "unexported",
				},
				Exported: []string{
					"DEFAULT_PORT", "Handler", "Mode", "Options", "Service", "run",
				},
				Entrypoints: []string{},
			},
		},
		{
			// A barrel file: nothing but re-exports. This is how most TypeScript
			// packages declare their public surface, so missing the forms here
			// disconnects the graph at the package boundary.
			File: tsFile("src/index.ts", `
export { Client } from "./client";
export { Service, type Options } from "./service";
export * from "./errors";
export * as helpers from "./helpers";
export type { Logger } from "./logging";
export { default as Engine } from "./engine";
`),
			Expected: Expected{
				Imports: []string{
					"./client", "./engine", "./errors", "./helpers", "./logging", "./service",
				},
				Symbols:     []string{},
				Exported:    []string{},
				Entrypoints: []string{},
			},
		},
		{
			// CommonJS, still ubiquitous in Node config and tooling.
			File: jsFile("build/config.js", `
const path = require("path");
const { readFile, writeFile } = require("fs/promises");
require("dotenv/config");
const plugin = require("./plugins/local");

const OUT_DIR = "dist";

function configure(options) {
  const lazy = require("./lazy-only-when-needed");
  return { ...options, lazy };
}

module.exports = { configure, OUT_DIR };
`),
			Expected: Expected{
				Imports: []string{
					"./lazy-only-when-needed", "./plugins/local", "dotenv/config",
					"fs/promises", "path",
				},
				Symbols:     []string{"OUT_DIR", "configure", "path", "plugin"},
				Exported:    []string{},
				Entrypoints: []string{},
			},
		},
		{
			// Modern function-valued declarations. Reported as vars, most
			// TypeScript would appear to contain no functions at all.
			File: tsFile("src/handlers.ts", `
import { z } from "zod";

export const handleGet = async (req: Request): Promise<Response> => {
  return new Response();
};

export const handlePost = function (req: Request) {
  return new Response();
};

const schema = z.object({
  name: z.string(),
  nested: { fn: () => true },
});

export default function handleDefault() {}

export function* generate() {
  yield 1;
}

let mutable = 0;
`),
			Expected: Expected{
				Imports: []string{"zod"},
				Symbols: []string{
					"generate", "handleDefault", "handleGet", "handlePost", "mutable", "schema",
				},
				Exported:    []string{"generate", "handleDefault", "handleGet", "handlePost"},
				Entrypoints: []string{},
			},
		},
		{
			// The adversarial fixture: everything that looks like a declaration or
			// an import but is not.
			File: tsFile("src/tricky.ts", `
import real from "./real";

const template = `+"`"+`
import fake from "fake";
export function ghost() {}
export class Phantom {}
`+"`"+`;

// import commented from "commented";
/* export function blockGhost() {} */

/**
 * Documented.
 */
export function documented() {
  function closure() {}
  class LocalClass {}
  const nested = 1;
  return { closure, LocalClass, nested };
}

const url = "https://example.com//not-a-comment";
const quote = "it's fine";
const escaped = "say \"import fake\"";

const dynamic = require(variableName);

export { documented as publicName };
`),
			Expected: Expected{
				Imports: []string{"./real"},
				Symbols: []string{
					"documented", "dynamic", "escaped", "publicName", "quote",
					"template", "url",
				},
				Exported:    []string{"documented", "publicName"},
				Entrypoints: []string{},
			},
		},
	}
}

// The measurement design §4.2 promises for TypeScript and JavaScript.
func TestTSExtractorMeetsTarget(t *testing.T) {
	ls := ScoreExtractor(TSExtractor{}, discover.LangTS, tsCorpus())
	if !ls.MeetsTarget() {
		t.Errorf("TS/JS extractor below target:\n%s", ls.Report())
	}
	t.Logf("TS/JS extractor score:\n%s", ls.Report())
}

// The case the scanner exists for, called out so a regression is unmistakable.
func TestTSIgnoresDeclarationsInsideTemplateLiterals(t *testing.T) {
	fa := extractTS(t, "a.ts", "import real from \"./real\";\nconst q = `\nimport fake from \"fake\";\nexport class Phantom {}\n`;\n")
	if got := strings.Join(fa.ImportPaths(), ","); got != "./real" {
		t.Errorf("imports = %q, want only ./real", got)
	}
	for _, s := range fa.Symbols {
		if s.Name == "Phantom" {
			t.Error("a class inside a template literal was extracted")
		}
	}
}

func TestTSImportForms(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"default", `import x from "m";`, "m"},
		{"namespace", `import * as ns from "m";`, "m"},
		{"named", `import { a, b } from "m";`, "m"},
		{"named renamed", `import { a as b } from "m";`, "m"},
		{"both", `import x, { a } from "m";`, "m"},
		{"side effect", `import "m";`, "m"},
		{"type only", `import type { T } from "m";`, "m"},
		{"inline type", `import { type T, v } from "m";`, "m"},
		{"single quotes", `import x from 'm';`, "m"},
		{"relative", `import x from "../a/b";`, "../a/b"},
		{"scoped package", `import x from "@scope/pkg";`, "@scope/pkg"},
		{"subpath", `import x from "pkg/sub/path.js";`, "pkg/sub/path.js"},
		{"no semicolon", `import x from "m"`, "m"},
		{"wrapped", "import {\n  a,\n  b,\n} from \"m\";", "m"},
		// Not imports.
		{"identifier prefix", `importantThing();`, ""},
	}
	for _, c := range cases {
		fa := extractTS(t, "a.ts", c.src)
		if got := strings.Join(fa.ImportPaths(), ","); got != c.want {
			t.Errorf("%s: imports = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTSImportBindings(t *testing.T) {
	fa := extractTS(t, "a.ts", `import def, { a, b as c } from "m";`)
	if len(fa.Imports) != 1 {
		t.Fatalf("expected 1 import, got %+v", fa.Imports)
	}
	im := fa.Imports[0]
	if got := strings.Join(im.Names, ","); got != "a,b" {
		t.Errorf("Names = %q, want the source-side names a,b", got)
	}
}

// A namespace import binds the whole module under one local name, which is how
// the code refers to it.
func TestTSNamespaceImportAlias(t *testing.T) {
	fa := extractTS(t, "a.ts", `import * as path from "node:path";`)
	if fa.Imports[0].Alias != "path" {
		t.Errorf("Alias = %q, want path", fa.Imports[0].Alias)
	}
}

// A re-export is a dependency. A barrel file made only of them must not look
// like a file with no dependencies.
func TestTSReExportsAreImports(t *testing.T) {
	fa := extractTS(t, "index.ts", `
export { A } from "./a";
export * from "./b";
export * as c from "./c";
export type { D } from "./d";
export { default as E } from "./e";
`)
	got := strings.Join(fa.ImportPaths(), ",")
	if got != "./a,./b,./c,./d,./e" {
		t.Errorf("re-export paths = %q, want all five", got)
	}
}

// `export const x = "y"` contains a string but no module path, and must not be
// read as a re-export.
func TestTSExportedConstWithStringIsNotAnImport(t *testing.T) {
	fa := extractTS(t, "a.ts", `export const region = "us-east-1";`)
	if len(fa.Imports) != 0 {
		t.Errorf("an exported string constant is not an import: %+v", fa.Imports)
	}
	if len(fa.Symbols) != 1 || fa.Symbols[0].Name != "region" {
		t.Errorf("expected the const to be a symbol, got %v", fa.SymbolNames())
	}
}

// An export list names declarations found elsewhere in the same file; the two
// records are one symbol.
func TestTSExportListMergesWithDeclaration(t *testing.T) {
	fa := extractTS(t, "a.ts", `
function helper() {}
class Thing {}
export { helper, Thing };
`)
	if len(fa.Symbols) != 2 {
		t.Fatalf("expected 2 merged symbols, got %d: %+v", len(fa.Symbols), fa.Symbols)
	}
	for _, s := range fa.Symbols {
		if !s.Exported {
			t.Errorf("%s should be exported by the export list", s.Name)
		}
		if s.Kind == "" {
			t.Errorf("%s lost its kind in the merge", s.Name)
		}
	}
}

// An export rename exposes the alias, which is the name a caller imports.
func TestTSExportRenameUsesAlias(t *testing.T) {
	fa := extractTS(t, "a.ts", "function internalName() {}\nexport { internalName as publicName };\n")
	names := strings.Join(fa.SymbolNames(), ",")
	if !strings.Contains(names, "publicName") {
		t.Errorf("the exported alias must appear as a symbol, got %q", names)
	}
	for _, s := range fa.Symbols {
		if s.Name == "internalName" && s.Exported {
			t.Error("the internal name is not what callers import, so it is not exported")
		}
	}
}

func TestTSDeclarationKinds(t *testing.T) {
	fa := extractTS(t, "a.ts", `
export function fn() {}
export function* gen() {}
export const arrow = () => {};
export const asyncArrow = async () => {};
export const fnExpr = function () {};
export const value = 42;
export let mutable = 1;
export var old = 2;
export class C {}
export abstract class AC {}
export interface I {}
export type T = string;
export enum E { A }
export namespace NS {}
declare module "m" {}
`)
	kinds := map[string]SymbolKind{}
	for _, s := range fa.Symbols {
		kinds[s.Name] = s.Kind
	}
	want := map[string]SymbolKind{
		"fn": SymFunc, "gen": SymFunc, "arrow": SymFunc, "asyncArrow": SymFunc,
		"fnExpr": SymFunc, "value": SymConst, "mutable": SymVar, "old": SymVar,
		"C": SymClass, "AC": SymClass, "I": SymInterface, "T": SymType,
		"E": SymType, "NS": SymType, "m": SymType,
	}
	for name, w := range want {
		if kinds[name] != w {
			t.Errorf("%s kind = %q, want %q", name, kinds[name], w)
		}
	}
}

// An arrow function inside an object initialiser does not make the constant a
// function.
func TestTSArrowInsideObjectIsNotAFunctionDeclaration(t *testing.T) {
	fa := extractTS(t, "a.ts", "export const config = {\n  onReady: () => {},\n};\n")
	for _, s := range fa.Symbols {
		if s.Name == "config" && s.Kind != SymConst {
			t.Errorf("config kind = %q, want const — the arrow is nested", s.Kind)
		}
	}
}

// Only module-level declarations are surface; brace depth decides, because
// JavaScript indentation carries no meaning and a reformat must not change the
// extracted set.
func TestTSSkipsNestedDeclarations(t *testing.T) {
	fa := extractTS(t, "a.ts", `
export function outer() {
  function inner() {}
  class Local {}
  const nested = 1;
  interface Shape {}
}
export const after = 1;
`)
	for _, s := range fa.Symbols {
		switch s.Name {
		case "inner", "Local", "nested", "Shape":
			t.Errorf("nested declaration %q must not be module surface", s.Name)
		}
	}
	names := strings.Join(fa.SymbolNames(), ",")
	if names != "after,outer" {
		t.Errorf("SymbolNames() = %q, want after,outer", names)
	}
}

// The same source reindented must yield the same symbols, which is the property
// brace-depth tracking exists to guarantee.
func TestTSExtractionIsIndentationIndependent(t *testing.T) {
	compact := "export function f() {\nfunction g() {}\n}\nexport const h = 1;\n"
	indented := "export function f() {\n        function g() {}\n}\n        export const h = 1;\n"
	a := extractTS(t, "a.ts", compact)
	b := extractTS(t, "a.ts", indented)
	if strings.Join(a.SymbolNames(), ",") != strings.Join(b.SymbolNames(), ",") {
		t.Errorf("reindenting changed the extracted symbols: %v vs %v",
			a.SymbolNames(), b.SymbolNames())
	}
}

func TestTSRequireForms(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"assigned", `const x = require("m");`, "m"},
		{"destructured", `const { a, b } = require("m");`, "m"},
		{"bare", `require("m");`, "m"},
		{"dynamic import", `const m = await import("m");`, "m"},
		{"nested lazy", "function f() {\n  const m = require(\"lazy\");\n}", "lazy"},
		// A computed path is not a knowable module. An interpolated template is
		// equally computed, and recording "./${name}" as a module path would put an
		// edge in the graph pointing at a file that does not exist.
		{"computed", `const x = require(name);`, ""},
		{"interpolated template", "const x = require(`./${name}`);", ""},
		{"plain template", "const x = require(`./static`);", "./static"},
		// A dynamic import can open a statement, in which case the line looks like
		// an import statement to the dispatcher. The statement branch claims it and
		// tsImport correctly rejects it as an expression, so the dependency is lost
		// unless the branch also tries the expression form.
		{"leading dynamic import", `import("m").then((x) => x.default);`, "m"},
		{"leading dynamic no chain", `import("m");`, "m"},
		{"leading await import", `await import("m");`, "m"},
		{"destructured default", `const { default: P } = await import("m");`, "m"},
		// Not require.
		{"identifier suffix", `const x = myRequire("m");`, ""},
	}
	for _, c := range cases {
		fa := extractTS(t, "a.js", c.src)
		if got := strings.Join(fa.ImportPaths(), ","); got != c.want {
			t.Errorf("%s: imports = %q, want %q", c.name, got, c.want)
		}
	}
}

// An ambient module names the package it types with a string literal rather than
// an identifier. A .d.ts whose whole purpose is to declare types for an untyped
// package consists of nothing else, so dropping the form reports an empty surface
// for precisely the file that exists to declare one.
func TestTSAmbientModuleDeclaration(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"single quotes", "declare module 'ext' {\n}\n", "ext"},
		{"double quotes", `declare module "ext" {}`, "ext"},
		{"exported", "export declare module 'ext' {\n}\n", "ext"},
		{"scoped package", `declare module "@scope/pkg" {}`, "@scope/pkg"},
		{"wildcard path", `declare module "*.svg" {}`, "*.svg"},
		{"identifier form still works", "declare module Foo {\n}\n", "Foo"},
		// `declare global` augments the global scope and names no module.
		{"global augmentation", "declare global {\n  interface Window {}\n}\n", ""},
	}
	for _, c := range cases {
		fa := extractTS(t, "a.d.ts", c.src)
		if got := strings.Join(fa.SymbolNames(), ","); got != c.want {
			t.Errorf("%s: symbols = %q, want %q", c.name, got, c.want)
		}
		if len(fa.Imports) != 0 {
			t.Errorf("%s: an ambient declaration declares, it does not import: %+v",
				c.name, fa.Imports)
		}
	}
}

// A quoted name makes the declaration an ambient external module, which types a
// whole package for every file in the program without anything importing it and
// without an `export` keyword being available to it. So it is surface either way.
// A bare identifier is a namespace and follows the ordinary rule, which is the
// distinction that keeps `Exported` from being unconditionally true.
func TestTSAmbientModuleIsSurfaceWithoutExport(t *testing.T) {
	cases := []struct {
		src      string
		name     string
		exported bool
	}{
		{`declare module "ext" {}`, "ext", true},
		{`export declare module "ext" {}`, "ext", true},
		{`declare module Foo {}`, "Foo", false},
		{`export declare module Foo {}`, "Foo", true},
		{`declare namespace Bar {}`, "Bar", false},
		{`export declare namespace Bar {}`, "Bar", true},
	}
	for _, c := range cases {
		fa := extractTS(t, "a.d.ts", c.src)
		if len(fa.Symbols) != 1 {
			t.Fatalf("%s: want exactly one symbol, got %+v", c.src, fa.Symbols)
		}
		s := fa.Symbols[0]
		if s.Name != c.name {
			t.Errorf("%s: name = %q, want %q", c.src, s.Name, c.name)
		}
		if s.Exported != c.exported {
			t.Errorf("%s: exported = %v, want %v", c.src, s.Exported, c.exported)
		}
	}
}

func TestTSRequireBindings(t *testing.T) {
	fa := extractTS(t, "a.js", `const { readFile, writeFile } = require("fs/promises");`)
	if len(fa.Imports) != 1 {
		t.Fatalf("expected 1 import, got %+v", fa.Imports)
	}
	if got := strings.Join(fa.Imports[0].Names, ","); got != "readFile,writeFile" {
		t.Errorf("Names = %q, want the destructured names", got)
	}

	fa = extractTS(t, "b.js", `const path = require("path");`)
	if fa.Imports[0].Alias != "path" {
		t.Errorf("Alias = %q, want path", fa.Imports[0].Alias)
	}
}

func TestTSDefaultExport(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"named function", "export default function handler() {}", "handler"},
		{"named class", "export default class Widget {}", "Widget"},
		{"anonymous function", "export default function () {}", "default"},
		{"expression", "export default { a: 1 };", "default"},
		{"identifier", "export default handler;", "default"},
	}
	for _, c := range cases {
		fa := extractTS(t, "a.ts", c.src)
		found := false
		for _, s := range fa.Symbols {
			if s.Name == c.want && s.Exported {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: expected exported symbol %q, got %v", c.name, c.want, fa.SymbolNames())
		}
	}
}

func TestTSJSDoc(t *testing.T) {
	fa := extractTS(t, "a.ts", `
/**
 * Does the thing. And more.
 */
export function documented() {}

/**
 * Summary here.
 * @param x nothing
 */
export function tagged() {}

/* not a doc comment */
export function plain() {}

// line comment
export function lineCommented() {}

export function undocumented() {}
`)
	docs := map[string]string{}
	for _, s := range fa.Symbols {
		docs[s.Name] = s.Doc
	}
	want := map[string]string{
		"documented":    "Does the thing.",
		"tagged":        "Summary here.",
		"plain":         "",
		"lineCommented": "",
		"undocumented":  "",
	}
	for name, w := range want {
		if docs[name] != w {
			t.Errorf("%s doc = %q, want %q", name, docs[name], w)
		}
	}
}

// A shebang is the one entrypoint signal available inside a JS source file.
func TestTSShebangEntrypoint(t *testing.T) {
	fa := extractTS(t, "cli.js", "#!/usr/bin/env node\nconsole.log(1);\n")
	if len(fa.Entrypoints) != 1 || fa.Entrypoints[0] != "#!" {
		t.Errorf("expected a shebang entrypoint, got %v", fa.Entrypoints)
	}
	// A non-node shebang is somebody else's interpreter.
	fa = extractTS(t, "x.js", "#!/bin/sh\nexit 0\n")
	if len(fa.Entrypoints) != 0 {
		t.Errorf("a non-node shebang is not a JS entrypoint, got %v", fa.Entrypoints)
	}
}

func TestTSHandlesBothLanguages(t *testing.T) {
	langs := TSExtractor{}.Langs()
	if len(langs) != 2 {
		t.Fatalf("expected 2 languages, got %v", langs)
	}
	fa, err := TSExtractor{}.Extract(jsFile("a.js", `import x from "m";`))
	if err != nil {
		t.Fatal(err)
	}
	if fa.Lang != discover.LangJS {
		t.Errorf("Lang = %q, should follow the input file", fa.Lang)
	}
}

func TestTSEmptyAndCommentOnlyFiles(t *testing.T) {
	for _, src := range []string{"", "\n\n", "// nothing\n", "/* nothing */\n"} {
		fa, err := TSExtractor{}.Extract(tsFile("a.ts", src))
		if err != nil {
			t.Errorf("Extract(%q) errored: %v", src, err)
		}
		if len(fa.Symbols) != 0 || len(fa.Imports) != 0 {
			t.Errorf("Extract(%q) found facts where there are none: %+v", src, fa)
		}
	}
}

func TestTSIsDeterministic(t *testing.T) {
	src := `
import { z, a } from "m";
export class C {}
export function f() {}
const x = require("n");
export { f as g };
`
	want := extractTS(t, "a.ts", src)
	for i := 0; i < 20; i++ {
		got := extractTS(t, "a.ts", src)
		if strings.Join(got.SymbolNames(), ",") != strings.Join(want.SymbolNames(), ",") {
			t.Fatalf("run %d: symbols differ", i)
		}
		if strings.Join(got.ImportPaths(), ",") != strings.Join(want.ImportPaths(), ",") {
			t.Fatalf("run %d: imports differ", i)
		}
	}
}

func TestMergeSymbolsUnionsKnowledge(t *testing.T) {
	fa := Facts{Symbols: []Symbol{
		{Name: "f", Kind: SymFunc, Doc: "Does f.", Line: 3},
		{Name: "f", Exported: true, Line: 20},
		{Name: "g", Kind: SymClass, Line: 5},
	}}
	fa.Normalize()
	if len(fa.Symbols) != 2 {
		t.Fatalf("expected f to merge, got %+v", fa.Symbols)
	}
	f := fa.Symbols[0]
	if f.Kind != SymFunc {
		t.Errorf("merged Kind = %q, want func", f.Kind)
	}
	if !f.Exported {
		t.Error("exportedness must survive the merge")
	}
	if f.Doc != "Does f." {
		t.Errorf("Doc = %q, should be kept", f.Doc)
	}
	if f.Line != 3 {
		t.Errorf("Line = %d, want the declaration's line not the export's", f.Line)
	}
}

// A method and a function of the same name are different declarations.
func TestMergeSymbolsKeepsDistinctReceiversApart(t *testing.T) {
	fa := Facts{Symbols: []Symbol{
		{Name: "get", Kind: SymMethod, Recv: "A"},
		{Name: "get", Kind: SymMethod, Recv: "B"},
		{Name: "get", Kind: SymFunc},
	}}
	fa.Normalize()
	if len(fa.Symbols) != 3 {
		t.Errorf("distinct receivers must not merge, got %+v", fa.Symbols)
	}
}
