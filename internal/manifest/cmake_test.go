package manifest

import (
	"strings"
	"testing"
	"time"

	"github.com/3rg0n/signpost/internal/discover"
)

func cmakeFile(path, content string) discover.File {
	return discover.File{Path: path, Class: discover.ClassManifest, Content: content}
}

func TestExtractCMakeReadsTargetsAndDependencies(t *testing.T) {
	f := cmakeFile("CMakeLists.txt", `
cmake_minimum_required(VERSION 3.20)
project(corpus VERSION 2.4.0 LANGUAGES C CXX)

find_package(OpenSSL 3.0 REQUIRED)
find_package(ZLIB)

add_library(corpus_core STATIC src/core.c src/util.c)
add_executable(corpusd src/main.c)
target_link_libraries(corpusd PRIVATE corpus_core OpenSSL::SSL)

add_subdirectory(tools)

enable_testing()
add_test(NAME core_roundtrip COMMAND corpusd --selftest)
`)
	facts := ExtractCMake(f)

	if facts.Kind != KindCMake {
		t.Errorf("kind = %q, want %q", facts.Kind, KindCMake)
	}
	if facts.Incomplete {
		t.Fatalf("a well-formed CMakeLists must not be reported as partially read: %q", facts.Note)
	}
	if facts.Module.Name != "corpus" || facts.Module.Version != "2.4.0" {
		t.Errorf("module = %+v, want name and VERSION from project()", facts.Module)
	}
	if facts.Module.Ecosystem != ecoCMake {
		t.Errorf("module ecosystem = %q, want %q", facts.Module.Ecosystem, ecoCMake)
	}

	deps := map[string]Dep{}
	for _, d := range facts.Deps {
		deps[d.Name] = d
	}
	// A version given positionally is the constraint; REQUIRED is not part of the identity.
	if got := deps["OpenSSL"]; got.Version != "3.0" || got.Optional {
		t.Errorf("OpenSSL = %+v, want version 3.0 and not optional", got)
	}
	// A package the build tolerates the absence of is optional, which is the fact a reader
	// auditing what actually ships needs — and it is stated by REQUIRED's absence alone.
	if got := deps["ZLIB"]; !got.Optional {
		t.Errorf("ZLIB = %+v, want optional for a find_package with no REQUIRED", got)
	}
	// A namespaced link target reduces to its package: one dependency with a part of it
	// used, not two.
	if _, ok := deps["OpenSSL::SSL"]; ok {
		t.Errorf("deps = %+v; a namespaced target must reduce to its package", facts.Deps)
	}
	// A subdirectory is this repository's own code, resolved against the declaring file.
	if got := deps["tools"]; got.Source != "tools" || !got.Local {
		t.Errorf("tools = %+v, want a local dependency on the resolved directory", got)
	}

	// The linked sibling library is declared in this file, so it is not a dependency at
	// all — the whole reason target_link_libraries is deferred until the declarations are in.
	if _, ok := deps["corpus_core"]; ok {
		t.Errorf("deps = %+v; a target this file declares is not an external dependency", facts.Deps)
	}

	// An executable is a program, a library is not. That distinction is the one thing this
	// pair of commands exists to make.
	if len(facts.Entrypoints) != 1 || facts.Entrypoints[0].Name != "corpusd" {
		t.Fatalf("entrypoints = %+v, want only the executable", facts.Entrypoints)
	}
	if facts.Entrypoints[0].Path != "src/main.c" {
		t.Errorf("entrypoint path = %q, want the sources with the keywords dropped", facts.Entrypoints[0].Path)
	}

	// add_test is the only place a C project says a binary is a test, which is what makes
	// practice's test finding fire for a repository whose tests are declared nowhere else.
	if len(facts.Scripts) != 1 || facts.Scripts[0].Name != "core_roundtrip" {
		t.Fatalf("scripts = %+v, want the add_test target", facts.Scripts)
	}
	if !strings.Contains(facts.Scripts[0].Command, "--selftest") {
		t.Errorf("test command = %q, want the COMMAND arguments", facts.Scripts[0].Command)
	}
}

// A target declared below the link that names it is still a target. The deferral exists for
// exactly this: CMake permits the declaration anywhere, and reading in one pass would report
// a sibling library as a package from outside.
func TestExtractCMakeDefersLinksUntilDeclarationsAreIn(t *testing.T) {
	facts := ExtractCMake(cmakeFile("CMakeLists.txt", `
project(late)
add_executable(app main.c)
target_link_libraries(app PRIVATE helper)
add_library(helper helper.c)
`))
	for _, d := range facts.Deps {
		if d.Name == "helper" {
			t.Fatalf("deps = %+v; helper is declared in this file, below the link that names it", facts.Deps)
		}
	}
}

