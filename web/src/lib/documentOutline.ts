import type { ChangeRow, PlanFormat } from "../types";

export interface OutlineItem {
  id: string;
  level: number;
  line: number;
  title: string;
  kind: "heading" | "section";
}

export function documentOutline(source: string, format: PlanFormat): OutlineItem[] {
  return format === "html" ? htmlOutline(source) : markdownOutline(source);
}

export function outlineRowIdForLine(line: number, rows?: ChangeRow[]): string | null {
  if (!rows) return `line-${line}`;
  return rows.find((row) => row.kind !== "remove" && row.after === line)?.id ?? null;
}

function markdownOutline(source: string): OutlineItem[] {
  const lines = source.replace(/\r\n?/g, "\n").split("\n");
  const items: OutlineItem[] = [];
  let fence: { marker: "`" | "~"; length: number } | null = null;

  for (let index = 0; index < lines.length; index++) {
    const line = lines[index];
    const fenceMatch = /^ {0,3}(`{3,}|~{3,})/.exec(line);
    if (fenceMatch) {
      const marker = fenceMatch[1][0] as "`" | "~";
      if (!fence) fence = { marker, length: fenceMatch[1].length };
      else if (marker === fence.marker && fenceMatch[1].length >= fence.length) fence = null;
      continue;
    }
    if (fence) continue;

    const atx = /^ {0,3}(#{1,6})(?:[ \t]+|$)(.*)$/.exec(line);
    if (atx) {
      const title = markdownLabel(atx[2].replace(/[ \t]+#+[ \t]*$/, ""));
      if (title) items.push(outlineItem(index + 1, atx[1].length, title, "heading"));
      continue;
    }

    const setext = /^ {0,3}(=+|-+)[ \t]*$/.exec(line);
    if (setext && index > 0 && lines[index - 1].trim()) {
      const title = markdownLabel(lines[index - 1].trim());
      if (title) items.push(outlineItem(index, setext[1][0] === "=" ? 1 : 2, title, "heading"));
    }
  }
  return items;
}

function htmlOutline(source: string): OutlineItem[] {
  const searchable = maskIgnoredHTML(source);
  const starts = lineStarts(source);
  const entries: Array<OutlineItem & { index: number }> = [];
  const headingPattern = /<(h([1-6]))\b[^>]*>([\s\S]*?)<\/\1\s*>/gi;
  let match: RegExpExecArray | null;
  while ((match = headingPattern.exec(searchable))) {
    const title = htmlLabel(match[3]);
    if (!title) continue;
    const line = lineAt(starts, match.index);
    entries.push({ ...outlineItem(line, Number(match[2]), title, "heading"), index: match.index });
  }

  const sectionPattern = /<section\b([^>]*)>/gi;
  while ((match = sectionPattern.exec(searchable))) {
    const label = attributeValue(match[1], "aria-label") || attributeValue(match[1], "title");
    if (!label) continue;
    const line = lineAt(starts, match.index);
    entries.push({ ...outlineItem(line, 2, htmlLabel(label), "section"), index: match.index });
  }

  return entries
    .sort((left, right) => left.index - right.index)
    .map(({ index: _index, ...item }) => item);
}

function maskIgnoredHTML(source: string): string {
  return source.replace(/<!--[\s\S]*?-->|<(script|style|template)\b[^>]*>[\s\S]*?<\/\1\s*>/gi, (value) =>
    value.replace(/[^\n]/g, " "),
  );
}

function outlineItem(line: number, level: number, title: string, kind: OutlineItem["kind"]): OutlineItem {
  const slug = title.toLocaleLowerCase().replace(/[^\p{L}\p{N}]+/gu, "-").replace(/^-|-$/g, "").slice(0, 48) || kind;
  return { id: `outline-${line}-${slug}`, level, line, title, kind };
}

function markdownLabel(value: string): string {
  return decodeEntities(value)
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/<[^>]+>/g, "")
    .replace(/[`*_~]/g, "")
    .replace(/\\([\\`*_[\]{}()#+.!|>-])/g, "$1")
    .replace(/\s+/g, " ")
    .trim();
}

function htmlLabel(value: string): string {
  return decodeEntities(value.replace(/<[^>]*>/g, " ")).replace(/\s+/g, " ").trim();
}

function attributeValue(attributes: string, name: string): string {
  const match = new RegExp(`\\b${name}\\s*=\\s*(?:"([^"]*)"|'([^']*)'|([^\\s>]+))`, "i").exec(attributes);
  return match?.[1] ?? match?.[2] ?? match?.[3] ?? "";
}

function lineStarts(source: string): number[] {
  const starts = [0];
  for (let index = 0; index < source.length; index++) {
    if (source.charCodeAt(index) === 10) starts.push(index + 1);
  }
  return starts;
}

function lineAt(starts: number[], index: number): number {
  let low = 0;
  let high = starts.length;
  while (low < high) {
    const middle = Math.floor((low + high) / 2);
    if (starts[middle] <= index) low = middle + 1;
    else high = middle;
  }
  return low;
}

function decodeEntities(value: string): string {
  return value.replace(/&(#x[\da-f]+|#\d+|amp|lt|gt|quot|apos|nbsp);/gi, (entity, body: string) => {
    const normalized = body.toLowerCase();
    if (normalized === "amp") return "&";
    if (normalized === "lt") return "<";
    if (normalized === "gt") return ">";
    if (normalized === "quot") return '"';
    if (normalized === "apos") return "'";
    if (normalized === "nbsp") return " ";
    const point = Number.parseInt(normalized.startsWith("#x") ? normalized.slice(2) : normalized.slice(1), normalized.startsWith("#x") ? 16 : 10);
    try { return String.fromCodePoint(point); } catch { return entity; }
  });
}
