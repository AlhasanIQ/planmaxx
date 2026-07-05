import type { Anchor } from "../types";

function startChar(anchor: Anchor): number {
  return anchor.startChar ?? 0;
}

function endChar(anchor: Anchor): number {
  return anchor.endChar ?? 0;
}

function hasCharacterRange(anchor: Anchor): boolean {
  return startChar(anchor) !== 0 || endChar(anchor) !== 0;
}

export function anchorTouchesLine(anchor: Anchor, lineNumber: number): boolean {
  return lineNumber >= anchor.startLine && lineNumber <= anchor.endLine;
}

export function anchorLabel(anchor: Anchor): string {
  if (!hasCharacterRange(anchor)) {
    return anchor.startLine === anchor.endLine
      ? `Line ${anchor.startLine}`
      : `Lines ${anchor.startLine}-${anchor.endLine}`;
  }

  if (anchor.startLine === anchor.endLine) {
    return `Line ${anchor.startLine}:${startChar(anchor)}-${endChar(anchor)}`;
  }
  return `Lines ${anchor.startLine}:${startChar(anchor)}-${anchor.endLine}:${endChar(anchor)}`;
}
