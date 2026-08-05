package assemble

import (
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
	"github.com/3rg0n/signpost/internal/graph"
	"github.com/3rg0n/signpost/internal/manifest"
)

// Descriptions here are assembled from counted facts, never composed as prose.
//
// The distinction is the point of §4.5: a deterministic pass can honestly say "12
// files, 47 exported symbols, entrypoint main" and cannot honestly say what a module
// is *for*. Writing the second from the first would be invention, and it would then
// be indistinguishable from the semantic pass's grounded output — which is what the
// generated/verified trust grading exists to keep separate. So these read as
// inventories, deliberately.

// moduleName is the slug source for a directory: its own basename, which is what
// people call it. The full path is the title.
func moduleName(dir string) string {
	if dir == "" {
		return "root"
	}
	return path.Base(dir)
}

// moduleTitle is the full repo-relative path, because a repository routinely has
// several directories named `handlers` and the basename alone would not say which.
func moduleTitle(dir string) string {
	if dir == "" {
		return "(repository root)"
	}
	return dir
}

func moduleDescription(n *graph.Node, exported int) string {
	parts := []string{plural(len(n.Files), "file")}
	if n.Lang != "" {
		parts[0] = plural(len(n.Files), n.Lang+" file")
	}
	if exported > 0 {
		parts = append(parts, plural(exported, "exported symbol"))
	}
	if ep := n.Attrs["entrypoints"]; ep != "" {
		parts = append(parts, "entrypoint "+ep)
	}
	if pkg := n.Attrs["package"]; pkg != "" && pkg != path.Base(n.Path) {
		parts = append(parts, "package "+pkg)
	}
	return capitalize(strings.Join(parts, "; ")) + "."
}

func serviceDescription(n *graph.Node, kinds []string) string {
	what := "Service"
	if k := sortedUnique(kinds); len(k) > 0 {
		what = strings.Join(k, "/")
	}
	parts := []string{what + " " + n.Title}
	if img := n.Attrs["image"]; img != "" {
		parts = append(parts, "image "+img)
	} else if bd := n.Attrs["build"]; bd != "" {
		parts = append(parts, "built from "+bd)
	}
	if p := n.Attrs["ports"]; p != "" {
		parts = append(parts, "ports "+p)
	}
	if s := n.Attrs["secret_refs"]; s != "" {
		parts = append(parts, "reads secrets "+s)
	}
	return strings.Join(parts, "; ") + "."
}

// contractName titles an interface by its filename, which is what a reader greps for.
func contractName(f manifest.Facts) string {
	base := path.Base(f.Path)
	if f.Module.Name != "" {
		return f.Module.Name + " (" + base + ")"
	}
	return base
}

func interfaceDescription(f manifest.Facts, byKind map[string][]string) string {
	var parts []string
	for _, k := range sortedKeys(byKind) {
		parts = append(parts, plural(len(sortedUnique(byKind[k])), k))
	}
	return capitalize(string(f.Kind)) + " definition: " + strings.Join(parts, ", ") + "."
}

// documentName titles a constraint document by its heading-bearing name.
func documentName(f manifest.Facts) string {
	if f.Kind == manifest.KindADR && f.Module.Name != "" {
		if f.Module.Version != "" {
			return "ADR " + f.Module.Version + ": " + f.Module.Name
		}
		return "ADR: " + f.Module.Name
	}
	return path.Base(f.Path)
}

func documentDescription(f manifest.Facts) string {
	what := "Stated constraints"
	if f.Kind == manifest.KindADR {
		what = "Architecture decision"
		if st := adrStatus(f); st != "" {
			what += " (" + st + ")"
		}
	}
	return what + ", " + plural(len(f.Rules), "rule") + " read from " + path.Base(f.Path) + "."
}

// adrStatus reads the status an ADR reader recorded.
//
// It lands in Module.LangVersion, which reads oddly and is deliberate: repo.go put it
// there rather than adding a field used by exactly one reader, and the mapping is
// documented at both ends so it cannot drift silently.
func adrStatus(f manifest.Facts) string { return f.Module.LangVersion }

