// Reaching the corpus schema from TypeScript. Corpus fixture: not built, not run.
//
// The three shapes go/store/store.go's doc comment describes, in TypeScript's spellings. The
// template literal is the interesting one: it is both how a multi-line query is written and how
// a name is interpolated, so the two boundaries share one delimiter here and the reader has to
// tell them apart by what is inside it rather than by which quote opened it.

const LIST_ORDERS = `
SELECT id, total
FROM orders
WHERE total > $1
`;

export async function listOrders(db: { query(sql: string): Promise<unknown> }) {
  return db.query(LIST_ORDERS);
}

export async function record(db: { query(sql: string): Promise<unknown> }) {
  return db.query("INSERT INTO customers (id) VALUES ($1)");
}

// The gap: same delimiter, and the table is a parameter.
export async function purge(
  db: { query(sql: string): Promise<unknown> },
  table: string,
) {
  return db.query(`DELETE FROM ${table} WHERE total = 0`);
}

// Prose.
export function warn(table: string): string {
  return `insert into ${table} failed, retrying`;
}
