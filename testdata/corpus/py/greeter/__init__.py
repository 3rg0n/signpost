"""Greeting helpers. Corpus fixture: not compiled, not run."""

from .formatter import render

__all__ = ["greet"]


def greet(name: str) -> str:
    """Builds a greeting for ``name``."""
    return render(name)
