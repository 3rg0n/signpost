"""Rendering. Corpus fixture."""

import httpx


def render(name: str) -> str:
    """Renders a greeting for ``name``."""
    return f"hello, {name}"


def _internal() -> None:
    del httpx
