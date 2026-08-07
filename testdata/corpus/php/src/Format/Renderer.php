<?php

// Corpus fixture: not installed, not run.

declare(strict_types=1);

// The namespace one segment deeper than the PSR-4 prefix, which is what makes the `use` in
// Greeter.php resolve to this directory rather than to the one holding the prefix. A map that
// matched the prefix and stopped there would draw every first-party edge onto `src/`, and the
// repository's own internal structure — the thing a reader opens the graph for — would be one
// node wide.
namespace Corpus\Format;

/** Renders a greeting. */
final class Renderer
{
    private const SEPARATOR = ', ';

    /** Returns the greeting for a name. */
    public function render(string $name): string
    {
        return 'Hello' . self::SEPARATOR . $name;
    }

    /** A trait composed into a class is `use` inside a class body, not an import. */
    protected function separator(): string
    {
        return self::SEPARATOR;
    }
}