// The negative boundary on link reading: a name this file never declares *is* a dependency,
// and dropping it would lose the actual declaration. Both halves have to hold or the deferral
// above is indistinguishable from dropping everything.
func TestExtractCMakeRecordsUndeclaredLinksAsDependencies(t *testing.T) {
	facts := ExtractCMake(cmakeFile("CMakeLists.txt", `
project(links)
add_executable(app main.c)
target_link_libraries(app PRIVATE fmt::fmt pthread)
`))
	got := map[string]bool{}
	for _, d := range facts.Deps {
		got[d.Name] = true
	}
	for _, want := range []string{"fmt", "pthread"} {
		if !got[want] {
			t.Errorf("deps = %+v, want %q recorded", facts.Deps, want)
		}
	}
	// PRIVATE is a keyword and `app` is the linking target, so neither is a dependency.
	for _, unwanted := range []string{"PRIVATE", "app"} {
		if got[unwanted] {
			t.Errorf("deps = %+v; %q is not a dependency", facts.Deps, unwanted)
		}
	}
}

func TestExtractCMakeReadsFetchContentPins(t *testing.T) {
	facts := ExtractCMake(cmakeFile("cmake/deps.cmake", `
include(FetchContent)
FetchContent_Declare(
  googletest
  GIT_REPOSITORY https://github.com/google/googletest.git
  GIT_TAG        v1.14.0
)
pkg_check_modules(SSL REQUIRED libssl>=3.0 libcrypto)
`))

	deps := map[string]Dep{}
	for _, d := range facts.Deps {
		deps[d.Name] = d
	}
	// The pin and the origin together are the whole supply-chain fact: a git dependency has
	// no registry to publish an advisory against.
	if got := deps["googletest"]; got.Version != "v1.14.0" ||
		got.Source != "https://github.com/google/googletest.git" {
		t.Errorf("googletest = %+v, want the tag and the repository", got)
	}
	// pkg-config writes name and constraint together; Dep keeps them apart.
	if got := deps["libssl"]; got.Version != ">=3.0" || got.Ecosystem != ecoPkgConfig {
		t.Errorf("libssl = %+v, want the constraint split off and the pkg-config ecosystem", got)
	}
	if got := deps["libcrypto"]; got.Ecosystem != ecoPkgConfig || got.Version != "" {
		t.Errorf("libcrypto = %+v, want no invented constraint", got)
	}
	// The variable prefix is not a package, and neither is the keyword beside it.
	for _, unwanted := range []string{"SSL", "REQUIRED"} {
		if _, ok := deps[unwanted]; ok {
			t.Errorf("deps = %+v; %q is not a package", facts.Deps, unwanted)
		}
	}
}

// The honest-limit boundary. A computed target name is a target this reader cannot name, and
// recording `${APP_NAME}` would put a target in the bundle that no build ever produces.
func TestExtractCMakeReportsComputedNamesAsUnread(t *testing.T) {
	facts := ExtractCMake(cmakeFile("CMakeLists.txt", `
project(computed)
set(APP_NAME tool)
add_executable(${APP_NAME} main.c)
`))
	if len(facts.Entrypoints) != 0 {
		t.Errorf("entrypoints = %+v; a computed name is not a target name", facts.Entrypoints)
	}
	if !facts.Incomplete || !strings.Contains(facts.Note, "computed") {
		t.Errorf("incomplete = %v note = %q, want the unread target recorded", facts.Incomplete, facts.Note)
	}
}

// An IMPORTED or ALIAS library is a name for something built elsewhere, so recording it as a
// unit of this repository would claim it is built here. It still counts as declared, which is
// what keeps a link to it out of the dependency list.
func TestExtractCMakeTreatsImportedTargetsAsDeclaredNotBuilt(t *testing.T) {
	facts := ExtractCMake(cmakeFile("CMakeLists.txt", `
project(imported)
add_library(prebuilt STATIC IMPORTED)
add_executable(app main.c)
target_link_libraries(app PRIVATE prebuilt)
`))
	if len(facts.Entrypoints) != 1 {
		t.Errorf("entrypoints = %+v, want only the executable", facts.Entrypoints)
	}
	for _, d := range facts.Deps {
		if d.Name == "prebuilt" {
			t.Errorf("deps = %+v; an IMPORTED target is declared here, not depended on", facts.Deps)
		}
	}
}

// A nested project() is a real pattern in vendored trees, and taking the last would name the
// whole repository after whatever it vendored.
func TestExtractCMakeKeepsTheFirstProjectName(t *testing.T) {
	facts := ExtractCMake(cmakeFile("CMakeLists.txt", `
project(outer)
add_subdirectory(vendor/inner)
project(inner)
`))
	if facts.Module.Name != "outer" {
		t.Errorf("module name = %q, want the first project()", facts.Module.Name)
	}
}

