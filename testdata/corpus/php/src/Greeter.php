<?php

// Corpus fixture: not installed, not run.

declare(strict_types=1);

namespace Corpus;

// The first-party import, and the whole reason composer.json has to be read. Nothing in this
// file says `src`: only `"Corpus\\": "src/"` in the autoload block makes `Corpus\Format`
// mean `src/Format`, and the same project could map it onto `lib/` with this line unchanged.
use Corpus\Format\Renderer;

// The declared vendor package, whose name — `monolog/monolog` — is not derivable from the
// namespace. Both spellings of the lookup are exercised: the root namespace alone, and a
// namespace one segment deeper that has to fall back to the same package.
use Monolog\Logger;
use Monolog\Handler\StreamHandler;

// A single-segment use names a class in the global namespace and declares a dependency on no
// namespace at all. PHP has no standard-library import list — an unresolved PHP import is a
// real gap every time — so a reader that recorded `Throwable` as a namespace would report the
// language's own base class as a package nobody declares, on every file that catches an error.
use Throwable;

// The first near-miss, on the boundary the PSR-4 map draws. `CorpusKernel` opens with the six
// characters of the prefix this file's own namespace matches, and a backslash is what
// separates a namespace from the one it sits under — so a prefix test written against the
// string rather than against the delimiter routes this into `src/` and draws an edge to a
// directory holding no such file.
use CorpusKernel\Boot\Loader;

// The second, on the dependency boundary rather than the namespace one. `MonologExtras` is
// not `monolog/monolog`, and a candidate list matching a declared package by prefix folds it
// into the one above — reporting a handler this code imports from a package that does not
// contain it.
use MonologExtras\Handler\FileHandler;

/** Greets by name. */
final class Greeter
{
    private Renderer $renderer;

    public function __construct(Renderer $renderer)
    {
        $this->renderer = $renderer;
    }

    /** Returns the greeting for a name. */
    public function greet(string $name): string
    {
        return $this->renderer->render($name);
    }

    /** A method with no modifier, which in PHP is public — the inverse of Java's rule. */
    function describe(): string
    {
        return 'greeter';
    }

    private function log(Logger $logger, StreamHandler $handler): void
    {
        $logger->pushHandler($handler);
    }
}
