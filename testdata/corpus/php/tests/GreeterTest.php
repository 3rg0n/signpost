<?php

// Corpus fixture: not installed, not run.

declare(strict_types=1);

// The autoload-dev prefix, which is a second PSR-4 entry nested under the first. Longest
// prefix wins, so a file here belongs to `tests/` and not to `src/` — a map consulted in
// declaration order instead would route this namespace into the production tree.
namespace Corpus\Tests;

// The declared dev package, and the reason its name is worth resolving twice over: the
// namespace is `PHPUnit\Framework` and the package is `phpunit/phpunit`, which no translation
// of the namespace produces. Only the `Vendor\Vendor` candidate reaches it.
use PHPUnit\Framework\TestCase;

// The subject. PHP has no arm in addTestEdges and it should not: a PHPUnit class declares
// `Corpus\Tests`, a namespace of its own that resolves to this very directory, so a
// declaration-derived edge would be the self-edge the graph drops. A PHP test names what it
// tests with a `use`, so the `tested_by` edge comes from this line.
use Corpus\Greeter;

final class GreeterTest extends TestCase
{
    public function testGreets(): void
    {
        $this->assertSame('greeter', (new Greeter(new \Corpus\Format\Renderer()))->describe());
    }
}
