<?php

// Reaching the corpus schema from PHP. Corpus fixture: not installed, not run.

declare(strict_types=1);

namespace Corpus;

use PDO;

/**
 * The three shapes go/store/store.go's doc comment describes, in PHP's spellings.
 *
 * The heredoc and the nowdoc differ in exactly the way that matters here: `<<<SQL` interpolates
 * and `<<<'SQL'` does not, so the same delimiter form holds a name and a gap depending on one
 * pair of quotes in the opener.
 */
final class Store
{
    /** @var PDO */
    private $db;

    public function __construct(PDO $db)
    {
        $this->db = $db;
    }

    /** Reads two tables. */
    public function listOrders(): void
    {
        $sql = <<<'SQL'
        SELECT o.id, o.total
        FROM orders o
        JOIN customers c ON c.id = o.customer_id
        SQL;
        $this->db->query($sql);
    }

    /** Writes the table it names. */
    public function record(): void
    {
        $this->db->exec('INSERT INTO customers (id) VALUES (1)');
    }

    /** The gap. */
    public function purge(string $table): void
    {
        $this->db->exec("DELETE FROM {$table} WHERE total = 0");
    }

    /** Prose. */
    public function warn(string $table): string
    {
        return "insert into {$table} failed: retry";
    }
}
