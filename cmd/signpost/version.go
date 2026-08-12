package main

import (
	"io"
	"runtime/debug"
	"strings"

	"github.com/3rg0n/signpost/internal/vcs"
)

// runVersion says what this binary is, in enough detail to tell a stale one from a
// current one.
//
// That is the whole point of the command and it did not do it: the version is stamped
// at link time by the release workflow, so every build that did not come through
// that path printed `dev` — a binary from this minute and one from a fortnight ago,
// reporting the same string. The symptom is not "an old version", it is `signpost:
// unknown command "view"`, which reads as a missing feature rather than a stale
// install, and `version` was the one command that could have said otherwise.
//
// So what it prints now is provenance rather than a bare string, and the source is
// runtime/debug: the Go toolchain records the revision, the commit time, and whether
// the tree was dirty into any binary built from a checkout, which is exactly the
// `go install` case ldflags do not cover. Stdlib, no new dependency, and nothing to
// keep in step — the toolchain fills it in.
func runVersion(_ []string, out, _ io.Writer) error {
	info, ok := debug.ReadBuildInfo()
	p := newPrinter(out)
	p.printf("%s\n", versionString(version, info, ok))
	return p.Err()
}

// versionString picks the most specific true thing about this build.
//
// A pure function of the two inputs, and that shape is what makes it testable at all:
// each branch below corresponds to a differently *built* binary, and a test can only
// ever be one of them. A test binary in particular carries no `vcs.*` settings — the
// toolchain does not stamp them into `go test` — so a version that read
// debug.ReadBuildInfo() inline would have three branches no test could reach.
//
// Order is by precision, not by preference. The four cases, all observed rather than
// assumed:
//
//   - A release. `-ldflags "-X main.version=v0.1.0"`, so the tag is the answer and
//     nothing here can improve on it.
//   - A checkout. `go build`, or `go install ./cmd/signpost` from a clone: `vcs.*`
//     carries the revision, the commit time, and whether the tree was modified.
//   - A module. `go install github.com/3rg0n/signpost/cmd/signpost@latest` builds from
//     a proxy zip, which has no `.git`, so there are no `vcs.*` settings at all — but
//     Main.Version carries the real tag. This case is why the fix is not only about
//     `vcs.*`: the documented install command produced a binary that printed `dev`
//     while being precisely v0.1.0.
//   - Neither. A test binary, or a build with `-buildvcs=false`. `dev` is then the
//     most specific true thing, which is what it already said.
func versionString(injected string, info *debug.BuildInfo, ok bool) string {
	if injected != devVersion {
		return injected
	}
	if !ok {
		// Only reachable in a binary built without module support, which the released
		// ones are not. Nothing to add.
		return injected
	}
	if rev := setting(info, "vcs.revision"); rev != "" {
		// The same 7 characters every other display of a sha in this tool uses, through
		// the same function, so `version` and a bundle page abbreviate a commit alike.
		out := injected + " (" + vcs.Commit{SHA: rev}.Short()
		if when := setting(info, "vcs.time"); when != "" {
			// The date, not the timestamp. The question this answers is "how old is this
			// binary", which a date settles, and the revision beside it is already exact
			// to the second for anybody who needs that.
			date, _, _ := strings.Cut(when, "T")
			out += ", " + date
		}
		if setting(info, "vcs.modified") == "true" {
			// Said out loud, because it is the case where the revision is a half-truth: the
			// commit is real and the binary contains something that is not in it.
			out += ", dirty"
		}
		return out + ")"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		// Qualified rather than printed bare. The tag is genuine, but this binary is not
		// the artifact the release published — somebody's own toolchain built it from the
		// module cache — and a `version` that claimed otherwise would be the same kind of
		// quiet wrong answer the bare `dev` was.
		return v + " (go install)"
	}
	return injected
}

// setting reads one build setting, or empty when the toolchain recorded none.
//
// A linear scan over a handful of entries, rather than a map built once: this runs
// at most once per process, and a map would be built to be read three times.
func setting(info *debug.BuildInfo, key string) string {
	for _, s := range info.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}
