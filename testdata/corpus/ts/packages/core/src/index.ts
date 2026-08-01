// The workspace package that siblings import by published name.
//
// `main` in package.json points at `dist/index.js`, which is build output and is not in
// the repository — the normal shape for a published package. So resolving a bare import
// of `@corpus/core` has to fall back to the source root, not give up at the package
// directory.

export interface Token {
  value: string;
}

export function mint(value: string): Token {
  return { value };
}
