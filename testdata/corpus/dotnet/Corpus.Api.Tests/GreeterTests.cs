// Corpus fixture: not restored, not built.

// The declared test package, whose name is its own root namespace — the one case where the
// convention that maps a namespace to a NuGet package holds exactly.
using Xunit;

// The subject, and the reason C# has no arm in addTestEdges even though it looks like the JVM.
// A .NET test project declares `Corpus.Api.Tests`, a namespace of its own, which resolves to
// this very directory and yields the self-edge the graph drops. A C# test names what it tests
// with a `using`, because a different namespace is exactly what a `using` is for — so the
// `tested_by` edge comes from this line and not from the declaration below.
using Corpus.Api;

namespace Corpus.Api.Tests;

public sealed class GreeterTests
{
    [Fact]
    public void Greets()
    {
        Assert.Equal("en", new Greeter(null!, null!).Culture);
    }
}
