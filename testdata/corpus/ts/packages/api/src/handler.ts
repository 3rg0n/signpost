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

export function handle(raw: string): string {
  void useState;
  void greet;
  void juice;
  void fs;
  return encode(mint(raw).value);
}
