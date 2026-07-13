import { describe, expect, test } from "bun:test";
import { htmlPreviewDocument } from "../src/lib/htmlPreview";

describe("htmlPreviewDocument", () => {
  test("places restrictive policy before untrusted source", () => {
    const source = `<script>parent.fetch('/api/finalize')</script><h1>Plan</h1>`;
    const preview = htmlPreviewDocument(source, "light");

    expect(preview.indexOf("Content-Security-Policy")).toBeLessThan(preview.indexOf(source));
    expect(preview).toContain("script-src 'none'");
    expect(preview).toContain("connect-src 'none'");
    expect(preview).toContain("form-action 'none'");
  });

  test("includes a final interaction guard for preview-only content", () => {
    const preview = htmlPreviewDocument(`<a href="https://example.com">Link</a><form><button>Send</button></form>`, "light");
    expect(preview).toContain("form-action 'none'");
    expect(preview).toContain("a { pointer-events: none !important; }");
    expect(preview).toContain("form, input, button");
  });

  test("keeps authored HTML and applies the selected base theme", () => {
    const preview = htmlPreviewDocument("<h1>Plan</h1>", "dark");
    expect(preview).toContain("<h1>Plan</h1>");
    expect(preview).toContain("color-scheme: dark");
  });
});