// isStdlib reports whether an import names the language's own library, which is
// resolved — to nothing that deserves a node.
//
// A standard-library import is not a supply-chain fact: nobody patches it separately,
// no advisory is published against it independently of the toolchain, and a
// references/ page for `os` would be noise in the one index that should be scannable.
// Counting it as unresolved would be worse still: it would make every honest repo
// look like assembly had failed.
func isStdlib(lang discover.Lang, raw string) bool {
	switch lang {
	case discover.LangGo:
		// The toolchain's own rule: no dot in the first path segment means standard
		// library. Cheaper and more accurate than a package list that goes stale.
		first, _, _ := strings.Cut(raw, "/")
		return !strings.Contains(first, ".")
	case discover.LangRust:
		first, _, _ := strings.Cut(raw, "::")
		switch first {
		case "std", "core", "alloc", "proc_macro", "test":
			return true
		}
		return false
	case discover.LangPython:
		first, _, _ := strings.Cut(raw, ".")
		return pyStdlib[first]
	case discover.LangJava, discover.LangKotlin:
		return jvmRuntimePackage(raw)
	case discover.LangTS, discover.LangJS:
		// Node's builtins are spelled `node:fs` in modern code and `fs` in older
		// code; both are the runtime, not a dependency.
		//
		// Several are also addressed by subpath — `fs/promises`, `stream/web`,
		// `util/types`, `node:test/reporters` — and the subpath is the same module,
		// shipped by the same runtime. So the first segment is what is looked up, as
		// the Python and Rust arms above already do with their own separators. The
		// prefix is trimmed first, which is what makes `node:test/reporters` land.
		//
		// Cutting on the separator rather than matching a prefix is the whole of the
		// care needed here: `pathe/utils` begins with the four characters of the
		// builtin `path` and is an npm package. This function is consulted only after
		// resolution has already failed, so a prefix comparison could not lose a
		// declared dependency's edge — what it would lose is the *report*. An
		// undeclared package silently reclassified as the runtime is a gap the reader
		// is never told about, which is the one thing the unresolved count exists to
		// surface, and it is the same direction this fix moves in.
		first, _, _ := strings.Cut(strings.TrimPrefix(raw, "node:"), "/")
		return nodeBuiltin[first]
	}
	return false
}

// jvmRuntimePackage reports whether a JVM import names a package the platform itself
// ships.
//
// Narrower than it first looks, and the narrowness is the point. `java.*` and `kotlin.*`
// are the JDK and the Kotlin standard library, shipped with the toolchain and patched
// with it. The three neighbouring prefixes that look like they belong are not:
//
//   - `jakarta.*` is Eclipse's EE namespace, and every one of its packages arrives as a
//     Maven artifact somebody chose and must upgrade. Calling it the runtime would hide
//     a real supply-chain fact behind the word "standard library".
//   - `kotlinx.*` is the Kotlin *extensions* namespace — coroutines, serialization,
//     datetime — and each is a separate versioned artifact.
//   - `javax.*` is split. `javax.crypto` and `javax.swing` are in the JDK; `javax.servlet`
//     and `javax.persistence` are the pre-rename EE artifacts. So the second segment is
//     what decides, and only the JDK's own list is accepted.
//
// Everything else — including a real dependency signpost has no manifest for, since #19
// reads no pom.xml or build.gradle — lands in the unresolved count, which is the report
// that tells a reader the map has a gap here rather than leaving them to infer it.
func jvmRuntimePackage(raw string) bool {
	first, rest, _ := strings.Cut(raw, ".")
	second, _, _ := strings.Cut(rest, ".")
	switch first {
	case "java", "kotlin", "sun", "jdk":
		return true
	case "com":
		// `com` is the most common first segment in any JVM repository; only `com.sun`
		// is the platform.
		return second == "sun"
	case "javax":
		return jdkJavaxPackages[second]
	}
	return false
}

