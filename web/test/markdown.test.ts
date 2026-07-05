import { describe, expect, test } from "bun:test";
import { renderPlanLines } from "../src/lib/markdown";

describe("renderPlanLines", () => {
  test("escapes raw html while preserving markdown formatting", () => {
    const [line] = renderPlanLines("Use **bold** <img src=x onerror=alert(1)>.");

    expect(line.html).toContain("<strong>bold</strong>");
    expect(line.html).not.toContain("<img");
    expect(line.html).toContain("&lt;img");
    expect(line.html).not.toContain("onerror=");
  });

  test("does not emit unsafe javascript links", () => {
    const [line] = renderPlanLines("[bad](javascript:alert(1)) [good](https://example.com)");

    expect(line.html).not.toContain("javascript:");
    expect(line.html).toContain('href="https://example.com"');
  });

  test("renders markdown images as plain alt text", () => {
    const [line] = renderPlanLines("![diagram](javascript:alert(1))");

    expect(line.html).toBe("diagram");
    expect(line.html).not.toContain("<img");
    expect(line.html).not.toContain("javascript:");
  });

  test("does not double escape autolink query separators", () => {
    const [line] = renderPlanLines("<https://example.com/?a=1&b=2>");

    expect(line.html).toContain('href="https://example.com/?a=1&amp;b=2"');
    expect(line.html).not.toContain("&amp;amp;");
  });

  test("does not emit entity-encoded unsafe links", () => {
    const [line] = renderPlanLines("[encoded](jav&#x61;script:alert(1))");

    expect(line.html).toBe("encoded");
    expect(line.html).not.toContain("href=");
  });

  test("does not emit links with ambiguous href characters", () => {
    const [backslashLine] = renderPlanLines("[backslash](java\\script:alert(1))");
    const [newlineLine] = renderPlanLines("[newline](java&#10;script:alert(1))");
    const [protocolRelativeLine] = renderPlanLines("[protocol-relative](//example.com/path)");

    expect(backslashLine.html).toBe("backslash");
    expect(newlineLine.html).toBe("newline");
    expect(protocolRelativeLine.html).toBe("protocol-relative");
    expect(`${backslashLine.html}${newlineLine.html}${protocolRelativeLine.html}`).not.toContain("href=");
  });
});
