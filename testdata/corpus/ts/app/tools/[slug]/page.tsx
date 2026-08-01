// A Next.js dynamic route. The `[slug]` directory is the point of this file:
// `[` is a YAML flow indicator, so an unquoted path here made the emitted
// frontmatter unparseable and silently dropped edges. Issue #9.
import React from "react";

import { greet } from "../../../src/greeter";
// An exact `paths` pattern, no wildcard, matched whole.
import { greet as greetAgain } from "@corpus/entry";
// A two-target alias whose first target does not exist. Resolution must fall through to
// the second rather than stopping at the miss.
import Marketing from "@corpus/ui/(marketing)/page";
// An alias onto something that is not source. The pattern matches and there is no module
// to point at, so the specifier must be claimed with no edge — falling through here reports
// a third-party package named `@corpus`, which is the mapping itself sold as a dependency.
import logo from "@corpus/assets/logo.svg";

export default function ToolPage({ params }: { params: { slug: string } }) {
  void greetAgain;
  void Marketing;
  void logo;
  return <p>{greet(params.slug).text}</p>;
}
