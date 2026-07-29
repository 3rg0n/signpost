package extract

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func pyFile(path, src string) discover.File {
	return discover.File{
		Path: path, Lang: discover.LangPython, Class: discover.ClassSource, Content: src,
	}
}

func extractPy(t *testing.T, path, src string) Facts {
	t.Helper()
	fa, err := PythonExtractor{}.Extract(pyFile(path, src))
	if err != nil {
		t.Fatalf("Extract(%s): %v", path, err)
	}
	fa.Normalize()
	return fa
}

// The scored corpus. Hand-labeled, and the labels are the contract: design §4.2
// requires the number to be measured against real Python, including the forms
// that a naive line matcher gets wrong.
func pyCorpus() []Fixture {
	return []Fixture{
		{
			File: pyFile("src/app/service.py", `
"""Service layer.

This docstring mentions "import fake" and "def ghost" which must not be
extracted, because a docstring is data.
"""
import os
import sys as system
from typing import (
    Any,
    Dict,
)
from .models import User, Group
from ..shared import helper as h

DEFAULT_TIMEOUT = 30
_private_cache: Dict[str, Any] = {}


def handle(request):
    """Handle a request."""
    return None


def _internal():
    # import fake
    def closure():
        pass
    return closure


class Service:
    """A service."""

    def start(self):
        pass

    def _stop(self):
        pass


if __name__ == "__main__":
    handle(None)
`),
			Expected: Expected{
				Package: "app.service",
				Imports: []string{".models", "..shared", "os", "sys", "typing"},
				Symbols: []string{
					"DEFAULT_TIMEOUT", "Service", "Service._stop", "Service.start",
					"_internal", "_private_cache", "handle",
				},
				Exported:    []string{"DEFAULT_TIMEOUT", "Service", "Service.start", "handle"},
				Entrypoints: []string{"__main__"},
			},
		},
		{
			// __all__ overrides the underscore convention in both directions.
			File: pyFile("pkg/__init__.py", `
from ._impl import Engine, _Registry

__all__ = [
    "Engine",
    "_Registry",
]


def helper():
    """Public by name, but not in __all__."""
    return 1
`),
			Expected: Expected{
				Package: "pkg",
				Imports: []string{"._impl"},
				// Engine and _Registry are re-exports: imported here, named in
				// __all__, and therefore part of this package's surface even
				// though neither is declared in this file. __all__ itself is a
				// dunder, not surface.
				Symbols:     []string{"Engine", "_Registry", "helper"},
				Exported:    []string{"Engine", "_Registry"},
				Entrypoints: []string{},
			},
		},
		{
			File: pyFile("async_mod.py", `
import asyncio
from contextlib import asynccontextmanager


async def fetch(url: str) -> bytes:
    """Fetch bytes."""
    return b""


class Client:
    async def get(self, path):
        return await fetch(path)

    @property
    def base(self):
        return ""
`),
			Expected: Expected{
				Package:     "async_mod",
				Imports:     []string{"asyncio", "contextlib"},
				Symbols:     []string{"Client", "Client.base", "Client.get", "fetch"},
				Exported:    []string{"Client", "Client.base", "Client.get", "fetch"},
				Entrypoints: []string{},
			},
		},
		{
			// Star imports, lazy imports inside functions, and multi-module import
			// statements. A lazy import is a real dependency and must be found.
			File: pyFile("lazy.py", `
import a.b.c
import x, y.z as yz
from legacy import *


def load():
    import heavy.module
    from json import loads
    return heavy.module, loads
`),
			Expected: Expected{
				Package:     "lazy",
				Imports:     []string{"a.b.c", "heavy.module", "json", "legacy", "x", "y.z"},
				Symbols:     []string{"load"},
				Exported:    []string{"load"},
				Entrypoints: []string{},
			},
		},
		{
			// The adversarial fixture: everything that looks like a declaration but
			// is not. A line matcher without a scanner fails this outright.
			File: pyFile("tricky.py", `
import real

CODE = """
import fake
def ghost():
    pass
class Phantom:
    pass
"""

OTHER = '''
from nowhere import nothing
'''

# import commented_out
# def commented_def(): pass

url = "http://example.com#not-a-comment"
message = "it's a quote"
pattern = "def \"quoted\" ghost"

x = 1
x += 1
if x == 1:
    pass

a, b = 1, 2
obj.attr = 3
`),
			Expected: Expected{
				Package:     "tricky",
				Imports:     []string{"real"},
				Symbols:     []string{"CODE", "OTHER", "message", "pattern", "url", "x"},
				Exported:    []string{"CODE", "OTHER", "message", "pattern", "url", "x"},
				Entrypoints: []string{},
			},
		},
	}
}

