// Corpus fixture: not restored, not built.
//
// The three shapes go/store/store.go's doc comment describes, in C#'s spellings.
//
// C# is the one language here whose multi-line form and whose interpolation are *composable*:
// `@"..."` spans lines, `$"..."` interpolates, and `$@"..."` does both. So the two boundaries
// are not two delimiters here but two prefixes on one, and a reader keying off the quote alone
// cannot tell them apart — which is why the verbatim query below carries a spelled-out name
// and the interpolated one does not.

using System.Data;

namespace Corpus.Api;

/// <summary>Reaches the corpus schema.</summary>
public class Orders
{
    /// <summary>A verbatim string, which is how C# wrote a multi-line query before raw
    /// literals and still how most existing code does.</summary>
    private const string List = @"
SELECT o.id, o.total
FROM orders o
JOIN customers c ON c.id = o.customer_id
WHERE o.total > @total
";

    /// <summary>Reads two tables.</summary>
    public void ListOrders(IDbCommand cmd)
    {
        cmd.CommandText = List;
        cmd.ExecuteReader();
    }

    /// <summary>Writes the table it names.</summary>
    public void Record(IDbCommand cmd)
    {
        cmd.CommandText = "INSERT INTO customers (id) VALUES (@id)";
        cmd.ExecuteNonQuery();
    }

    /// <summary>The gap.</summary>
    public void Purge(IDbCommand cmd, string table)
    {
        cmd.CommandText = $"DELETE FROM {table} WHERE total = 0";
        cmd.ExecuteNonQuery();
    }

    /// <summary>Prose.</summary>
    public string Warn() => "could not update the order";
}