// jdkJavaxPackages is the second segment of each `javax.*` package a current JDK ships.
//
// A list rather than a rule, because `javax` is the one JVM prefix with no rule: the
// namespace was split between the platform and Java EE in 1999 and the split is
// historical. Taken from the module exports of a current JDK, which is why two names an
// older list would hold are absent — `javax.annotation` and `javax.transaction` were both
// exported by JDK 8 and both moved out to Maven artifacts in JDK 11, so treating them as
// the runtime today would hide a dependency somebody has to upgrade.
var jdkJavaxPackages = map[string]bool{
	"accessibility": true,
	"crypto":        true,
	"imageio":       true,
	"lang":          true, // javax.lang.model, from java.compiler
	"management":    true,
	"naming":        true,
	"net":           true,
	"print":         true,
	"rmi":           true,
	"script":        true,
	"security":      true,
	"smartcardio":   true,
	"sound":         true,
	"sql":           true,
	"swing":         true,
	"tools":         true, // javax.tools, from java.compiler
	"xml":           true,
}

// pyStdlib is the Python standard-library top-level names.
//
// A list rather than a rule, because Python has no syntactic marker distinguishing
// `import os` from `import requests` — the only difference is which one is installed.
//
// Complete rather than representative, and the difference is the point. This began as a
// hand-kept list of what appears in common code, on the reasoning that a missing entry
// degrades to "unresolved" — visible and correctable rather than silent. That reasoning was
// wrong in one direction it did not anticipate: the names it omitted were not obscure, they
// were *platform-specific*. `winreg` and `msvcrt` are Windows-only and `fcntl`, `grp`, `pwd`
// and `termios` are Unix-only, so a list assembled from code read on one platform omits
// exactly the modules the other platform's code imports. A repository doing conditional
// imports for portability was reported as depending on packages nobody can install, and the
// gap count — the number a reader uses to judge whether the map covers their repository —
// was inflated by the most portable code in the tree.
//
// Generated from `sys.stdlib_module_names`, less the private `_`-prefixed names, which are
// implementation internals nothing imports directly. Kept as a literal rather than derived at
// runtime because signpost does not require a Python interpreter to read Python.
var pyStdlib = map[string]bool{
	"__future__": true, "abc": true, "annotationlib": true, "antigravity": true,
	"argparse": true, "array": true, "ast": true, "asyncio": true, "atexit": true,
	"base64": true, "bdb": true, "binascii": true, "bisect": true, "builtins": true,
	"bz2": true, "cProfile": true, "calendar": true, "cmath": true, "cmd": true, "code": true,
	"codecs": true, "codeop": true, "collections": true, "colorsys": true, "compileall": true,
	"compression": true, "concurrent": true, "configparser": true, "contextlib": true,
	"contextvars": true, "copy": true, "copyreg": true, "csv": true, "ctypes": true,
	"curses": true, "dataclasses": true, "datetime": true, "dbm": true, "decimal": true,
	"difflib": true, "dis": true, "doctest": true, "email": true, "encodings": true,
	"ensurepip": true, "enum": true, "errno": true, "faulthandler": true, "fcntl": true,
	"filecmp": true, "fileinput": true, "fnmatch": true, "fractions": true, "ftplib": true,
	"functools": true, "gc": true, "genericpath": true, "getopt": true, "getpass": true,
	"gettext": true, "glob": true, "graphlib": true, "grp": true, "gzip": true,
	"hashlib": true, "heapq": true, "hmac": true, "html": true, "http": true, "idlelib": true,
	"imaplib": true, "importlib": true, "inspect": true, "io": true, "ipaddress": true,
	"itertools": true, "json": true, "keyword": true, "linecache": true, "locale": true,
	"logging": true, "lzma": true, "mailbox": true, "marshal": true, "math": true,
	"mimetypes": true, "mmap": true, "modulefinder": true, "msvcrt": true,
	"multiprocessing": true, "netrc": true, "nt": true, "ntpath": true, "nturl2path": true,
	"numbers": true, "opcode": true, "operator": true, "optparse": true, "os": true,
	"pathlib": true, "pdb": true, "pickle": true, "pickletools": true, "pkgutil": true,
	"platform": true, "plistlib": true, "poplib": true, "posix": true, "posixpath": true,
	"pprint": true, "profile": true, "pstats": true, "pty": true, "pwd": true,
	"py_compile": true, "pyclbr": true, "pydoc": true, "pydoc_data": true, "pyexpat": true,
	"queue": true, "quopri": true, "random": true, "re": true, "readline": true,
	"reprlib": true, "resource": true, "rlcompleter": true, "runpy": true, "sched": true,
	"secrets": true, "select": true, "selectors": true, "shelve": true, "shlex": true,
	"shutil": true, "signal": true, "site": true, "smtplib": true, "socket": true,
	"socketserver": true, "sqlite3": true, "sre_compile": true, "sre_constants": true,
	"sre_parse": true, "ssl": true, "stat": true, "statistics": true, "string": true,
	"stringprep": true, "struct": true, "subprocess": true, "symtable": true, "sys": true,
	"sysconfig": true, "syslog": true, "tabnanny": true, "tarfile": true, "tempfile": true,
	"termios": true, "textwrap": true, "this": true, "threading": true, "time": true,
	"timeit": true, "tkinter": true, "token": true, "tokenize": true, "tomllib": true,
	"trace": true, "traceback": true, "tracemalloc": true, "tty": true, "turtle": true,
	"turtledemo": true, "types": true, "typing": true, "unicodedata": true, "unittest": true,
	"urllib": true, "uuid": true, "venv": true, "warnings": true, "wave": true,
	"weakref": true, "webbrowser": true, "winreg": true, "winsound": true, "wsgiref": true,
	"xml": true, "xmlrpc": true, "zipapp": true, "zipfile": true, "zipimport": true,
	"zlib": true, "zoneinfo": true,

	// Removed from the standard library by PEP 594 and PEP 632, and still the standard
	// library of the interpreter the code being read runs on. signpost reads a repository,
	// not a running process: a project pinned to `requires-python = ">=3.8"` imports `cgi`
	// and `distutils` correctly, and reporting them as unresolved dependencies would make
	// every such repository look like the analysis had failed. There is no PyPI package
	// named `cgi` to mistake this for.
	"aifc": true, "asynchat": true, "asyncore": true, "audioop": true, "cgi": true,
	"cgitb": true, "chunk": true, "crypt": true, "distutils": true, "imghdr": true,
	"imp": true, "lib2to3": true, "mailcap": true, "msilib": true, "nis": true,
	"nntplib": true, "ossaudiodev": true, "pipes": true, "smtpd": true, "sndhdr": true,
	"spwd": true, "sunau": true, "telnetlib": true, "uu": true, "xdrlib": true,
}

