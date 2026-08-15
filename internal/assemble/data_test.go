package assemble

import (
	"sort"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/graph"
)

// The data pass is two behaviours, and only one of them draws anything.
//
// A test suite that asserted the edges alone would pass equally well against a pass that
// drew an edge for every table-shaped word it found: `%s`, `{table}`, a table no migration
// declares, a name inside a comment. Those are the readings that put a page called `%s` in a
// committed bundle, and ADR 0034's whole decision is about declining them. So the refusals
// are tested here beside the edges, and the counts are asserted rather than the presence —
// an assertion that `orders` got an edge cannot fail when `%s` got one too.

// accessOf renders every data edge in the graph as "module kind table", sorted. The whole
// pass's output in one comparable value, which is what makes an extra edge a failure rather
// than something a presence check walks past.
func accessOf(g *graph.Graph) []string {
	var out []string
	for _, e := range g.Edges() {
		if e.Kind != graph.EdgeWrites && e.Kind != graph.EdgeReads {
			continue
		}
		out = append(out, e.From+" "+string(e.Kind)+" "+e.To)
	}
	sort.Strings(out)
	return out
}

func sameAccess(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The positive boundary: a module whose source names a table a migration declares is linked
// to it, in the direction the statement states.
func TestAModuleIsLinkedToTheTablesItsSQLNames(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                   "module example.com/app\n\ngo 1.26\n",
		"migrations/0001_init.sql": "CREATE TABLE orders (id int);\nCREATE TABLE customers (id int);\n",
		"internal/store/store.go": "package store\n\nimport \"database/sql\"\n\n" +
			"func Insert(db *sql.DB) error {\n" +
			"\t_, err := db.Exec(\"INSERT INTO orders (id) VALUES ($1)\", 1)\n\treturn err\n}\n\n" +
			"func List(db *sql.DB) error {\n" +
			"\t_, err := db.Query(\"SELECT id FROM customers WHERE id = $1\", 1)\n\treturn err\n}\n",
	})
	want := []string{
		"/modules/store reads /data/customers",
		"/modules/store writes /data/orders",
	}
	if got := accessOf(out.Graph); !sameAccess(got, want) {
		t.Errorf("data edges = %v, want %v", got, want)
	}
	// Extracted and nothing else. A deterministic pass draws Extracted or draws nothing
	// (ADR 0034), and an Ambiguous edge here would merge silently into a real one through
	// confRank while a reviewer had nothing to check about it.
	for _, e := range out.Graph.Edges() {
		if e.Kind != graph.EdgeWrites && e.Kind != graph.EdgeReads {
			continue
		}
		if e.Conf != graph.Extracted {
			t.Errorf("%s->%s confidence = %q, want extracted", e.From, e.To, e.Conf)
		}
		if e.Source == "" {
			t.Errorf("%s->%s carries no source file; the edge claims a file says something "+
				"and a reviewer has to be able to open it", e.From, e.To)
		}
	}
}

// The negative boundary ADR 0034 turns on. A statement whose table is assembled at run time
// draws no edge and is counted, and the count is what keeps the absence honest: a module
// reporting one table when it touches three is the failure §4.4 forbids.
func TestAnInterpolatedTableNameDrawsNoEdgeAndIsCounted(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                   "module example.com/app\n\ngo 1.26\n",
		"migrations/0001_init.sql": "CREATE TABLE orders (id int);\n",
		"internal/store/store.go": "package store\n\nimport (\n\t\"database/sql\"\n\t\"fmt\"\n)\n\n" +
			"func Purge(db *sql.DB, table string) error {\n" +
			"\t_, err := db.Exec(fmt.Sprintf(\"DELETE FROM %s WHERE done\", table))\n\treturn err\n}\n\n" +
			"func Count(db *sql.DB, table string) error {\n" +
			"\t_, err := db.Query(\"SELECT count(*) FROM \" + table)\n\treturn err\n}\n",
	})
	if got := accessOf(out.Graph); len(got) != 0 {
		t.Errorf("data edges = %v, want none. The table is whatever the caller passed, and "+
			"resolving it needs a call graph this project does not have", got)
	}
	if out.InterpolatedTables != 2 {
		t.Errorf("InterpolatedTables = %d, want 2. Dropped silently, a module that touches "+
			"two tables reads as touching none with nothing saying so",
			out.InterpolatedTables)
	}
	// And not in the other counter: the name is not unknown, it is absent.
	if len(out.UnknownTables) != 0 {
		t.Errorf("UnknownTables = %v, want empty. A run-time name and a name no migration "+
			"declares have different fixes, and one merged number is a number nobody can act "+
			"on", out.UnknownTables)
	}
}