// The measurement design §4.2 promises for Python.
func TestPythonExtractorMeetsTarget(t *testing.T) {
	ls := ScoreExtractor(PythonExtractor{}, discover.LangPython, pyCorpus())
	if !ls.MeetsTarget() {
		t.Errorf("Python extractor below target:\n%s", ls.Report())
	}
	t.Logf("Python extractor score:\n%s", ls.Report())
}

// The case the whole scanner exists for. Called out separately from the corpus so
// a regression here is unmistakable rather than a fractional F1 drop.
func TestPythonIgnoresDeclarationsInsideStrings(t *testing.T) {
	fa := extractPy(t, "a.py", `
import real
SQL = """
import fake
def ghost(): pass
class Phantom: pass
"""
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "real" {
		t.Errorf("imports = %q, want only real", got)
	}
	for _, s := range fa.Symbols {
		if s.Name == "ghost" || s.Name == "Phantom" {
			t.Errorf("declaration inside a string was extracted: %+v", s)
		}
	}
}

func TestPythonModulePath(t *testing.T) {
	cases := []struct{ path, want string }{
		{"mod.py", "mod"},
		{"pkg/mod.py", "pkg.mod"},
		{"src/pkg/mod.py", "pkg.mod"},
		{"lib/pkg/mod.py", "pkg.mod"},
		{"pkg/__init__.py", "pkg"},
		{"src/pkg/sub/__init__.py", "pkg.sub"},
		{"__init__.py", ""},
		// A backslash path from a Windows walk must yield the same module path.
		{`pkg\mod.py`, "pkg.mod"},
	}
	for _, c := range cases {
		if got := pyModulePath(c.path); got != c.want {
			t.Errorf("pyModulePath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// A closure is unreachable from outside the function, so it is not module surface.
func TestPythonSkipsClosures(t *testing.T) {
	fa := extractPy(t, "a.py", `
def outer():
    def inner():
        pass
    class LocalClass:
        pass
    return inner
`)
	for _, s := range fa.Symbols {
		if s.Name == "inner" || s.Name == "LocalClass" {
			t.Errorf("a nested declaration must not be module surface: %+v", s)
		}
	}
	if len(fa.Symbols) != 1 || fa.Symbols[0].Name != "outer" {
		t.Errorf("expected only outer, got %v", fa.SymbolNames())
	}
}

func TestPythonMethodsAttributedToClass(t *testing.T) {
	fa := extractPy(t, "a.py", `
class A:
    def m(self):
        pass

class B:
    def m(self):
        pass

def m():
    pass
`)
	got := strings.Join(fa.SymbolNames(), ",")
	if got != "A,A.m,B,B.m,m" {
		t.Errorf("SymbolNames() = %q, want methods attributed to their own class", got)
	}
}

// Leaving a class body has to be detected by indentation, or every subsequent
// top-level def becomes a method of the last class seen.
func TestPythonExitsClassBody(t *testing.T) {
	fa := extractPy(t, "a.py", `
class A:
    def inside(self):
        pass

def outside():
    pass
`)
	for _, s := range fa.Symbols {
		if s.Name == "outside" && s.Recv != "" {
			t.Errorf("a top-level def after a class must not be a method, got recv %q", s.Recv)
		}
	}
}

// A method on a private class is not public surface: the class cannot be reached
// from outside the module, so neither can its methods.
func TestPythonMethodOnPrivateClassIsNotExported(t *testing.T) {
	fa := extractPy(t, "a.py", `
class _Hidden:
    def Public(self):
        pass

class Shown:
    def Public(self):
        pass
`)
	for _, s := range fa.Symbols {
		if s.Recv == "_Hidden" && s.Exported {
			t.Error("a method on a private class must not be exported")
		}
		if s.Recv == "Shown" && !s.Exported {
			t.Error("a method on a public class should be exported")
		}
	}
}

func TestPythonAllOverridesUnderscoreConvention(t *testing.T) {
	fa := extractPy(t, "a.py", `
__all__ = ["_exported", "AlsoPublic"]

def _exported(): pass
def AlsoPublic(): pass
def not_listed(): pass
`)
	exported := map[string]bool{}
	for _, s := range fa.Symbols {
		exported[s.Name] = s.Exported
	}
	if !exported["_exported"] {
		t.Error("__all__ should export a name the underscore convention would hide")
	}
	if !exported["AlsoPublic"] {
		t.Error("a name in __all__ should be exported")
	}
	if exported["not_listed"] {
		t.Error("__all__ is authoritative: a public-looking name not listed is not exported")
	}
}

// Without __all__ the underscore convention applies unchanged.
func TestPythonWithoutAllUsesConvention(t *testing.T) {
	fa := extractPy(t, "a.py", "def pub(): pass\ndef _priv(): pass\n")
	exported := map[string]bool{}
	for _, s := range fa.Symbols {
		exported[s.Name] = s.Exported
	}
	if !exported["pub"] || exported["_priv"] {
		t.Errorf("underscore convention not applied: %v", exported)
	}
}

func TestPythonImportForms(t *testing.T) {
	cases := []struct {
		name, src, wantPaths string
	}{
		{"plain", "import os", "os"},
		{"dotted", "import a.b.c", "a.b.c"},
		{"aliased", "import numpy as np", "numpy"},
		{"multiple", "import os, sys", "os,sys"},
		{"multiple aliased", "import os, numpy as np", "numpy,os"},
		{"from", "from os import path", "os"},
		{"from multiple", "from os import path, sep", "os"},
		{"from aliased", "from os import path as p", "os"},
		{"relative one", "from . import sibling", "."},
		{"relative named", "from .mod import thing", ".mod"},
		{"relative parent", "from ..pkg import thing", "..pkg"},
		{"star", "from os import *", "os"},
		{"wrapped", "from typing import (\n    Any,\n    Dict,\n)", "typing"},
		{"backslash", "import os, \\\n    sys", "os,sys"},
		// Malformed forms must yield nothing rather than a junk path.
		{"from with no import", "from os", ""},
		{"bare import", "import", ""},
	}
	for _, c := range cases {
		fa := extractPy(t, "a.py", c.src)
		if got := strings.Join(fa.ImportPaths(), ","); got != c.wantPaths {
			t.Errorf("%s: imports = %q, want %q", c.name, got, c.wantPaths)
		}
	}
}

func TestPythonImportNames(t *testing.T) {
	fa := extractPy(t, "a.py", "from typing import Any, Dict, Optional\n")
	if len(fa.Imports) != 1 {
		t.Fatalf("expected 1 import, got %+v", fa.Imports)
	}
	if got := strings.Join(fa.Imports[0].Names, ","); got != "Any,Dict,Optional" {
		t.Errorf("Names = %q, want the named symbols", got)
	}
}

// A star import names nothing specific, and recording "*" as a symbol would put a
// name in the graph that no declaration matches.
func TestPythonStarImportHasNoNames(t *testing.T) {
	fa := extractPy(t, "a.py", "from legacy import *\n")
	if len(fa.Imports) != 1 {
		t.Fatalf("expected 1 import, got %+v", fa.Imports)
	}
	if len(fa.Imports[0].Names) != 0 {
		t.Errorf("a star import should name nothing, got %v", fa.Imports[0].Names)
	}
}

// A generic in an import list contains a comma that is not a separator.
func TestPythonImportWithSubscriptedName(t *testing.T) {
	fa := extractPy(t, "a.py", "from typing import Dict, List\nx: Dict[str, int] = {}\n")
	if got := strings.Join(fa.Imports[0].Names, ","); got != "Dict,List" {
		t.Errorf("Names = %q, want Dict,List", got)
	}
}

func TestPythonDocstrings(t *testing.T) {
	fa := extractPy(t, "a.py", `
def one():
    """Does one thing."""
    pass

def two():
    """First sentence. Second is dropped."""
    pass

def three():
    """
    Leading newline style.
    """
    pass

def four():
    'Single quoted docstring.'
    pass

def none():
    pass
`)
	docs := map[string]string{}
	for _, s := range fa.Symbols {
		docs[s.Name] = s.Doc
	}
	want := map[string]string{
		"one":   "Does one thing.",
		"two":   "First sentence.",
		"three": "Leading newline style.",
		"four":  "Single quoted docstring.",
		"none":  "",
	}
	for name, w := range want {
		if docs[name] != w {
			t.Errorf("%s doc = %q, want %q", name, docs[name], w)
		}
	}
}

func TestPythonClassDocstring(t *testing.T) {
	fa := extractPy(t, "a.py", "class A:\n    \"\"\"A class.\"\"\"\n    pass\n")
	for _, s := range fa.Symbols {
		if s.Name == "A" && s.Doc != "A class." {
			t.Errorf("class doc = %q, want %q", s.Doc, "A class.")
		}
	}
}

func TestPythonEntrypoint(t *testing.T) {
	cases := []struct {
		name, src string
		want      bool
	}{
		{"canonical", "if __name__ == \"__main__\":\n    pass", true},
		{"single quotes", "if __name__ == '__main__':\n    pass", true},
		{"spaced", "if __name__=='__main__':\n    pass", true},
		{"in a string is not a guard", "x = \"if __name__ == '__main__':\"", false},
		{"nested is not a guard", "def f():\n    if __name__ == \"__main__\":\n        pass", false},
	}
	for _, c := range cases {
		fa := extractPy(t, "a.py", c.src)
		got := len(fa.Entrypoints) > 0
		if got != c.want {
			t.Errorf("%s: entrypoint detected = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPythonConstantVersusVariable(t *testing.T) {
	fa := extractPy(t, "a.py", "MAX = 1\nname = \"x\"\nMIXED_Case = 2\n_HIDDEN = 3\n")
	kinds := map[string]SymbolKind{}
	for _, s := range fa.Symbols {
		kinds[s.Name] = s.Kind
	}
	if kinds["MAX"] != SymConst {
		t.Errorf("MAX kind = %q, want const", kinds["MAX"])
	}
	if kinds["name"] != SymVar {
		t.Errorf("name kind = %q, want var", kinds["name"])
	}
	if kinds["_HIDDEN"] != SymConst {
		t.Errorf("_HIDDEN kind = %q, want const (screaming case)", kinds["_HIDDEN"])
	}
}

// Assignments that are not declarations of a new importable name.
func TestPythonRejectsNonDeclarationAssignments(t *testing.T) {
	fa := extractPy(t, "a.py", `
x = 1
x += 1
x -= 1
obj.attr = 2
d["k"] = 3
a, b = 1, 2
if x == 1:
    pass
while x != 2:
    break
result = x <= 5
`)
	names := strings.Join(fa.SymbolNames(), ",")
	for _, bad := range []string{"obj.attr", "a, b", "d[", "if x"} {
		if strings.Contains(names, bad) {
			t.Errorf("extracted a non-declaration %q from %q", bad, names)
		}
	}
	// The two genuine declarations are found.
	if !strings.Contains(names, "x") || !strings.Contains(names, "result") {
		t.Errorf("genuine declarations missing from %q", names)
	}
}

func TestPythonEmptyAndCommentOnlyFiles(t *testing.T) {
	for _, src := range []string{"", "\n\n\n", "# just a comment\n", `"""Only a docstring."""`} {
		fa, err := PythonExtractor{}.Extract(pyFile("a.py", src))
		if err != nil {
			t.Errorf("Extract(%q) errored: %v", src, err)
		}
		if len(fa.Symbols) != 0 || len(fa.Imports) != 0 {
			t.Errorf("Extract(%q) found facts where there are none: %+v", src, fa)
		}
	}
}

func TestPythonIsDeterministic(t *testing.T) {
	src := `
import os
from typing import Any
class C:
    def z(self): pass
    def a(self): pass
def top(): pass
X = 1
`
	want := extractPy(t, "a.py", src)
	for i := 0; i < 20; i++ {
		got := extractPy(t, "a.py", src)
		if strings.Join(got.SymbolNames(), ",") != strings.Join(want.SymbolNames(), ",") {
			t.Fatalf("run %d: symbols differ", i)
		}
		if strings.Join(got.ImportPaths(), ",") != strings.Join(want.ImportPaths(), ",") {
			t.Fatalf("run %d: imports differ", i)
		}
	}
}