// nodeBuiltin is Node's built-in module names.
var nodeBuiltin = map[string]bool{
	"assert": true, "async_hooks": true, "buffer": true, "child_process": true,
	"cluster": true, "console": true, "constants": true, "crypto": true, "dgram": true,
	"diagnostics_channel": true, "dns": true, "domain": true, "events": true, "fs": true,
	"http": true, "http2": true, "https": true, "inspector": true, "module": true,
	"net": true, "os": true, "path": true, "perf_hooks": true, "process": true,
	"punycode": true, "querystring": true, "readline": true, "repl": true, "stream": true,
	"string_decoder": true, "sys": true, "timers": true, "tls": true, "trace_events": true,
	"tty": true, "url": true, "util": true, "v8": true, "vm": true, "wasi": true,
	"worker_threads": true, "zlib": true, "test": true, "sqlite": true,
}

func plural(n int, unit string) string {
	s := strconv.Itoa(n) + " " + unit
	if n != 1 {
		s += "s"
	}
	return s
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	// ASCII only, and only the first byte: every string reaching here starts with a
	// count, a file kind, or a path, all of which are ASCII. Pulling in unicode
	// casing for that would be ceremony.
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}

func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func containsStr(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

// setJoined sets an attribute only when there is something to say. An empty attribute
// is worse than an absent one: it renders as a field with nothing after it.
func setJoined(attrs map[string]string, key string, vals []string) {
	if v := sortedUnique(vals); len(v) > 0 {
		attrs[key] = strings.Join(v, ", ")
	}
}

// dominant returns the most common language, ties broken alphabetically so the choice
// does not depend on file order.
func dominant(langs []string) string {
	if len(langs) == 0 {
		return ""
	}
	count := make(map[string]int, len(langs))
	for _, l := range langs {
		count[l]++
	}
	best := ""
	for _, l := range sortedKeys(count) {
		if best == "" || count[l] > count[best] {
			best = l
		}
	}
	return best
}
