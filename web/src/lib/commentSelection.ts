import type { Anchor } from "../types";

export function anchorForCommentSelection(
  startLine: number,
  startChar: number,
  endLine: number,
  endChar: number,
): Anchor {
  return { startLine, startChar, endLine, endChar };
}

// Map boundaries in rendered inline text back to the source Markdown. This
// lets table cells omit pipes and formatting punctuation without losing exact
// comment coordinates.
export function sourceBoundariesForRenderedText(source: string, rendered: string): number[] {
  if (!rendered) return [0];

  const spans: Array<{ start: number; end: number }> = [];
  let cursor = 0;
  for (const character of rendered) {
    const escaped = escapedCharacterAtOrAfter(source, character, cursor);
    const entity = entityAtOrAfter(source, character, cursor);
    const literal = source.indexOf(character, cursor);
    const candidates = [escaped, entity, literal < 0 ? null : { start: literal, end: literal + character.length }]
      .filter((candidate): candidate is { start: number; end: number } => candidate !== null)
      .sort((left, right) => left.start - right.start);
    const match = candidates[0] ?? { start: cursor, end: cursor };
    spans.push(match);
    cursor = match.end;
  }

  const boundaries = [spans[0].start];
  let spanIndex = 0;
  for (const character of rendered) {
    const span = spans[spanIndex++];
    if (character.length === 2) {
      boundaries.push(source.slice(span.start, span.end) === character ? span.start + 1 : span.start);
    }
    boundaries.push(span.end);
  }
  return boundaries;
}

function escapedCharacterAtOrAfter(
  source: string,
  character: string,
  cursor: number,
): { start: number; end: number } | null {
  for (let index = source.indexOf("\\", cursor); index >= 0; index = source.indexOf("\\", index + 1)) {
    if (source.slice(index + 1, index + 1 + character.length) === character) {
      return { start: index, end: index + 1 + character.length };
    }
  }
  return null;
}

function entityAtOrAfter(
  source: string,
  character: string,
  cursor: number,
): { start: number; end: number } | null {
  const pattern = /&(?:#x[0-9a-f]+|#\d+|amp|quot|apos|#39|lt|gt);/gi;
  pattern.lastIndex = cursor;
  for (let match = pattern.exec(source); match; match = pattern.exec(source)) {
    if (decodeEntity(match[0]) === character) {
      return { start: match.index, end: match.index + match[0].length };
    }
  }
  return null;
}

function decodeEntity(entity: string): string {
  const normalized = entity.toLowerCase();
  const named: Record<string, string> = {
    "&amp;": "&",
    "&quot;": "\"",
    "&apos;": "'",
    "&#39;": "'",
    "&lt;": "<",
    "&gt;": ">",
  };
  if (named[normalized]) return named[normalized];
  const value = normalized.startsWith("&#x")
    ? Number.parseInt(normalized.slice(3, -1), 16)
    : Number.parseInt(normalized.slice(2, -1), 10);
  try {
    return String.fromCodePoint(value);
  } catch {
    return entity;
  }
}
