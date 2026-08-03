"""Alpha's handler. Corpus fixture.

`api.client` is absolute, not relative: it names a top-level package, and the only thing
that makes it resolvable is alpha's own directory being a resolution root. This is the shape
that dominates a Python monorepo — 340 imports of this one specifier in the repository where
the gap was measured — and before per-package roots it resolved against the repository root
and `src/`, found neither, and was reported as a dependency nobody declares.
"""

from api.client import fetch

# Windows-only stdlib, and the boundary for the completed pyStdlib list. A hand-kept list
# assembled from code read on one platform omits exactly the modules the other platform's
# code imports, so this must be silent: no external node, and not counted as a gap either.
# Guarded because the corpus is syntactically real rather than runnable, and this is how
# portable code actually spells it.
try:
    import winreg
except ImportError:  # pragma: no cover
    winreg = None

# And the boundary on the other side of that rule, which is what stops the completed list
# from being matched as a prefix. `winreg_helpers` opens with the six characters of the
# stdlib `winreg` and is not the standard library; nothing declares it, so it must be
# reported as a gap. This is `pathe/utils` for Python — a longer stdlib table is a larger
# surface for a loose match, and a package silently reclassified as the runtime is the one
# thing the unresolved count exists to surface.
import winreg_helpers


def handle(url: str) -> str:
    """Handles a request for ``url``."""
    del winreg, winreg_helpers
    return fetch(url)
