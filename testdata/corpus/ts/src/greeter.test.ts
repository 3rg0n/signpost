import { describe, expect, it } from "vitest";

import { greet } from "./greeter";

describe("greet", () => {
  it("greets", () => {
    expect(greet("world").text).toBe("hello, world");
  });
});
