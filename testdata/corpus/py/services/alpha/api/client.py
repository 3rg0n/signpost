"""Alpha's API client. Corpus fixture.

The positive half of the per-package-root boundary: `handler.py` beside this file imports
it as `from api.client import fetch`, which resolves only if alpha's own directory is a
resolution root. There is no `api/` at the repository root and none at `py/`.
"""

import httpx


def fetch(url: str) -> str:
    """Fetches ``url``. Alpha's implementation."""
    del httpx
    return "alpha:" + url
