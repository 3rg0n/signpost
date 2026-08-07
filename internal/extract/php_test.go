package extract

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func phpFile(path, src string) discover.File {
	return discover.File{
		Path: path, Lang: discover.LangPHP, Class: discover.ClassSource, Content: src,
	}
}

func extractPHP(t *testing.T, path, src string) Facts {
	t.Helper()
	fa, err := PHPExtractor{}.Extract(phpFile(path, src))
	if err != nil {
		t.Fatalf("Extract(%s): %v", path, err)
	}
	fa.Normalize()
	return fa
}

// The scored corpus. Hand-labeled against real PHP, including the forms that are PHP's own:
// the four `use` spellings, a trait `use` inside a class body that is not an import at all,
// a PHP 8 constructor-promoted property, an attribute, an enum with methods, and a template
// file whose markup surrounds the code.
func phpCorpus() []Fixture {
	return []Fixture{
		{
			File: phpFile("src/Domain/OrderService.php", `<?php

declare(strict_types=1);

namespace App\Domain;

use App\Store\Repository;
use App\Store\Entity as StoredEntity;
use App\Support\{Clock, Money};
use function App\Support\slugify;
use Psr\Log\LoggerInterface;
use Throwable;

/**
 * Serves order requests.
 */
final class OrderService
{
    public const MAX_ITEMS = 50;
    private const CACHE_KEY = 'orders';

    use Loggable;

    public function __construct(
        private readonly Repository $repo,
        private LoggerInterface $log,
    ) {
    }

    /** Looks an order up. */
    public function lookup(string $key): ?StoredEntity
    {
        return $this->repo->find($key);
    }

    public function slug(string $name): string
    {
        return slugify($name);
    }

    protected function guarded(): void
    {
    }

    private function helper(int $a, int $b): int
    {
        return $a > $b ? $a : $b;
    }

    function noModifier(): void
    {
    }
}
`),
			Expected: Expected{
				Package: `App\Domain`,
				// The namespace is what is recorded, never the class: a PSR-4 map declares a
				// namespace prefix, so `App\Domain\Order` would point at a node no autoload
				// entry can ever claim. `use Throwable` is a single segment naming a
				// global-namespace class and declares no namespace dependency at all.
				Imports: []string{`App\Store`, `App\Support`, `Psr\Log`},
				Symbols: []string{
					"OrderService", "OrderService.CACHE_KEY", "OrderService.MAX_ITEMS",
					"OrderService.__construct", "OrderService.guarded", "OrderService.helper",
					"OrderService.lookup", "OrderService.noModifier", "OrderService.slug",
				},
				// Public is the default, so `function noModifier()` is surface — the inverse
				// of Java's rule. What is missing is what said otherwise: the private const,
				// `protected`, and `private`.
				Exported: []string{
					"OrderService", "OrderService.MAX_ITEMS", "OrderService.__construct",
					"OrderService.lookup", "OrderService.noModifier", "OrderService.slug",
				},
				Entrypoints: []string{},
			},
		},
		{
			// The declaration forms: interface, trait, abstract class, enum with a method,
			// a PHP 8 attribute above a declaration, and free functions and constants,
			// which the JVM has no equivalent of.
			File: phpFile("src/Domain/Model.php", `<?php

namespace App\Domain;

const DEFAULT_CURRENCY = 'USD';

interface Contract
{
    public function name(): string;

    public function id(): int;
}

trait Loggable
{
    public function logLine(string $m): void
    {
    }

    private function prefix(): string
    {
        return static::class;
    }
}

abstract class Base implements Contract
{
    abstract public function name(): string;

    public function id(): int
    {
        return 0;
    }
}

enum Mode: string
{
    case Fast = 'fast';
    case Slow = 'slow';

    public function label(): string
    {
        return ucfirst($this->value);
    }
}

#[Attribute]
final class Marker
{
}

function slugify(string $s): string
{
    return strtolower($s);
}
`),
			Expected: Expected{
				Package: `App\Domain`,
				Imports: []string{},
				Symbols: []string{
					"Base", "Base.id", "Base.name", "Contract", "Contract.id", "Contract.name",
					"DEFAULT_CURRENCY", "Loggable", "Loggable.logLine", "Loggable.prefix",
					"Marker", "Mode", "Mode.label", "slugify",
				},
				Exported: []string{
					"Base", "Base.id", "Base.name", "Contract", "Contract.id", "Contract.name",
					"DEFAULT_CURRENCY", "Loggable", "Loggable.logLine", "Marker", "Mode",
					"Mode.label", "slugify",
				},
				Entrypoints: []string{},
			},
		},
		{
			// The adversarial fixture: declarations inside single- and double-quoted
			// strings, a heredoc and a nowdoc, a `#` line comment (which PHP has and Java
			// does not), a closure and an arrow function that use the `function` keyword
			// without declaring a name, and an anonymous class.
			File: phpFile("src/Tricky.php", `<?php

namespace App;

use Real\Thing\Used;

class Tricky
{
    private string $snippet = 'use Fake\Thing\Ghost; class QuotedGhost {}';

    private string $block = <<<SQL
        namespace Nowhere;
        use Nowhere\Nothing;
        class HeredocGhost {
            public function ghostly() {}
        }
        SQL;

    private string $literal = <<<'RAW'
        class NowdocGhost {}
        RAW;

    # use Commented\Out;
    // class LineCommentGhost {}
    /* class BlockGhost {} */

    public function real(array $items): int
    {
        $fn = function (int $x): int {
            return $x + 1;
        };
        $arrow = fn (int $x): int => $x * 2;
        $anon = new class implements Used {
            public function inside(): void
            {
            }
        };
        $brace = '{';
        if (count($items) === 0) {
            return 0;
        }
        return $fn(1) + $arrow(2);
    }

    private function compute(int $x): int
    {
        return $x;
    }
}
`),
			Expected: Expected{
				Package: "App",
				Imports: []string{`Real\Thing`},
				// The closure, the arrow function and the anonymous class declare no name, so
				// no symbol. `inside` is a method of the anonymous class, which is not a
				// declaration site this extractor records.
				Symbols: []string{
					"Tricky", "Tricky.compute", "Tricky.real",
				},
				Exported:    []string{"Tricky", "Tricky.real"},
				Entrypoints: []string{},
			},
		},
		{
			// A template file: markup outside the script tags, code inside them, and the
			// pre-autoloader `require` that a project's entrypoint still uses. Code exists
			// only between `<?php` and `?>`, so an extractor that read the whole file would
			// find declarations in the HTML.
			File: phpFile("public/index.php", `<!DOCTYPE html>
<html>
<head><title>class MarkupGhost {}</title></head>
<body>
<?php

require __DIR__ . '/../vendor/autoload.php';
require_once 'bootstrap.php';

use App\Domain\OrderService;

function render(string $name): string
{
    return htmlspecialchars($name);
}

?>
<p>use Markup\Ghost;</p>
<?php echo render('x'); ?>
</body>
</html>
`),
			Expected: Expected{
				Imports: []string{
					"./../vendor/autoload.php", "App\\Domain", "bootstrap.php",
				},
				Symbols:     []string{"render"},
				Exported:    []string{"render"},
				Entrypoints: []string{},
			},
		},
	}
}

