import { describe, expect, test } from "bun:test";
import { codeBlocks, highlightCodeBlocks } from "../src/lib/codeHighlight";

describe("code highlighting", () => {
  test("finds fenced blocks and their source line numbers", () => {
    expect(codeBlocks("# Plan\n```ts\nconst answer = 42;\n```\ntext\n```python\nprint('ok')\n```")).toEqual([
      { language: "ts", startLine: 3, lines: ["const answer = 42;"] },
      { language: "python", startLine: 7, lines: ["print('ok')"] },
    ]);
  });

  test("returns styled tokens for a known fenced language", async () => {
    const highlighted = await highlightCodeBlocks("```ts\nconst answer = 42;\n```", "light");
    const tokens = highlighted.get(2);

    expect(tokens?.map((token) => token.content).join("")).toBe("const answer = 42;");
    expect(tokens?.some((token) => token.color)).toBe(true);
  });
});