// A find_package's second positional argument is a version or nothing. An unknown all-caps
// keyword sitting there must not become a constraint: a dependency pinned to "EXACT" reads as
// a real pin and is wrong in the direction a reader acts on.
func TestExtractCMakeDoesNotInventVersions(t *testing.T) {
	facts := ExtractCMake(cmakeFile("CMakeLists.txt", `
project(v)
find_package(Threads REQUIRED)
find_package(Boost EXACT REQUIRED)
`))
	for _, d := range facts.Deps {
		if d.Version != "" {
			t.Errorf("dep %+v; neither call states a version", d)
		}
	}
}

func TestParseCMakeHandlesQuotingAndComments(t *testing.T) {
	cmds, diag := parseCMake(`
# a leading comment with a ) in it
#[[
add_executable(never_seen ghost.c)
]]
add_executable(app "src/main file.c" src/other.c)  # trailing
set(TEXT [[a raw ) string]])
if(NOT (WIN32 AND MSVC) AND (UNIX OR APPLE))
  add_library(win STATIC win.c)
endif()
`)
	if diag.Malformed {
		t.Fatalf("diag = %+v, want a clean read", diag)
	}
	names := map[string]cmakeCommand{}
	for _, c := range cmds {
		if n := c.literal(0); n != "" {
			names[n] = c
		}
	}
	// A bracket comment spans lines. A to-end-of-line skip would read the command inside it.
	if _, ok := names["never_seen"]; ok {
		t.Errorf("commands = %+v; a #[[ ]] comment spans lines", cmds)
	}
	// A quoted argument holding a space is one argument, not two.
	app, ok := names["app"]
	if !ok {
		t.Fatalf("commands = %+v, want the app target", cmds)
	}
	if got := app.literal(1); got != "src/main file.c" {
		t.Errorf("first source = %q, want the quoted path whole", got)
	}
	// Nested parens are counted, not treated as terminators. Asserted as the exact command
	// sequence rather than by the presence of `win`, because losing the count does not lose that
	// target — it ends `if(` early and leaves `AND (UNIX OR APPLE))` to be read as a command of
	// its own, which a by-name check for what survives would not see. Two groups, not one,
	// because a single nested group is indistinguishable: returning at its closer happens to
	// land where the real end of the argument list is.
	want := []string{"add_executable", "set", "if", "add_library", "endif"}
	var got []string
	for _, c := range cmds {
		got = append(got, c.name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("commands = %v, want exactly %v", got, want)
	}
	if _, ok := names["win"]; !ok {
		t.Errorf("commands = %+v, want the target inside the nested if()", cmds)
	}
}

// The termination boundary, matching the Starlark one: a malformed file is read wrongly at
// worst, never read forever. A byte no scanner in the argument loop consumes was once offered
// to that loop unchanged until the process was killed, and a reader that hangs stops the whole
// bundle rather than reporting one partial page. Written with a deadline because a spin does
// not fail a test, it hangs the suite.
func TestParseCMakeTerminatesOnEveryInput(t *testing.T) {
	for _, src := range []string{
		"add_executable(app\n",
		"add_executable(\"unterminated\n",
		"add_executable([=[unterminated\n",
		"add_executable(a[b)\n",
		"#[[ unterminated bracket comment\n",
	} {
		done := make(chan bool, 1)
		go func(src string) {
			parseCMake(src)
			done <- true
		}(src)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("parseCMake(%q) did not terminate", src)
		}
	}
}

// An unterminated argument list is a broken document rather than a construct this reader
// steps over, which is the distinction Diag.Malformed exists to carry.
func TestParseCMakeReportsUnterminatedArgumentLists(t *testing.T) {
	cmds, diag := parseCMake("project(fine)\nadd_executable(app main.c\n")
	if !diag.Malformed {
		t.Errorf("diag = %+v, want the document reported unparseable", diag)
	}
	// What was read before the break is kept: the commands above the fault still hold.
	if len(cmds) < 1 || cmds[0].name != "project" {
		t.Errorf("commands = %+v, want the commands before the fault", cmds)
	}
}

func TestMatchCMakeClaimsBuildFilesAndModulesOnly(t *testing.T) {
	for _, p := range []string{"CMakeLists.txt", "src/CMakeLists.txt", "cmake/FindFoo.cmake"} {
		if !matchCMake(discover.File{Path: p}) {
			t.Errorf("matchCMake(%q) = false, want true", p)
		}
	}
	// The negative half: `.txt` is overwhelmingly not CMake, and claiming every one would
	// route every README.txt and requirements.txt through a command parser.
	for _, p := range []string{"README.txt", "requirements.txt", "docs/notes.txt", "Makefile"} {
		if matchCMake(discover.File{Path: p}) {
			t.Errorf("matchCMake(%q) = true, want false", p)
		}
	}
}
