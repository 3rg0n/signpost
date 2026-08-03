"""Beta's handler. Corpus fixture.

Byte-for-byte the same import as alpha's handler, resolving to a different file. That is the
whole of the negative boundary: the specifier cannot distinguish the two, so only the scope
of the root can, and a repo-wide root list would send both here or both to alpha.
"""

from api.client import fetch

# Unix-only stdlib, the mirror of alpha's `winreg`. Both spellings appear in one tree so the
# list cannot be completed for one platform and left short for the other.
try:
    import fcntl
except ImportError:  # pragma: no cover
    fcntl = None


def handle(url: str) -> str:
    """Handles a request for ``url``."""
    del fcntl
    return fetch(url)
