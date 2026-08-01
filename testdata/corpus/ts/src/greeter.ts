// Greeting helpers. Corpus fixture: not compiled, not run.

export interface Greeting {
  text: string;
}

/** Builds a greeting for `name`. */
export function greet(name: string): Greeting {
  return { text: `hello, ${name}` };
}

function unexported(): void {}
