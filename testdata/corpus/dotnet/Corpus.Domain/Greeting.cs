// Corpus fixture: not restored, not built.

// The file-scoped namespace, which is what every template has emitted since C# 10 and so
// what most C# written today looks like. It declares the namespace for the whole file and
// opens no scope, so the type below sits at brace depth 0.
namespace Corpus.Domain;

/// <summary>A greeting the API returns.</summary>
public sealed record Greeting(string Text)
{
    /// <summary>The separator between the greeting and the name.</summary>
    public const string Separator = ", ";
}