// The measurement design §4.2 promises for PHP.
func TestPHPExtractorMeetsTarget(t *testing.T) {
	ls := ScoreExtractor(PHPExtractor{}, discover.LangPHP, phpCorpus())
	if !ls.MeetsTarget() {
		t.Errorf("PHP extractor below target:\n%s", ls.Report())
	}
	t.Logf("PHP extractor score:\n%s", ls.Report())
}

// PHP's default is public and Java's is package-private, so one shared rule would invert
// the public surface of every file in whichever language lost. Both directions are asserted
// against the same declarations, because a regression that copies one language's rule onto
// the other passes each language's own corpus in only one direction.
func TestPHPDefaultVisibilityIsPublicAndJavaIsNot(t *testing.T) {
	php := extractPHP(t, "A.php", `<?php
class Bare
{
    function bare() {}
    private function hidden() {}
    protected function guarded() {}
}
`)
	if got := strings.Join(exportedNames(php), ","); got != "Bare,Bare.bare" {
		t.Errorf("PHP exported = %q; an unmodified method is public", got)
	}

	jv := extractJava(t, "A.java", `
package p;

class Bare {
    void bare() {}
    private void hidden() {}
    protected void guarded() {}
}
`)
	if got := strings.Join(exportedNames(jv), ","); got != "" {
		t.Errorf("Java exported = %q; an unmodified declaration is package-private", got)
	}
}

