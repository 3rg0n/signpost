"""Beta's API client, with the same module path as alpha's. Corpus fixture.

The negative half: two packages, each with `api/client.py`, and `from api.client import
fetch` written in both. A resolution root that governed the whole repository instead of the
files beneath it would send one package's import here and the other's to alpha — an edge
between two packages that cannot see each other, which is worse than the gap it replaces
because nothing in the bundle reports it as a guess.
"""

import httpx


def fetch(url: str) -> str:
    """Fetches ``url``. Beta's implementation, which alpha must never reach."""
    del httpx
    return "beta:" + url
