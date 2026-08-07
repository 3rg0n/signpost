// Corpus fixture: not restored, not built.

// The runtime, and it must not become a reference page: `System.*` arrives with the SDK and
// is versioned with it, so a supply-chain entry for it is an entry for the toolchain.
using System.Text.Json;

// The other half of the .NET runtime rule, and the reason it is a rule rather than a prefix.
// `Microsoft.Win32` ships with the SDK; `Microsoft.Extensions.Caching.Memory` below does not.
// They share their first segment, so a check that accepted `Microsoft.*` as the platform
// would hide a NuGet package somebody has to upgrade behind the word "runtime".
using Microsoft.Win32;

// The declared package. `Microsoft.Extensions.Logging` is a PackageReference in the csproj,
// which is the only place its version is stated, so it must reach a reference page.
using Microsoft.Extensions.Logging;

// The first-party namespace, matched against what another file in this repository declares.
// C# has no manifest naming its own namespaces — `using Corpus.Domain` looks identical
// whether Corpus.Domain is a project in this tree or a package from nuget.org — so the
// declaration in Greeting.cs is what makes this an internal edge rather than a dependency.
using Corpus.Domain;

// The first near-miss, on the boundary between those two readings. `Corpus.DomainModel` opens
// with the thirteen characters of the namespace declared above and is not under it: a
// namespace nests on the dot. A prefix test done on the string routes this onto the Domain
// project and draws an edge to code that does not exist, and the undeclared name it really is
// disappears from the gap report.
using Corpus.DomainModel;

// The second, on the runtime boundary rather than the namespace one. Nothing in this
// repository declares `Microsoft.Extensions.Caching.Memory` and the SDK does not ship it, so
// it is a gap — and it sits one segment away from `Microsoft.Win32` above, which is the
// platform, and one segment away from `Microsoft.Extensions.Logging`, which is declared.
using Microsoft.Extensions.Caching.Memory;

// The braced namespace form, which is what C# written before C# 10 looks like and still the
// majority of existing code. It puts every declaration in the file at brace depth 1, where
// the file-scoped form in Greeting.cs puts them at 0 — read as the same thing, one of the two
// files loses every type, member and import it declares.
namespace Corpus.Api
{
    /// <summary>Greets by name.</summary>
    public sealed class Greeter
    {
        private readonly ILogger<Greeter> _log;
        private readonly IMemoryCache _cache;

        public Greeter(ILogger<Greeter> log, IMemoryCache cache)
        {
            _log = log;
            _cache = cache;
        }

        /// <summary>The greeting for a name.</summary>
        public Greeting Greet(string name) =>
            new Greeting("Hello" + Greeting.Separator + name);

        /// <summary>A property, which is what C# exposes where Java exposes a getter.</summary>
        public string Culture { get; init; } = "en";

        // No modifier, which in C# means private on a member and internal on a type — two
        // defaults, not one, and neither of them public.
        string Serialise(Greeting g) => JsonSerializer.Serialize(g);

        void Probe(RegistryKey key) => _log.LogDebug("{Name}", key.Name);
    }
}
