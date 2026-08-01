// Cross-package imports by published name — the shape a workspace actually uses, and
// the one that used to be reported as a third-party dependency.
//
// Both spellings appear on purpose: the bare specifier, which resolves through the
// package root, and the deep import, which addresses a path inside it. They took
// different paths through the resolver and only the bare one was ever exercised by a
// fixture.
import { mint } from "@corpus/core";
import { encode } from "@corpus/core/src/codec";
import { useState } from "react";

// A tsconfig `paths` alias, resolved through a config this file does not itself declare:
// packages/api/tsconfig.json states only `extends`, so `@corpus/app` is defined two
// directories up. Without reading that inheritance this import is unresolved, and without
// reading `paths` at all it becomes an external dependency named `@corpus`.
import { greet } from "@corpus/app/greeter";

// The negative boundary for that alias, and the reason it is spelled this way: the pattern
// is `@corpus/app/*`, so the prefix a matcher compares against ends in a slash.
// `@corpus/apples` shares the first ten characters and is a different name. A matcher that
// compared the prefix without its slash claims this specifier, captures `les/juice` as the
// wildcard, and maps it to `src/les/juice` — a wrong answer, silently, with no edge to show
// for it. Nothing declares `@corpus/apples`, so the only correct outcome is unresolved:
// neither routed into this repository nor invented as a package.
import { juice } from "@corpus/apples/juice";

// Node's own builtins are the runtime, not a dependency. Nobody patches `node:fs` and it
// belongs in no manifest, so it must produce no external node and must not be counted as a
// gap in the map either — silence is the correct report, and it is the one outcome that
// looks identical to a resolver that simply lost the import.
import fs from "node:fs";

// The same modules addressed by subpath, which is how half of them are used in practice:
// there is no other way to reach the promise-based `fs`. Both spellings appear because they
// take different routes — the bare one is what older code writes and what a real repository
// was reporting as an unresolved dependency, and the prefixed one only lands if `node:` is
// trimmed before the subpath is cut, which is the ordering issue #14 turns on.
import fsp from "fs/promises";
import { tap } from "node:test/reporters";

// The negative boundary for that rule, and the reason it is a package and not a builtin:
// `pathe` is a real npm path utility whose name begins with the four characters of the
// builtin `path`. A first-segment lookup sees `pathe`, which is not a builtin, and reports
// it — correct, because nothing here declares it. A prefix comparison instead sees `path`
// and calls this the runtime, which drops a genuine supply-chain fact and prints nothing to
// say it did. The whole subpath rule is one `strings.Cut` away from that.
import { resolve } from "pathe/utils";

export function handle(raw: string): string {
  void useState;
  void greet;
  void juice;
  void fs;
  void fsp;
  void tap;
  void resolve;
  return encode(mint(raw).value);
}