// The other refusal: a name the source spells out that no migration declares. No node is
// created for it, because a table page is a durable concept and a typo in a string literal
// would mint one for a table that does not exist.
func TestATableNoMigrationDeclaresGetsNoNodeAndIsCounted(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                   "module example.com/app\n\ngo 1.26\n",
		"migrations/0001_init.sql": "CREATE TABLE orders (id int);\n",
		"internal/store/store.go": "package store\n\nimport \"database/sql\"\n\n" +
			"func Read(db *sql.DB) error {\n" +
			"\t_, err := db.Query(\"SELECT id FROM legacy_orders\")\n\treturn err\n}\n\n" +
			"func Write(db *sql.DB) error {\n" +
			"\t_, err := db.Exec(\"INSERT INTO orders (id) VALUES (1)\")\n\treturn err\n}\n",
	})
	g := out.Graph
	if n := g.Node("/data/legacy-orders"); n != nil {
		t.Errorf("a node was created for legacy_orders from a query. addData builds tables "+
			"from migrations, and minting one here lets a misspelled literal put a table on "+
			"the map that does not exist: %+v", n)
	}
	// The table that is declared still gets its edge: the refusal is per name, not per file.
	want := []string{"/modules/store writes /data/orders"}
	if got := accessOf(g); !sameAccess(got, want) {
		t.Errorf("data edges = %v, want %v", got, want)
	}
	if out.UnknownTables["legacy_orders"] != 1 {
		t.Errorf("UnknownTables = %v, want legacy_orders counted once", out.UnknownTables)
	}
	if out.InterpolatedTables != 0 {
		t.Errorf("InterpolatedTables = %d, want 0; the name is spelled out in the source",
			out.InterpolatedTables)
	}
	// Reported as the name the source used, not as a slug: the reader's next step is to grep
	// for it or to go write the migration, and `legacy-orders` is neither.
	for name := range out.UnknownTables {
		if strings.Contains(name, "-") {
			t.Errorf("UnknownTables key = %q, want the name as the source spelled it", name)
		}
	}
}

// The question ADR 0032 left open and 0034 answered: two writers of one table are each
// linked to the table and never to each other.
func TestTwoWritersOfOneTableAreNotLinkedToEachOther(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                   "module example.com/app\n\ngo 1.26\n",
		"migrations/0001_init.sql": "CREATE TABLE orders (id int);\n",
		"internal/store/store.go": "package store\n\nimport \"database/sql\"\n\n" +
			"func Write(db *sql.DB) error {\n" +
			"\t_, err := db.Exec(\"INSERT INTO orders (id) VALUES (1)\")\n\treturn err\n}\n",
		"reconcile/fix.py": "import sqlite3\n\n\ndef fix(db):\n" +
			"    db.execute(\"UPDATE orders SET total = 0\")\n",
	})
	g := out.Graph
	want := []string{
		"/modules/reconcile writes /data/orders",
		"/modules/store writes /data/orders",
	}
	if got := accessOf(g); !sameAccess(got, want) {
		t.Fatalf("data edges = %v, want %v", got, want)
	}
	for _, e := range g.EdgesFrom("/modules/store") {
		if e.To == "/modules/reconcile" {
			t.Errorf("store->reconcile %s. The coupling is real and no file declares it; "+
				"both writers are one hop from the table, which is the page a reader with a "+
				"duplicate row opens", e.Kind)
		}
	}
	for _, e := range g.EdgesFrom("/modules/reconcile") {
		if e.To == "/modules/store" {
			t.Errorf("reconcile->store %s, same reason", e.Kind)
		}
	}
}

