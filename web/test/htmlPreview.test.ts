import { describe, expect, test } from "bun:test";
import {
  htmlPreviewAnnotation,
  htmlPreviewDocument,
  sanitizeHTMLPreviewSource,
  sourceOffsetsForAnchor,
} from "../src/lib/htmlPreview";

describe("htmlPreviewDocument", () => {
  test("places a nonce-restricted review bridge before sanitized authored content", () => {
    const source = `<script>parent.fetch('/api/finalize')</script><h1>Plan</h1>`;
    const preview = htmlPreviewDocument(source, "light");

    expect(preview.indexOf("Content-Security-Policy")).toBeLessThan(preview.indexOf("Plan</h1>"));
    expect(preview).toContain("script-src 'nonce-");
    expect(preview).toContain("connect-src 'none'");
    expect(preview).toContain("form-action 'none'");
    expect(preview).toContain("planmaxx:preview-selection");
    expect(preview).toContain('"keyup"');
    expect(preview).not.toContain("parent.fetch");
  });

  test("includes a final interaction guard for preview-only content", () => {
    const preview = htmlPreviewDocument(`<a href="https://example.com">Link</a><form><button>Send</button></form>`, "light");
    expect(preview).toContain("form-action 'none'");
    expect(preview).not.toContain('href="https://example.com"');
    expect(preview).not.toContain("a { pointer-events: none !important; }");
    expect(preview).toContain("form, input, button");
  });

  test("publishes source-backed scroll positions for Preview and Source synchronization", () => {
    const document = htmlPreviewDocument(
      "<html><head><style>.plan { color: red; }</style></head><body><main>\n<section><h2>Safety</h2></section>\n</main></body></html>",
      "light",
    );

    expect(document).toContain("planmaxx:preview-position");
    expect(document).toContain("currentSourcePosition");
    expect(document).toContain("renderedSourceEntries");
    expect(document).toContain("body.querySelectorAll");
    expect(document).toContain("scrollRectForSourceSpan");
    expect(document).toContain("sourcePosition(offset).line");
    expect(document).not.toContain("ratio: maxScroll");
    expect(document).not.toContain("ratioTop");
  });

  test("renders segmented element targets without conflating text-range comments", () => {
    const document = htmlPreviewDocument("<p>Review <span>a long inline target</span></p>", "dark");

    expect(document).toContain("rectsForElement");
    expect(document).toContain("element.getClientRects()");
    expect(document).toContain("Comment on ");
    expect(document).toContain("Open comment");
    expect(document).toContain("annotation.start === span.start && annotation.end === span.end");
    expect(document).toContain("if (hasTextSelection()) setHoverTarget(null)");
  });

  test("publishes exact comment geometry and reserves safe inline slots", () => {
    const document = htmlPreviewDocument(
      "<main><ol><li>Step</li></ol><table><tr><td>Gate</td></tr></table><svg><text>Flow</text></svg></main>",
      "light",
    );

    expect(document).toContain("planmaxx:preview-layout");
    expect(document).toContain("data-planmaxx-inline-slot");
    expect(document).toContain('element.closest("table")');
    expect(document).toContain('element.closest("svg")');
    expect(document).toContain('element.closest("li")');
    expect(document).toContain("slot.scrollIntoView");
    expect(document).toContain("planmaxx:preview-scroll-by");
  });

  test("supports two-way range and thread focus animations", () => {
    const document = htmlPreviewDocument("<p>Ship safely</p>", "dark");

    expect(document).toContain("annotationAtPoint");
    expect(document).toContain("planmaxx:preview-focus-thread");
    expect(document).toContain("planmaxx:preview-ping");
    expect(document).toContain("planmaxx-target-ping");
  });

  test("keeps authored HTML and applies the selected base theme", () => {
    const preview = htmlPreviewDocument("<h1>Plan</h1>", "dark");
    expect(preview).toContain('<h1 data-planmaxx-source="0:13">');
    expect(preview).toContain("Plan</h1>");
    expect(preview).toContain("color-scheme: dark");
  });
});

describe("sanitizeHTMLPreviewSource", () => {
  test("removes executable and remote-resource surfaces while retaining safe raster data", () => {
    const preview = sanitizeHTMLPreviewSource(
      `<div data-planmaxx-source="spoof" onclick="alert(1)"><img src="https://example.com/a.png"><img src="data:image/png;base64,AA=="></div>`,
    );

    expect(preview).not.toContain("onclick");
    expect(preview).not.toContain("https://example.com");
    expect(preview).not.toContain("spoof");
    expect(preview).toContain("data:image/png;base64,AA==");
  });

  test("marks literal text precisely and entity-decoded text conservatively", () => {
    const preview = sanitizeHTMLPreviewSource("<p>Literal &amp; encoded</p>");

    expect(preview).toContain("planmaxx-text:1:3:24:0");
    expect(preview).toContain("Literal &amp; encoded");
  });

  test("maps decoded entity boundaries back to their exact source offsets", () => {
    const preview = htmlPreviewDocument("<p>A &amp; B</p>", "light");

    expect(preview).toContain('"textMappings":{"1":[3,4,5,10,11,12]}');
  });

  test("does not inject source markers into authored CSS raw text", () => {
    const preview = sanitizeHTMLPreviewSource("<style>p { color: red; }</style><p>Text</p>");
    const style = preview.slice(preview.indexOf("<style"), preview.indexOf("</style>"));

    expect(style).not.toContain("planmaxx-text");
    expect(preview).toContain("p { color: red; }");
    expect(preview).toContain("planmaxx-text");
  });
});

describe("HTML preview source anchors", () => {
  const source = "<main>\n  <h1>Launch</h1>\n  <p>Ship safely.</p>\n</main>";

  test("converts source anchors to global offsets for preview highlighting", () => {
    expect(sourceOffsetsForAnchor(source, { startLine: 2, startChar: 6, endLine: 2, endChar: 12 })).toEqual({
      start: source.indexOf("Launch"),
      end: source.indexOf("Launch") + "Launch".length,
    });
    expect(sourceOffsetsForAnchor(source, { startLine: 3, endLine: 3 })).toEqual({
      start: source.indexOf("  <p>"),
      end: source.indexOf("\n</main>"),
    });
  });

  test("builds persisted preview annotations from the same source coordinates", () => {
    expect(htmlPreviewAnnotation(
      source,
      { startLine: 2, startChar: 6, endLine: 2, endChar: 12 },
      "thread-1",
    )).toEqual({
      id: "thread-1",
      start: source.indexOf("Launch"),
      end: source.indexOf("Launch") + "Launch".length,
    });
  });
});
