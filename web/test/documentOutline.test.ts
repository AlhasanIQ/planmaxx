import { describe, expect, test } from "bun:test";
import { documentOutline, outlineRowIdForLine } from "../src/lib/documentOutline";
import type { ChangeRow } from "../src/types";

describe("documentOutline", () => {
  test("parses ATX and setext Markdown headings while ignoring fenced examples", () => {
    const outline = documentOutline([
      "# Product **plan**",
      "",
      "Overview",
      "--------",
      "```md",
      "## Not a section",
      "```",
      "### [Delivery](https://example.com)",
    ].join("\n"), "markdown");

    expect(outline.map(({ level, line, title }) => ({ level, line, title }))).toEqual([
      { level: 1, line: 1, title: "Product plan" },
      { level: 2, line: 3, title: "Overview" },
      { level: 3, line: 8, title: "Delivery" },
    ]);
  });

  test("parses HTML headings and explicitly labelled semantic sections", () => {
    const outline = documentOutline([
      "<main>",
      "  <h1>Account &amp; billing</h1>",
      "  <section aria-label=\"Rollout controls\">",
      "    <h2><span>Safety</span> checks</h2>",
      "  </section>",
      "  <script>'<h2>Not visible</h2>'</script>",
      "</main>",
    ].join("\n"), "html");

    expect(outline.map(({ kind, line, title }) => ({ kind, line, title }))).toEqual([
      { kind: "heading", line: 2, title: "Account & billing" },
      { kind: "section", line: 3, title: "Rollout controls" },
      { kind: "heading", line: 4, title: "Safety checks" },
    ]);
  });

  test("maps current-document lines to visible rows in a comparison", () => {
    const rows: ChangeRow[] = [
      { id: "row-1", index: 0, kind: "remove", before: 1, text: "old" },
      { id: "row-2", index: 1, kind: "add", after: 1, text: "new" },
      { id: "row-3", index: 2, kind: "context", before: 2, after: 2, text: "keep" },
    ];
    expect(outlineRowIdForLine(1, rows)).toBe("row-2");
    expect(outlineRowIdForLine(2, rows)).toBe("row-3");
    expect(outlineRowIdForLine(4, rows)).toBeNull();
    expect(outlineRowIdForLine(4)).toBe("line-4");
  });
});
