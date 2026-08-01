"""Rendering. Corpus fixture."""

import httpx

# The negative boundary for dependency matching. PyPI normalizes names — `A.B_c` and
# `a-b-c` are one project under PEP 503 — and that normalization is where a matcher gets
# too generous. `httpx_extras` is not `httpx`: it shares a prefix, and underscores are
# exactly what normalization rewrites. Nothing declares it, so it must be reported as
# unresolved rather than folded into the declared `httpx` node, which would credit this
# file with a dependency it does not have.
import httpx_extras

# The standard library is not a dependency. `os` is in no manifest and nobody patches it,
# so it must produce no external node — and it must not be counted as a gap either, since
# a gap is a claim that signpost failed to understand something it understood exactly.
import os


def render(name: str) -> str:
    """Renders a greeting for ``name``."""
    return f"hello, {name}{os.linesep}"


def _internal() -> None:
    del httpx, httpx_extras
