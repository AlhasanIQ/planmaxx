import { describe, expect, test } from "bun:test";
import { codeBlocks, highlightCodeBlocks, highlightHTMLSource } from "../src/lib/codeHighlight";

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

  test("highlights HTML source line by line without changing its text", async () => {
    const source = "<main>\n  <h1>Launch &amp; rollback</h1>\n</main>";
    const highlighted = await highlightHTMLSource(source, "dark");

    expect([...highlighted.keys()]).toEqual([1, 2, 3]);
    expect(highlighted.get(2)?.map((token) => token.content).join("")).toBe("  <h1>Launch &amp; rollback</h1>");
    expect(highlighted.get(2)?.some((token) => token.color)).toBe(true);
  });
});