// One module writing one table from many statements is one edge. The count that matters is
// which module, not how many call sites — a weight would read as coupling strength where it
// only measures verbosity.
func TestManyStatementsAgainstOneTableAreOneEdge(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                   "module example.com/app\n\ngo 1.26\n",
		"migrations/0001_init.sql": "CREATE TABLE orders (id int);\n",
		"internal/store/a.go": "package store\n\nimport \"database/sql\"\n\n" +
			"func A(db *sql.DB) error {\n" +
			"\t_, err := db.Exec(\"INSERT INTO orders (id) VALUES (1)\")\n\treturn err\n}\n",
		"internal/store/b.go": "package store\n\nimport \"database/sql\"\n\n" +
			"func B(db *sql.DB) error {\n" +
			"\t_, err := db.Exec(\"UPDATE orders SET total = 0\")\n\treturn err\n}\n" +
			"func C(db *sql.DB) error {\n" +
			"\t_, err := db.Exec(\"DELETE FROM orders WHERE done\")\n\treturn err\n}\n",
	})
	want := []string{"/modules/store writes /data/orders"}
	if got := accessOf(out.Graph); !sameAccess(got, want) {
		t.Errorf("data edges = %v, want %v", got, want)
	}
	for _, e := range out.Graph.Edges() {
		if e.Kind == graph.EdgeWrites && e.Weight != 0 {
			t.Errorf("writes edge weight = %d, want 0. Eleven statements against one table "+
				"is not eleven times the relationship one statement is", e.Weight)
		}
	}
}

// A module may both write and read one table, and both facts survive as separate edges: the
// pair is what a reader chasing a stale read is comparing.
func TestAModuleThatWritesAndReadsOneTableGetsBothEdges(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                   "module example.com/app\n\ngo 1.26\n",
		"migrations/0001_init.sql": "CREATE TABLE orders (id int);\n",
		"internal/store/store.go": "package store\n\nimport \"database/sql\"\n\n" +
			"func W(db *sql.DB) error {\n" +
			"\t_, err := db.Exec(\"INSERT INTO orders (id) VALUES (1)\")\n\treturn err\n}\n" +
			"func R(db *sql.DB) error {\n" +
			"\t_, err := db.Query(\"SELECT id FROM orders\")\n\treturn err\n}\n",
	})
	want := []string{
		"/modules/store reads /data/orders",
		"/modules/store writes /data/orders",
	}
	if got := accessOf(out.Graph); !sameAccess(got, want) {
		t.Errorf("data edges = %v, want %v — two kinds rather than one with a direction "+
			"attribute, because they are two questions", got, want)
	}
}

// A statement in a migration is not a module's data access. The migration is already the
// table's history, and drawing an edge from the directory holding it would report the
// migrations directory as a writer of every table in the schema.
func TestAMigrationIsNotCountedAsAWriter(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                   "module example.com/app\n\ngo 1.26\n",
		"migrations/0001_init.sql": "CREATE TABLE orders (id int);\nINSERT INTO orders (id) VALUES (1);\n",
	})
	if got := accessOf(out.Graph); len(got) != 0 {
		t.Errorf("data edges = %v, want none. A .sql file is read by the migration reader "+
			"and is not source; the table's page already holds its migration history", got)
	}
}

// Prose is most of what a source file's string literals hold, and an error message that
// mentions a verb is the shape that reaches this pass looking like a query.
func TestProseMentioningATableNameDrawsNothing(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                   "module example.com/app\n\ngo 1.26\n",
		"migrations/0001_init.sql": "CREATE TABLE orders (id int);\n",
		"internal/store/store.go": "package store\n\nimport \"errors\"\n\n" +
			"// Insert into orders after validating the payload.\n" +
			"func Insert() error {\n" +
			"\treturn errors.New(\"could not update the order\")\n}\n\n" +
			"func Fail() error {\n" +
			"\treturn errors.New(\"insert into orders failed: retry\")\n}\n",
	})
	if got := accessOf(out.Graph); len(got) != 0 {
		t.Errorf("data edges = %v, want none", got)
	}
	// And no gap either. Prose is not a statement signpost could not resolve, and counting
	// it would make the number a reader cannot act on — this is the direction that looks
	// fine in the output, because nothing wrong appears on the map.
	if out.InterpolatedTables != 0 || len(out.UnknownTables) != 0 {
		t.Errorf("gaps = %d interpolated, %v unknown; want none. A comment and an error "+
			"message are not queries", out.InterpolatedTables, out.UnknownTables)
	}
}

