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

export function handle(raw: string): string {
  void useState;
  return encode(mint(raw).value);
}
