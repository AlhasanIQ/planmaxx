import { parse, serialize, type DefaultTreeAdapterTypes } from "parse5";
import { DecodingMode, EntityDecoder, htmlDecodeTree } from "entities/decode";
import type { Anchor } from "../types";
import { htmlPreviewBridgeScript } from "./htmlPreviewBridge";

export function htmlPreviewDocument(source: string, theme: "light" | "dark"): string {
  const background = theme === "dark" ? "#111831" : "#ffffff";
  const foreground = theme === "dark" ? "#e6ebf5" : "#0f172a";
  const muted = theme === "dark" ? "#9aa6bc" : "#5b6573";
  const border = theme === "dark" ? "#2e3b66" : "#d8dee8";
  const nonce = previewNonce();
  const csp = [
    "default-src 'none'",
    `script-src 'nonce-${nonce}'`,
    "style-src 'unsafe-inline'",
    "img-src data:",
    "font-src data:",
    "media-src data:",
    "connect-src 'none'",
    "frame-src 'none'",
    "object-src 'none'",
    "form-action 'none'",
    "base-uri 'none'",
  ].join("; ");

  const prepared = prepareHTMLPreviewSource(source);
  const reviewHead = `
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${csp}">
<style>
  :root { color-scheme: ${theme}; }
  html { background: ${background}; color: ${foreground}; }
  body { box-sizing: border-box; margin: 0; padding: 24px; font: 14px/1.6 ui-sans-serif, system-ui, sans-serif; }
  *, *::before, *::after { box-sizing: inherit; }
  img, video, canvas, svg, table, pre { max-width: 100%; }
  table { border-collapse: collapse; }
  th, td { border: 1px solid ${border}; padding: 6px 8px; }
  pre { overflow: auto; }
  code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  blockquote { margin-left: 0; padding-left: 1em; border-left: 3px solid ${border}; color: ${muted}; }
</style>`;
  const reviewBody = `
<style>
  script, iframe, frame, object, embed, form, input, button, select, textarea { display: none !important; }
</style>
<script nonce="${nonce}">
window.__PLANMAXX_PREVIEW__ = ${JSON.stringify({
  lineStarts: sourceLineStarts(source),
  textMappings: prepared.textMappings,
})};
${htmlPreviewBridgeScript}
</script>`;
  return prepared.html
    .replace(/<head(\s[^>]*)?>/i, (head) => `${head}${reviewHead}`)
    .replace(/<\/body\s*>/i, `${reviewBody}</body>`);
}

const blockedTags = new Set("script,iframe,frame,object,embed,applet,form,input,button,select,textarea,template,meta,base,link,plaintext,animate,set,animatemotion,animatetransform".split(","));
const unmarkedTextParents = new Set(["style", "title", "noscript", "noembed", "noframes"]);
const resourceAttributes = new Set([
  "action",
  "background",
  "cite",
  "data",
  "dynsrc",
  "formaction",
  "href",
  "longdesc",
  "lowsrc",
  "ping",
  "poster",
  "src",
  "srcdoc",
  "srcset",
  "xlink:href",
]);

export function sanitizeHTMLPreviewSource(source: string): string {
  return prepareHTMLPreviewSource(source).html;
}

function prepareHTMLPreviewSource(source: string): {
  html: string;
  textMappings: Record<string, number[]>;
} {
  const document = parse(source, { sourceCodeLocationInfo: true });
  const state = { textID: 0, textMappings: {} as Record<string, number[]> };
  sanitizeChildren(document, source, state);
  return { html: serialize(document), textMappings: state.textMappings };
}

export interface HTMLPreviewAnnotation {
  id: string;
  start: number;
  end: number;
  active?: boolean;
  draft?: boolean;
  inlineSlot?: boolean;
  slotHeight?: number;
}

export function htmlPreviewAnnotation(source: string, anchor: Anchor, id: string): HTMLPreviewAnnotation | null {
  const range = sourceOffsetsForAnchor(source, anchor);
  return range.end > range.start ? { id, ...range } : null;
}

export function sourceOffsetsForAnchor(source: string, anchor: Anchor): { start: number; end: number } {
  const starts = sourceLineStarts(source);
  const startLine = clamp(anchor.startLine, 1, starts.length);
  const endLine = clamp(anchor.endLine, startLine, starts.length);
  const startLineOffset = starts[startLine - 1];
  const endLineOffset = starts[endLine - 1];
  const startLineEnd = sourceLineEnd(source, startLineOffset);
  const endLineEnd = sourceLineEnd(source, endLineOffset);
  const isCharacterRange = (anchor.startChar ?? 0) !== 0 || (anchor.endChar ?? 0) !== 0;
  if (!isCharacterRange) {
    return { start: startLineOffset, end: endLineEnd };
  }
  return {
    start: clamp(startLineOffset + (anchor.startChar ?? 0), startLineOffset, startLineEnd),
    end: clamp(endLineOffset + (anchor.endChar ?? 0), endLineOffset, endLineEnd),
  };
}

