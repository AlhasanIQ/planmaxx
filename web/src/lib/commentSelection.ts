import type { Anchor } from "../types";

// Table cells intentionally render without Markdown delimiters. Their DOM
// character offsets cannot address the original source line, so selections
// touching a table always apply to whole source rows.
export function anchorForCommentSelection(
  startLine: number,
  startChar: number,
  endLine: number,
  endChar: number,
  touchesStructuredRow: boolean,
): Anchor {
  if (touchesStructuredRow) {
    return { startLine, endLine };
  }
  return { startLine, startChar, endLine, endChar };
}
