"""A comma in a filename.

POSIX permits it and a comma terminates a YAML flow-mapping entry, so this covers the
same emitter path as the bracketed Next.js routes by a different character.

The import below is what makes that reachable: a path only reaches a flow mapping as an
edge's `source:`, so a file with no imports contributes no edge and its name is never
emitted anywhere a comma could do damage. An external dependency rather than a local one,
because a local import resolves to a module in the same directory and collapses to a
self-edge the graph drops.
"""

import httpx

NOTES = "corpus fixture"
TIMEOUT = httpx.Timeout(5.0)
