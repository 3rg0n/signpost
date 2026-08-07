package extract

import (
	"sort"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func csFile(path, src string) discover.File {
	return discover.File{
		Path: path, Lang: discover.LangCSharp, Class: discover.ClassSource, Content: src,
	}
}

func extractCSharp(t *testing.T, path, src string) Facts {
	t.Helper()
	fa, err := CSharpExtractor{}.Extract(csFile(path, src))
	if err != nil {
		t.Fatalf("Extract(%s): %v", path, err)
	}
	fa.Normalize()
	return fa
}

// The scored corpus. Hand-labeled against real C#, covering what makes C# different from
// the Java it shares scope machinery with: two visibility defaults rather than one,
// properties as the main thing a type exposes, the file-scoped namespace that every
// template since C# 10 emits, and a positional record that declares a whole type with no
// body at all.
func csharpCorpus() []Fixture {
	return []Fixture{
		{
			File: csFile("src/Ordering.Api/OrderService.cs", `using System;
using System.Collections.Generic;
using System.Threading.Tasks;
using Ordering.Domain.Orders;
using Serilog;
using Json = System.Text.Json;
using static System.Math;

namespace Ordering.Api.Services;

/// <summary>
/// Serves order requests.
/// </summary>
/// <param name="repo">The store.</param>
public sealed class OrderService
{
    private readonly IOrderRepository _repo;
    private readonly ILogger _log;

    public const int MaxItems = 50;

    public OrderService(IOrderRepository repo, ILogger log)
    {
        _repo = repo;
        _log = log;
    }

    /// <summary>The service's name.</summary>
    public string Name { get; init; } = "orders";

    public int Count { get; private set; }

    public bool IsEmpty => Count == 0;

    internal string Diagnostics { get; set; }

    /// <summary>Looks an order up.</summary>
    public async Task<Order?> LookupAsync(Guid id)
    {
        var found = await _repo.FindAsync(id);
        if (found is null)
        {
            _log.Warning("missing {Id}", id);
        }
        return found;
    }

    public int Clamp(int value) => Max(0, Min(value, MaxItems));

    protected virtual void OnLoaded()
    {
    }

    private static int Round(double v)
    {
        return (int)v;
    }

    void Untagged()
    {
    }
}
`),
			Expected: Expected{
				Package: "Ordering.Api.Services",
				// `using static System.Math` names a type, so the dependency is on System —
				// System.Math is a name nothing declares as a namespace. The alias form carries
				// the namespace it aliases, not the alias.
				Imports: []string{
					"Ordering.Domain.Orders", "Serilog", "System",
					"System.Collections.Generic", "System.Text.Json", "System.Threading.Tasks",
				},
				Symbols: []string{
					"OrderService", "OrderService.Clamp", "OrderService.Count",
					"OrderService.Diagnostics", "OrderService.IsEmpty", "OrderService.LookupAsync",
					"OrderService.MaxItems", "OrderService.Name", "OrderService.OnLoaded",
					"OrderService.Round", "OrderService.Untagged",
				},
				// A member with no modifier is private, so `void Untagged()` is not surface —
				// the opposite of PHP's default and stricter than Java's. `internal` is
				// assembly-visible and not public, `protected` is subclass-visible and not
				// public, and `{ get; private set; }` is a public property whose setter is not.
				Exported: []string{
					"OrderService", "OrderService.Clamp", "OrderService.Count",
					"OrderService.IsEmpty", "OrderService.LookupAsync", "OrderService.MaxItems",
					"OrderService.Name",
				},
				Entrypoints: []string{},
			},
		},
		{
			// The declaration forms: a braced namespace (the pre-C#-10 spelling, still the
			// majority of existing code), an interface whose members are public with no
			// modifier, a struct, an enum, a delegate, a positional record with no body, a
			// record with a body, and a nested type inside an internal one.
			File: csFile("src/Ordering.Domain/Model.cs", `using System;

namespace Ordering.Domain
{
    public interface IOrderRepository
    {
        Task<Order?> FindAsync(Guid id);

        int Count { get; }
    }

    public readonly struct Money
    {
        public decimal Amount { get; }

        public string Currency { get; }
    }

    public enum Status
    {
        Draft,
        Placed,
    }

    public delegate void OrderPlaced(Order order);

    public record Line(string Sku, int Qty);

    public record Order(Guid Id)
    {
        public IReadOnlyList<Line> Lines { get; init; } = Array.Empty<Line>();

        public decimal Total() => Lines.Sum(l => l.Qty);
    }

    internal class Cache
    {
        public sealed class Entry
        {
            public string Key { get; set; }
        }

        public int Size { get; set; }
    }

    class Bare
    {
        public int Visible { get; set; }
    }
}
`),
			Expected: Expected{
				Package: "Ordering.Domain",
				Imports: []string{"System"},
				Symbols: []string{
					"Bare", "Bare.Visible", "Cache", "Cache.Size", "Entry", "Entry.Key",
					"IOrderRepository", "IOrderRepository.Count", "IOrderRepository.FindAsync",
					"Line", "Money", "Money.Amount", "Money.Currency", "Order", "Order.Lines",
					"Order.Total", "OrderPlaced", "Status",
				},
				// An interface's members are public with no modifier, which is the membersPublic
				// case. `internal class Cache` is not public surface, so neither is anything
				// inside it however it is marked — a nested public type is reachable only if
				// every type enclosing it is. `class Bare` with no modifier is internal too.
				Exported: []string{
					"IOrderRepository", "IOrderRepository.Count", "IOrderRepository.FindAsync",
					"Line", "Money", "Money.Amount", "Money.Currency", "Order", "Order.Lines",
					"Order.Total", "OrderPlaced", "Status",
				},
				Entrypoints: []string{},
			},
		},
		{
			// The adversarial fixture: declarations inside verbatim, interpolated and raw
			// string literals, `using var` disposal scopes that are not imports, a local
			// function inside a method body, a lambda, a class declared inside a method, a
			// `#region` directive, and an attribute carrying a brace.
			File: csFile("src/Tricky.cs", `using System;
using Real.Thing;

namespace App;

public class Tricky
{
    private const string Snippet = "public class QuotedGhost { public void Phantom() {} }";

    private const string Verbatim = @"using Ghost.Namespace;
public class VerbatimGhost {
    public void Spectral() {}
}";

    private static readonly string Raw = """
        using Raw.Ghost;
        public class RawGhost { }
        """;

    #region Real work

    /// <summary>Does the real work. See <see cref="T:App.Tricky"/>.</summary>
    public int Real(IEnumerable<int> items)
    {
        using var reader = new StreamReader("x");
        using (var conn = Open())
        {
            conn.Go();
        }

        int Local(int x) => x + 1;

        var lambda = (int x) => x * 2;

        Func<int, int> other = delegate (int x) { return x; };

        class LocalOnly
        {
            public void Inside() {}
        }

        var anon = new { Name = "x", Count = 1 };

        return items.Select(Local).Sum();
    }

    #endregion

    public string Interpolated(string name) => $"class {name}Ghost {{ }}";

    private int Hidden() => 0;
}
`),
			Expected: Expected{
				Package: "App",
				Imports: []string{"Real.Thing", "System"},
				// The local function, the lambda, the anonymous-object initialiser and the
				// method-local class all sit inside a method body, which is not a declaration
				// site. Nothing in any of the three string literals is code.
				//
				// Snippet and Verbatim are `const`, which is a member; Raw is `static readonly`,
				// which is a field and is not. The difference is not stylistic — a const is
				// compiled into every caller, so changing one is a breaking change and a
				// readonly field's value is read at run time.
				Symbols: []string{
					"Tricky", "Tricky.Hidden", "Tricky.Interpolated", "Tricky.Real",
					"Tricky.Snippet", "Tricky.Verbatim",
				},
				Exported:    []string{"Tricky", "Tricky.Interpolated", "Tricky.Real"},
				Entrypoints: []string{},
			},
		},
		{
			// A console entrypoint: the classic `static void Main` form, top-level statements
			// having replaced it in templates but not in existing code, plus a global using
			// and a namespace-level using inside the braced form.
			File: csFile("src/Ordering.Cli/Program.cs", `global using System;

using System.CommandLine;
using Ordering.Api.Services;

namespace Ordering.Cli;

public static class Program
{
    public static int Main(string[] args)
    {
        var svc = new OrderService(null, null);
        return svc.Clamp(args.Length);
    }

    private static void Configure(RootCommand cmd)
    {
    }
}
`),
			Expected: Expected{
				Package: "Ordering.Cli",
				Imports: []string{"Ordering.Api.Services", "System", "System.CommandLine"},
				Symbols: []string{
					"Program", "Program.Configure", "Program.Main",
				},
				Exported:    []string{"Program", "Program.Main"},
				Entrypoints: []string{"Main"},
			},
		},
	}
}

// The measurement design §4.2 promises for C#.
func TestCSharpExtractorMeetsTarget(t *testing.T) {
	ls := ScoreExtractor(CSharpExtractor{}, discover.LangCSharp, csharpCorpus())
	if !ls.MeetsTarget() {
		t.Errorf("C# extractor below target:\n%s", ls.Report())
	}
	t.Logf("C# extractor score:\n%s", ls.Report())
}

// C# is the only language here with *two* visibility defaults: an unmodified type is
// `internal` and an unmodified member is `private`. Every other language in this package
// has one rule for both, so a regression that shares one — Java's package-private, PHP's
// public, Kotlin's public — flips exactly one of these two assertions and leaves the other
// passing. Both are stated against the same file for that reason.
func TestCSharpHasTwoVisibilityDefaults(t *testing.T) {
	fa := extractCSharp(t, "A.cs", `namespace App;

class InternalByDefault
{
    public int Reachable { get; set; }
}

public class PublicType
{
    int privateByDefault;

    void PrivateMethod() {}

    int PrivateProp { get; set; }

    public void PublicMethod() {}
}
`)
	// The type default: internal is assembly-visible, which is not public surface — and
	// nothing inside an unreachable type is reachable either, however it is marked.
	got := exportedNames(fa)
	sort.Strings(got)
	if j := strings.Join(got, ","); j != "PublicType,PublicType.PublicMethod" {
		t.Errorf("exported = %q; an unmodified type is internal and an unmodified member is private", j)
	}
	// The members are still recorded — they are real declarations, just not surface. A rule
	// that dropped them would report a class with one method. The plain field is the one
	// thing genuinely absent, because C# convention keeps fields private.
	want := "InternalByDefault,InternalByDefault.Reachable,PublicType," +
		"PublicType.PrivateMethod,PublicType.PrivateProp,PublicType.PublicMethod"
	if j := strings.Join(fa.SymbolNames(), ","); j != want {
		t.Errorf("symbols = %q, want %q", j, want)
	}
}

// The file-scoped namespace is the default every template has emitted since C# 10, and the
// braced form is the majority of code written before it. Reading the first as the second
// puts every declaration in the file one brace shallower than the scope stack expects,
// which drops the whole file.
func TestCSharpBothNamespaceFormsDeclareTheSameMembers(t *testing.T) {
	scoped := extractCSharp(t, "A.cs", `namespace App.Domain;

public class A
{
    public void M() {}
}
`)
	braced := extractCSharp(t, "B.cs", `namespace App.Domain
{
    public class A
    {
        public void M() {}
    }
}
`)
	if scoped.Package != "App.Domain" || braced.Package != "App.Domain" {
		t.Errorf("packages = %q / %q", scoped.Package, braced.Package)
	}
	if a, b := strings.Join(scoped.SymbolNames(), ","), strings.Join(braced.SymbolNames(), ","); a != b {
		t.Errorf("file-scoped = %q, braced = %q; the two forms declare the same members", a, b)
	} else if a != "A,A.M" {
		t.Errorf("symbols = %q", a)
	}
}

// A property is what a C# type mostly exposes, and it has no Java analogue. A reader that
// skipped them would report every DTO, record and options class in a .NET codebase as
// having no members at all. Both spellings are the same fact; a field is not.
func TestCSharpPropertiesAreMembersAndFieldsAreNot(t *testing.T) {
	fa := extractCSharp(t, "A.cs", `public class Dto
{
    public string Name { get; set; }
    public int Count { get; init; }
    public bool Ready { get; }
    public decimal Total => Count * 2;
    public string Tagged { get; private set; }

    private readonly ILogger _log;
    public static int Shared = 0;
}
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "Dto,Dto.Count,Dto.Name,Dto.Ready,Dto.Tagged,Dto.Total" {
		t.Errorf("symbols = %q; a property is a member and a field is not", got)
	}
}

// `using` means two unrelated things. At file level it imports a namespace; inside a method
// body `using var f = ...` is a disposal scope naming a local variable. Reading the second
// as the first records a dependency on a namespace named after a local.
func TestCSharpUsingStatementIsNotAnImport(t *testing.T) {
	fa := extractCSharp(t, "A.cs", `using System.IO;

namespace App;

public class A
{
    public void M()
    {
        using var reader = new StreamReader("x");
        using (var conn = Open())
        {
        }
    }
}
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "System.IO" {
		t.Errorf("imports = %q; only the file-level directive is an import", got)
	}
}

// `using static` names a *type*, so the namespace is everything before its last segment.
// Recording System.Math would point at a node no file declares as a namespace, which is
// the same rule javaImport states for a static import.
func TestCSharpUsingStaticRecordsTheNamespace(t *testing.T) {
	fa := extractCSharp(t, "A.cs", `using static System.Math;
using static Ordering.Domain.Constants;
using Json = System.Text.Json;
global using System;
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "Ordering.Domain,System,System.Text.Json" {
		t.Errorf("imports = %q", got)
	}
	// The alias is recorded so a reference written through it can still be traced, but the
	// namespace — not the alias — is the dependency.
	for _, im := range fa.Imports {
		if im.Raw == "System.Text.Json" && im.Alias != "Json" {
			t.Errorf("alias = %q, want Json", im.Alias)
		}
	}
}

// A positional record declares a whole type on one line with no body. A scope pushed for
// it never closes, and an unclosed scope claims every declaration in the rest of the file
// as its own member — a silent, total misattribution rather than a visible error.
func TestCSharpPositionalRecordOpensNoScope(t *testing.T) {
	fa := extractCSharp(t, "A.cs", `namespace App;

public record Money(decimal Amount, string Currency);

public record struct Point(int X, int Y);

public class After
{
    public void Reached() {}
}
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "After,After.Reached,Money,Point" {
		t.Errorf("symbols = %q; a bodyless record must not claim what follows it", got)
	}
}

// An interface's members are public by definition and carry no modifier saying so. Applying
// the class rule to an interface would report every contract in a codebase as having no
// public members — the exact inverse of the truth.
func TestCSharpInterfaceMembersArePublicWithoutAModifier(t *testing.T) {
	iface := extractCSharp(t, "A.cs", `public interface IContract
{
    int Id { get; }

    void Run();
}
`)
	if got := strings.Join(exportedNames(iface), ","); got != "IContract,IContract.Id,IContract.Run" {
		t.Errorf("interface exported = %q", got)
	}
	// The same declarations in a class are private, which is what makes the interface rule a
	// rule rather than an accident of the modifier check.
	cls := extractCSharp(t, "B.cs", `public class Concrete
{
    int Id { get; }

    void Run();
}
`)
	if got := strings.Join(exportedNames(cls), ","); got != "Concrete" {
		t.Errorf("class exported = %q; the same members in a class are private", got)
	}
}

// `Main` is the entrypoint only as a static method. An instance method named Main is an
// ordinary method, and listing it would put a type in the set of things you can start.
func TestCSharpMainIsAnEntrypointOnlyWhenStatic(t *testing.T) {
	fa := extractCSharp(t, "Program.cs", `public static class Program
{
    public static int Main(string[] args) => 0;
}
`)
	if got := strings.Join(fa.Entrypoints, ","); got != "Main" {
		t.Errorf("entrypoints = %q", got)
	}
	other := extractCSharp(t, "A.cs", `public class Widget
{
    public void Main() {}
}
`)
	if len(other.Entrypoints) != 0 {
		t.Errorf("entrypoints = %v, want none for an instance Main", other.Entrypoints)
	}
}

// A verbatim string escapes a quote by doubling it, and a raw string ends at its own fence.
// Both hold text that looks exactly like source, which is how a phantom class or a phantom
// namespace dependency gets into a graph.
func TestCSharpStringLiteralsDeclareNothing(t *testing.T) {
	fa := extractCSharp(t, "A.cs", `using Real.Only;

public class Real
{
    private const string V = @"using Ghost.One;
public class VerbatimGhost { }
He said ""using Ghost.Two;"" and stopped.";

    private const string R = """
        using Ghost.Three;
        public class RawGhost { }
        """;

    public void Method() {}
}
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "Real.Only" {
		t.Errorf("imports = %q; a literal declares no dependency", got)
	}
	if got := strings.Join(fa.SymbolNames(), ","); got != "Real,Real.Method,Real.R,Real.V" {
		t.Errorf("symbols = %q", got)
	}
}

// A local function and a method-local class are both legal and both reachable from nothing.
// Recording either would put a symbol on the page that no caller outside the method can
// name, and a scope pushed for the class would misattribute the members after it.
func TestCSharpMethodLocalDeclarationsAreNotSurface(t *testing.T) {
	fa := extractCSharp(t, "A.cs", `public class Outer
{
    public int Run()
    {
        int Local(int x) => x + 1;

        class Helper
        {
            public void Deep() {}
        }

        return Local(1);
    }

    public void After() {}
}
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "Outer,Outer.After,Outer.Run" {
		t.Errorf("symbols = %q; a method-local declaration is not surface", got)
	}
}

// An XML doc comment's tags are markup, and an unstripped `<summary>` on a bundle page
// reads as a bug in the extracted prose. A cref carries the only noun in some sentences,
// so it is substituted rather than dropped with its tag.
func TestCSharpXMLDocIsStrippedToProse(t *testing.T) {
	fa := extractCSharp(t, "A.cs", `public class A
{
    /// <summary>
    /// Places an <see cref="T:App.Order"/>. Returns nothing.
    /// </summary>
    /// <param name="id">The identifier.</param>
    /// <returns>Nothing at all.</returns>
    public void Place(Guid id) {}

    /// Terse and untagged.
    public void Terse() {}
}
`)
	docs := map[string]string{}
	for _, s := range fa.Symbols {
		docs[s.Name] = s.Doc
	}
	if got := docs["Place"]; got != "Places an Order." {
		t.Errorf("Place doc = %q; the summary is the sentence and the tags are markup", got)
	}
	// A run of `///` with no elements at all is prose, which is how a one-line comment is
	// written and is the majority of them.
	if got := docs["Terse"]; got != "Terse and untagged." {
		t.Errorf("Terse doc = %q", got)
	}
}

// A preprocessor directive is not a declaration and opens no brace. `#region` is
// ubiquitous in existing C# and `#if DEBUG` wraps real code; counting either would move a
// depth that tracks type bodies.
func TestCSharpPreprocessorDirectivesAreNotCode(t *testing.T) {
	fa := extractCSharp(t, "A.cs", `public class A
{
    #region Public API

    public void First() {}

    #endregion

#if DEBUG
    public void DebugOnly() {}
#endif

    public void Last() {}
}
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "A,A.DebugOnly,A.First,A.Last" {
		t.Errorf("symbols = %q", got)
	}
}