// `use` means two unrelated things depending on where it sits: at file level it imports a
// namespace, and inside a class body it composes a trait into that class. Reading the
// second as an import would record a dependency on a namespace nothing declares — and the
// trait is in the same file more often than not.
func TestPHPTraitUseIsNotAnImport(t *testing.T) {
	fa := extractPHP(t, "A.php", `<?php
namespace App;

use App\Store\Repository;

class Service
{
    use Loggable;
    use Cacheable, Timeable;
}
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != `App\Store` {
		t.Errorf("imports = %q; only the file-level use is an import", got)
	}
}

// What resolution can match is a PSR-4 prefix, which is a *namespace*. Recording the class
// would point at a node no autoload entry will ever claim, and would split two imports from
// one namespace into two dependencies that look unrelated.
func TestPHPUseRecordsTheNamespaceNotTheClass(t *testing.T) {
	fa := extractPHP(t, "A.php", `<?php
namespace App;

use App\Domain\Order;
use App\Domain\Invoice;
use App\Support\{Clock, Money};
use function App\Support\slugify;
use const App\Config\MAX;
use Throwable;
use \DateTimeImmutable;
`)
	// Two classes from one namespace are one dependency, and `use function` and `use const`
	// name a symbol in a namespace rather than a namespace of their own.
	if got := strings.Join(fa.ImportPaths(), ","); got != `App\Config,App\Domain,App\Support` {
		t.Errorf("imports = %q", got)
	}
	// A single-segment use names a global-namespace class. There is no namespace to depend
	// on, and recording the class as one would invent a node.
	for _, p := range fa.ImportPaths() {
		if p == "Throwable" || p == "DateTimeImmutable" {
			t.Errorf("%q is a global-namespace class, not a namespace", p)
		}
	}
}

// The group form is the only `use` that yields several imports, and each element may carry
// its own alias. A reader that took the prefix alone would miss which symbols came in.
func TestPHPGroupUseExpandsToEachName(t *testing.T) {
	fa := extractPHP(t, "A.php", `<?php
use App\Domain\{Order, Invoice as Bill, Nested\Line};
`)
	var seen []string
	for _, im := range fa.Imports {
		seen = append(seen, im.Raw+":"+strings.Join(im.Names, "|")+":"+im.Alias)
	}
	want := `App\Domain:Order:,App\Domain:Invoice:Bill,App\Domain\Nested:Line:`
	if got := strings.Join(seen, ","); got != want {
		t.Errorf("group use = %q, want %q", got, want)
	}
}

// The pre-autoloader form is a real dependency on a real file, and it is what a project's
// entrypoint uses. `__DIR__ . '/x'` is relative to this file, and the literal alone begins
// with a slash — which would read as absolute and resolve nowhere.
func TestPHPRequireKeepsItsRelativeMarker(t *testing.T) {
	fa := extractPHP(t, "public/index.php", `<?php
require __DIR__ . '/../vendor/autoload.php';
require_once 'bootstrap.php';
include './partial.php';
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "./../vendor/autoload.php,./partial.php,bootstrap.php" {
		t.Errorf("imports = %q", got)
	}
}

// A closure and an arrow function both spell `function` without declaring a name. Reading
// either as a declaration would invent a symbol named after whatever token followed.
func TestPHPClosuresDeclareNoSymbol(t *testing.T) {
	fa := extractPHP(t, "A.php", `<?php
class A
{
    public function real(): callable
    {
        $fn = function (int $x) { return $x; };
        $arrow = fn ($x) => $x;
        return $fn;
    }
}
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "A,A.real" {
		t.Errorf("symbols = %q", got)
	}
}

// Code exists only between `<?php` and `?>`. A template is mostly markup, and an extractor
// reading the whole file would find declarations in the HTML — a phantom class on the page
// for text a browser renders.
func TestPHPMarkupOutsideScriptTagsIsNotCode(t *testing.T) {
	fa := extractPHP(t, "view.php", `<h1>class HeadingGhost {}</h1>
<?php
function real(): string { return 'x'; }
?>
<p>use Markup\Ghost; class TailGhost {}</p>
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "real" {
		t.Errorf("symbols = %q; only the code between the tags is source", got)
	}
	if got := strings.Join(fa.ImportPaths(), ","); got != "" {
		t.Errorf("imports = %q; markup declares nothing", got)
	}
}

// An anonymous class declares no name. Pushing a scope for one would attribute its methods
// to a type that does not exist, and recording a symbol for it would invent one.
func TestPHPAnonymousClassDeclaresNothing(t *testing.T) {
	fa := extractPHP(t, "A.php", `<?php
class Factory
{
    public function make(): object
    {
        return new class {
            public function inside(): void {}
        };
    }

    public function after(): void {}
}
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "Factory,Factory.after,Factory.make" {
		t.Errorf("symbols = %q", got)
	}
}

// A namespace is separated by a backslash, which is also PHP's string escape. A namespace
// read out of a double-quoted literal would be mangled, which is why every name here comes
// from the code rather than a literal.
func TestPHPBackslashInStringsDoesNotAffectNames(t *testing.T) {
	fa := extractPHP(t, "A.php", `<?php
namespace App\Domain\Sub;

use App\Store\Repository;

class A
{
    private string $pattern = "App\\Fake\\Ghost";
}
`)
	if fa.Package != `App\Domain\Sub` {
		t.Errorf("package = %q", fa.Package)
	}
	if got := strings.Join(fa.ImportPaths(), ","); got != `App\Store` {
		t.Errorf("imports = %q", got)
	}
}