// A repository whose schema lives outside the tree: every query names a real table, no
// migration declares any of them, and the honest output is no edges and a count. This is the
// case that would otherwise mint a page per table read out of a string literal.
func TestASchemaManagedOutsideTheTreeReportsEveryTable(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.26\n",
		"internal/store/store.go": "package store\n\nimport \"database/sql\"\n\n" +
			"func A(db *sql.DB) error {\n" +
			"\t_, err := db.Query(\"SELECT id FROM orders JOIN customers ON true\")\n\treturn err\n}\n" +
			"func B(db *sql.DB) error {\n" +
			"\t_, err := db.Exec(\"INSERT INTO audit (id) VALUES (1)\")\n\treturn err\n}\n",
	})
	if n := len(out.Graph.NodesOfKind(graph.KindDataStore)); n != 0 {
		t.Errorf("data nodes = %d, want 0", n)
	}
	if got := accessOf(out.Graph); len(got) != 0 {
		t.Errorf("data edges = %v, want none", got)
	}
	for _, name := range []string{"orders", "customers", "audit"} {
		if out.UnknownTables[name] != 1 {
			t.Errorf("UnknownTables = %v, want %s counted once", out.UnknownTables, name)
		}
	}
	if len(out.UnknownTables) != 3 {
		t.Errorf("UnknownTables = %v, want exactly the three tables the source names",
			out.UnknownTables)
	}
}

// A qualified name keeps its qualifier on both sides of the join. Two schemas holding a
// table of one name are two tables, and resolving `things` to `public.things` would need the
// search_path, which is a run-time value.
func TestAQualifiedNameMatchesOnlyTheQualifiedTable(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                   "module example.com/app\n\ngo 1.26\n",
		"migrations/0001_init.sql": "CREATE TABLE public.orders (id int);\n",
		"internal/store/store.go": "package store\n\nimport \"database/sql\"\n\n" +
			"func A(db *sql.DB) error {\n" +
			"\t_, err := db.Query(\"SELECT id FROM public.orders\")\n\treturn err\n}\n" +
			"func B(db *sql.DB) error {\n" +
			"\t_, err := db.Query(\"SELECT id FROM audit.orders\")\n\treturn err\n}\n",
	})
	want := []string{"/modules/store reads /data/public-orders"}
	if got := accessOf(out.Graph); !sameAccess(got, want) {
		t.Errorf("data edges = %v, want %v", got, want)
	}
	if out.UnknownTables["audit.orders"] != 1 {
		t.Errorf("UnknownTables = %v, want audit.orders counted: a table of the same name in "+
			"another schema is another table", out.UnknownTables)
	}
}

// A gap in a file whose directory became no module page is still a gap. Counting it after
// the module lookup would make the number depend on which directories got pages, which is
// the quiet failure Unlinked was added for.
func TestAGapInAnUnplaceableFileIsStillCounted(t *testing.T) {
	out := build(t, map[string]string{
		// No go.mod and no sibling source: a lone .sql-free script directory that discover
		// classifies as source and assemble gives no module page.
		"scripts/purge.sh": "#!/bin/sh\npsql -c \"DELETE FROM $TABLE WHERE done\"\n",
	})
	if out.InterpolatedTables != 1 {
		t.Errorf("InterpolatedTables = %d, want 1", out.InterpolatedTables)
	}
}

// The pass is deterministic, which the committed bundle depends on: the same repository
// assembled twice must produce the same edges in the same order, or CI churns on every run.
func TestDataEdgesAreDeterministic(t *testing.T) {
	files := map[string]string{
		"go.mod":                   "module example.com/app\n\ngo 1.26\n",
		"migrations/0001_init.sql": "CREATE TABLE orders (id int);\nCREATE TABLE customers (id int);\n",
		"internal/store/store.go": "package store\n\nimport \"database/sql\"\n\n" +
			"func A(db *sql.DB) error {\n" +
			"\t_, err := db.Exec(\"INSERT INTO orders (id) VALUES (1)\")\n\treturn err\n}\n" +
			"func B(db *sql.DB) error {\n" +
			"\t_, err := db.Query(\"SELECT id FROM customers\")\n\treturn err\n}\n",
		"reconcile/fix.py": "def fix(db):\n    db.execute(\"UPDATE orders SET total = 0\")\n",
	}
	first := accessOf(build(t, files).Graph)
	if len(first) != 3 {
		t.Fatalf("data edges = %v, want 3", first)
	}
	for i := 0; i < 5; i++ {
		if got := accessOf(build(t, files).Graph); !sameAccess(got, first) {
			t.Fatalf("run %d = %v, first = %v", i, got, first)
		}
	}
}
