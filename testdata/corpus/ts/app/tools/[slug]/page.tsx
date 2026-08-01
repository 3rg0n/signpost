// A Next.js dynamic route. The `[slug]` directory is the point of this file:
// `[` is a YAML flow indicator, so an unquoted path here made the emitted
// frontmatter unparseable and silently dropped edges. Issue #9.
import React from "react";

import { greet } from "../../../src/greeter";

export default function ToolPage({ params }: { params: { slug: string } }) {
  return <p>{greet(params.slug).text}</p>;
}