function sanitizeChildren(
  parent: DefaultTreeAdapterTypes.ParentNode,
  source: string,
  state: { textID: number; textMappings: Record<string, number[]> },
) {
  const children: DefaultTreeAdapterTypes.ChildNode[] = [];
  for (const child of parent.childNodes) {
    if (isComment(child)) {
      if (!child.data.startsWith("planmaxx-text:")) children.push(child);
      continue;
    }
    if (isElement(child)) {
      if (blockedTags.has(child.tagName.toLowerCase())) continue;
      sanitizeElement(child);
      sanitizeChildren(child, source, state);
      children.push(child);
      continue;
    }
    if (
      isText(child) &&
      !isUnmarkedTextParent(parent) &&
      child.sourceCodeLocation &&
      child.value.length > 0 &&
      child.value.trim().length > 0
    ) {
      const { startOffset, endOffset } = child.sourceCodeLocation;
      const raw = source.slice(startOffset, endOffset);
      const exact = raw === child.value;
      const textID = ++state.textID;
      if (!exact) {
        const mapping = decodedTextBoundaryMap(raw, child.value, startOffset);
        if (mapping) state.textMappings[String(textID)] = mapping;
      }
      const marker = textMarker(textID, startOffset, endOffset, exact, parent);
      children.push(marker, child);
      continue;
    }
    children.push(child);
  }
  parent.childNodes = children;
  for (const child of children) {
    if ("parentNode" in child) child.parentNode = parent;
  }
}

function sanitizeElement(element: DefaultTreeAdapterTypes.Element) {
  const tag = element.tagName.toLowerCase();
  element.attrs = element.attrs.filter((attribute) => {
    const name = attribute.name.toLowerCase();
    if (name.startsWith("data-planmaxx-") || name === "nonce") return false;
    const safeRasterImage =
      name === "src" &&
      tag === "img" &&
      /^data:image\/(?:avif|gif|jpeg|png|webp);base64,/i.test(attribute.value);
    if (safeRasterImage) return true;
    return !name.startsWith("on") && !resourceAttributes.has(name);
  });
  const location = element.sourceCodeLocation;
  if (location && location.startOffset < location.endOffset) {
    element.attrs.push({
      name: "data-planmaxx-source",
      value: `${location.startOffset}:${location.endOffset}`,
    });
  }
}

function textMarker(
  id: number,
  start: number,
  end: number,
  exact: boolean,
  parent: DefaultTreeAdapterTypes.ParentNode,
): DefaultTreeAdapterTypes.CommentNode {
  return {
    nodeName: "#comment",
    data: `planmaxx-text:${id}:${start}:${end}:${exact ? "1" : "0"}`,
    parentNode: parent,
  };
}

function isElement(node: DefaultTreeAdapterTypes.ChildNode): node is DefaultTreeAdapterTypes.Element {
  return "tagName" in node;
}

function isText(node: DefaultTreeAdapterTypes.ChildNode): node is DefaultTreeAdapterTypes.TextNode {
  return node.nodeName === "#text";
}

function isComment(node: DefaultTreeAdapterTypes.ChildNode): node is DefaultTreeAdapterTypes.CommentNode {
  return node.nodeName === "#comment";
}

function isUnmarkedTextParent(parent: DefaultTreeAdapterTypes.ParentNode): boolean {
  return "tagName" in parent && unmarkedTextParents.has(parent.tagName.toLowerCase());
}

function sourceLineStarts(source: string): number[] {
  const starts = [0];
  for (let index = 0; index < source.length; index++) {
    if (source[index] === "\n") starts.push(index + 1);
  }
  return starts;
}

function decodedTextBoundaryMap(raw: string, decoded: string, sourceStart: number): number[] | null {
  const boundaries = [sourceStart];
  let decodedValue = "";
  let rawOffset = 0;
  const emitted: Array<{ codePoint: number; consumed: number }> = [];
  const decoder = new EntityDecoder(
    htmlDecodeTree,
    (codePoint, consumed) => emitted.push({ codePoint, consumed }),
  );

  while (rawOffset < raw.length) {
    if (raw[rawOffset] === "&") {
      emitted.length = 0;
      decoder.startEntity(DecodingMode.Legacy);
      let consumed = decoder.write(raw, rawOffset + 1);
      if (consumed < 0) consumed = decoder.end();
      if (consumed > 0 && emitted.length > 0) {
        const entityEnd = sourceStart + rawOffset + consumed;
        for (const output of emitted) {
          const value = String.fromCodePoint(output.codePoint);
          decodedValue += value;
          for (let index = 0; index < value.length; index++) boundaries.push(entityEnd);
        }
        rawOffset += consumed;
        continue;
      }
    }

    const code = raw.charCodeAt(rawOffset);
    if (code === 0x0d) {
      const consumed = raw.charCodeAt(rawOffset + 1) === 0x0a ? 2 : 1;
      decodedValue += "\n";
      rawOffset += consumed;
      boundaries.push(sourceStart + rawOffset);
      continue;
    }
    if (code === 0x00) {
      decodedValue += "\ufffd";
      rawOffset++;
      boundaries.push(sourceStart + rawOffset);
      continue;
    }

    decodedValue += raw[rawOffset];
    rawOffset++;
    boundaries.push(sourceStart + rawOffset);
  }

  return decodedValue === decoded && boundaries.length === decoded.length + 1 ? boundaries : null;
}

function sourceLineEnd(source: string, lineStart: number): number {
  const newline = source.indexOf("\n", lineStart);
  const end = newline < 0 ? source.length : newline;
  return end > lineStart && source[end - 1] === "\r" ? end - 1 : end;
}

function previewNonce(): string {
  const bytes = new Uint8Array(16);
  if (globalThis.crypto?.getRandomValues) {
    globalThis.crypto.getRandomValues(bytes);
    return [...bytes].map((byte) => byte.toString(16).padStart(2, "0")).join("");
  }
  return `planmaxx-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value));
}
