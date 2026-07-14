import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import type React from "react";
import { anchorForCommentSelection } from "../lib/commentSelection";
import type { Anchor, Thread } from "../types";

export interface SelectionDraft {
  anchor: Anchor;
  selectedText: string;
  body: string;
}

export function DraftBoundaryHandles({
  articleRef,
  anchor,
  onChange,
}: {
  articleRef: React.RefObject<HTMLElement>;
  anchor: Anchor | null;
  onChange: (anchor: Anchor) => void;
}) {
  const dragSide = useRef<"start" | "end" | null>(null);
  const [positions, setPositions] = useState<{
    start: BoundaryHandlePosition;
    end: BoundaryHandlePosition;
  } | null>(null);

  const updatePositions = useCallback(() => {
    const root = articleRef.current;
    if (!root || !anchor) {
      setPositions(null);
      return;
    }
    const characterAnchor = materializeCharacterAnchor(root, anchor);
    const start = boundaryHandlePosition(root, characterAnchor.startLine, characterAnchor.startChar ?? 0);
    const end = boundaryHandlePosition(root, characterAnchor.endLine, characterAnchor.endChar ?? 0);
    setPositions(start && end ? { start, end } : null);
  }, [anchor, articleRef]);
  const updatePositionsRef = useRef(updatePositions);

  useEffect(() => {
    updatePositionsRef.current = updatePositions;
  }, [updatePositions]);

  useLayoutEffect(() => {
    updatePositionsRef.current();
    if (!anchor) return;
    const update = () => updatePositionsRef.current();
    window.addEventListener("resize", update);
    window.addEventListener("scroll", update, true);
    return () => {
      window.removeEventListener("resize", update);
      window.removeEventListener("scroll", update, true);
    };
  }, [anchor]);

  useEffect(() => {
    if (!anchor) return;
    const activeAnchor = anchor;

    function onPointerMove(event: PointerEvent) {
      const root = articleRef.current;
      const side = dragSide.current;
      if (!root || !side) return;
      const point = anchorPointFromClientPosition(root, event.clientX, event.clientY);
      if (!point) return;
      event.preventDefault();
      onChange(moveAnchorBoundary(root, activeAnchor, side, point));
    }

    function onPointerUp() {
      dragSide.current = null;
    }

    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp);
    window.addEventListener("pointercancel", onPointerUp);
    return () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
      window.removeEventListener("pointercancel", onPointerUp);
    };
  }, [anchor, articleRef, onChange]);

  if (!anchor || !positions) return null;

  return (
    <>
      <BoundaryHandle
        side="start"
        position={positions.start}
        onPointerDown={() => {
          dragSide.current = "start";
        }}
      />
      <BoundaryHandle
        side="end"
        position={positions.end}
        onPointerDown={() => {
          dragSide.current = "end";
        }}
      />
    </>
  );
}

function BoundaryHandle({
  side,
  position,
  onPointerDown,
}: {
  side: "start" | "end";
  position: BoundaryHandlePosition;
  onPointerDown: () => void;
}) {
  return (
    <button
      type="button"
      className={`draft-boundary-handle is-${side}`}
      style={{
        left: `${position.left}px`,
        top: `${position.top}px`,
        height: `${position.height}px`,
      }}
      aria-label={side === "start" ? "Move selection start" : "Move selection end"}
      title={side === "start" ? "Move selection start" : "Move selection end"}
      onPointerDown={(event) => {
        event.preventDefault();
        event.stopPropagation();
        onPointerDown();
      }}
    >
      <span aria-hidden className="draft-boundary-grip" />
    </button>
  );
}

export function draftFromSelection(selection: Selection | null): SelectionDraft | null {
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return null;
  const range = selection.getRangeAt(0);
  const startLine = lineContentForNode(range.startContainer);
  const endLine = lineContentForNode(range.endContainer);
  if (!startLine || !endLine || startLine.closest(".inline-comment-composer")) return null;

  const startLineNumber = Number(startLine.dataset.lineContent);
  const endLineNumber = Number(endLine.dataset.lineContent);
  if (!Number.isInteger(startLineNumber) || !Number.isInteger(endLineNumber)) return null;

  const selectionTouchesTable = isStructuredRow(startLine) || isStructuredRow(endLine);
  // Table cells omit Markdown pipes, alignment markers, and spacing. Their DOM
  // offsets therefore cannot safely target source characters. Keep the user's
  // exact selected text, but scope table selections to complete source rows.
  const anchor = anchorForCommentSelection(
    startLineNumber,
    textOffset(startLine, range.startContainer, range.startOffset),
    endLineNumber,
    textOffset(endLine, range.endContainer, range.endOffset),
    selectionTouchesTable,
  );
  const quote = selectionTouchesTable
    ? range.toString().trim()
    : textForAnchorContents(startLine, endLine, anchor).trim();
  if (!quote || (!selectionTouchesTable && compareAnchorPoints(anchor.startLine, anchor.startChar ?? 0, anchor.endLine, anchor.endChar ?? 0) === 0)) {
    return null;
  }

  return {
    anchor,
    selectedText: quote,
    body: "",
  };
}

export function selectedTextForAnchorInArticle(root: HTMLElement | null, anchor: Anchor): string {
  if (!root) return "";
  const startContent = lineContent(root, anchor.startLine);
  const endContent = lineContent(root, anchor.endLine);
  if (!startContent || !endContent) return "";
  return textForAnchorContents(startContent, endContent, anchor);
}

function isStructuredRow(line: HTMLElement): boolean {
  return line.dataset.structuredRow === "true";
}

export function restoreNativeSelection(root: HTMLElement | null, anchor: Anchor) {
  if (!root) return;
  const start = lineContent(root, anchor.startLine);
  const end = lineContent(root, anchor.endLine);
  if (!start || !end) return;
  const range = document.createRange();
  setBoundary(range, "start", start, anchor.startChar ?? 0);
  setBoundary(range, "end", end, anchor.endChar ?? 0);
  const selection = window.getSelection();
  if (!selection) return;
  selection.removeAllRanges();
  selection.addRange(range);
}

function lineContentForNode(node: Node): HTMLElement | null {
  const element = node.nodeType === Node.ELEMENT_NODE ? (node as Element) : node.parentElement;
  return element?.closest<HTMLElement>("[data-line-content]") ?? null;
}

function textOffset(container: HTMLElement, node: Node, offset: number): number {
  const range = document.createRange();
  range.selectNodeContents(container);
  range.setEnd(node, offset);
  return range.toString().length;
}

interface BoundaryHandlePosition {
  left: number;
  top: number;
  height: number;
}

interface AnchorPoint {
  line: number;
  char: number;
}

function textForAnchorContents(startContent: HTMLElement, endContent: HTMLElement, anchor: Anchor): string {
  const root = startContent.closest<HTMLElement>("article");
  const parts: string[] = [];
  for (let line = anchor.startLine; line <= anchor.endLine; line++) {
    const content =
      line === anchor.startLine
        ? startContent
        : line === anchor.endLine
          ? endContent
          : root
            ? lineContent(root, line)
            : null;
    const text = content?.textContent ?? "";
    if (line === anchor.startLine && line === anchor.endLine) {
      parts.push(text.slice(anchor.startChar ?? 0, anchor.endChar ?? text.length));
    } else if (line === anchor.startLine) {
      parts.push(text.slice(anchor.startChar ?? 0));
    } else if (line === anchor.endLine) {
      parts.push(text.slice(0, anchor.endChar ?? text.length));
    } else {
      parts.push(text);
    }
  }
  return parts.join("\n");
}

function materializeCharacterAnchor(root: HTMLElement, anchor: Anchor): Anchor {
  const endContent = lineContent(root, anchor.endLine);
  return {
    startLine: anchor.startLine,
    startChar: anchor.startChar ?? 0,
    endLine: anchor.endLine,
    endChar: anchor.endChar ?? textLength(endContent),
  };
}

function moveAnchorBoundary(
  root: HTMLElement,
  anchor: Anchor,
  side: "start" | "end",
  point: AnchorPoint,
): Anchor {
  const current = materializeCharacterAnchor(root, anchor);
  const next =
    side === "start"
      ? { ...current, startLine: point.line, startChar: point.char }
      : { ...current, endLine: point.line, endChar: point.char };
  const order = compareAnchorPoints(next.startLine, next.startChar ?? 0, next.endLine, next.endChar ?? 0);
  if (order === 0) return current;
  if (order < 0) return next;
  return {
    startLine: next.endLine,
    startChar: next.endChar,
    endLine: next.startLine,
    endChar: next.startChar,
  };
}

function boundaryHandlePosition(
  root: HTMLElement,
  lineNumber: number,
  char: number,
): BoundaryHandlePosition | null {
  const content = lineContent(root, lineNumber);
  if (!content) return null;

  const range = document.createRange();
  setBoundary(range, "start", content, clamp(char, 0, textLength(content)));
  range.collapse(true);

  const rect = range.getClientRects()[0];
  const contentRect = content.getBoundingClientRect();
  const rootRect = root.getBoundingClientRect();
  const fallbackLeft = char > 0 ? contentRect.right : contentRect.left;

  return {
    left: (rect?.left ?? fallbackLeft) - rootRect.left,
    top: (rect?.top ?? contentRect.top) - rootRect.top,
    height: rect?.height || contentRect.height || 22,
  };
}

function anchorPointFromClientPosition(
  root: HTMLElement,
  clientX: number,
  clientY: number,
): AnchorPoint | null {
  const directPoint = caretPointFromClientPosition(clientX, clientY);
  const directContent = directPoint ? lineContentForNode(directPoint.node) : null;
  if (directPoint && directContent && root.contains(directContent)) {
    return anchorPointForContent(directContent, directPoint.node, directPoint.offset);
  }

  const fallbackContent = lineContentFromClientPosition(root, clientX, clientY);
  if (!fallbackContent) return null;
  const rect = fallbackContent.getBoundingClientRect();
  const clampedX = clamp(clientX, rect.left + 1, Math.max(rect.left + 1, rect.right - 1));
  const clampedY = clamp(clientY, rect.top + 1, Math.max(rect.top + 1, rect.bottom - 1));
  const fallbackPoint = caretPointFromClientPosition(clampedX, clampedY);
  const fallbackPointContent = fallbackPoint ? lineContentForNode(fallbackPoint.node) : null;
  if (fallbackPoint && fallbackPointContent === fallbackContent) {
    return anchorPointForContent(fallbackContent, fallbackPoint.node, fallbackPoint.offset);
  }

  return {
    line: Number(fallbackContent.dataset.lineContent),
    char: clientX <= rect.left ? 0 : textLength(fallbackContent),
  };
}

function anchorPointForContent(content: HTMLElement, node: Node, offset: number): AnchorPoint | null {
  const line = Number(content.dataset.lineContent);
  if (!Number.isInteger(line)) return null;
  return {
    line,
    char: clamp(textOffset(content, node, offset), 0, textLength(content)),
  };
}

function lineContentFromClientPosition(
  root: HTMLElement,
  clientX: number,
  clientY: number,
): HTMLElement | null {
  const element = document.elementFromPoint(clientX, clientY);
  if (element && root.contains(element)) {
    const direct = element.closest<HTMLElement>("[data-line-content]");
    if (direct) return direct;
    const row = element.closest<HTMLElement>("[data-line]");
    const rowContent = row?.querySelector<HTMLElement>("[data-line-content]");
    if (rowContent) return rowContent;
  }

  let closest: HTMLElement | null = null;
  let closestDistance = Number.POSITIVE_INFINITY;
  for (const row of root.querySelectorAll<HTMLElement>("[data-line]")) {
    const rect = row.getBoundingClientRect();
    const distance =
      clientY < rect.top ? rect.top - clientY : clientY > rect.bottom ? clientY - rect.bottom : 0;
    if (distance < closestDistance) {
      closestDistance = distance;
      closest = row.querySelector<HTMLElement>("[data-line-content]");
    }
  }
  return closest;
}

function caretPointFromClientPosition(clientX: number, clientY: number): { node: Node; offset: number } | null {
  const caretDocument = document as Document & {
    caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node; offset: number } | null;
    caretRangeFromPoint?: (x: number, y: number) => Range | null;
  };
  const position = caretDocument.caretPositionFromPoint?.(clientX, clientY);
  if (position) {
    return { node: position.offsetNode, offset: position.offset };
  }
  const range = caretDocument.caretRangeFromPoint?.(clientX, clientY);
  if (range) {
    return { node: range.startContainer, offset: range.startOffset };
  }
  return null;
}

function textLength(content: HTMLElement | null): number {
  return content?.textContent?.length ?? 0;
}

function compareAnchorPoints(startLine: number, startChar: number, endLine: number, endChar: number): number {
  if (startLine !== endLine) return startLine - endLine;
  return startChar - endChar;
}

function clamp(value: number, min: number, max: number): number {
  if (max < min) return min;
  return Math.min(max, Math.max(min, value));
}

export function usePlanHighlights(
  articleRef: React.RefObject<HTMLElement>,
  threads: Thread[],
  hoveredAnchor: Anchor | null,
  draftAnchor: Anchor | null,
) {
  useEffect(() => {
    const root = articleRef.current;
    const highlights = (CSS as typeof CSS & { highlights?: HighlightRegistry }).highlights;
    const HighlightClass = (window as Window & { Highlight?: HighlightConstructor }).Highlight;
    if (!root || !highlights || !HighlightClass) return;
    ensureHighlightStyles();

    const threadRanges = threads
      .filter((thread) => thread.lifecycle === "active")
      .flatMap((thread) => rangesForAnchor(root, thread.anchor));
    const draftRanges = draftAnchor ? rangesForAnchor(root, draftAnchor) : [];
    const hoverRanges = hoveredAnchor ? rangesForAnchor(root, hoveredAnchor) : [];

    highlights.set("planmaxx-comment-anchor", new HighlightClass(...threadRanges));
    highlights.set("planmaxx-draft-anchor", new HighlightClass(...draftRanges));
    highlights.set("planmaxx-hover-anchor", new HighlightClass(...hoverRanges));

    return () => {
      highlights.delete("planmaxx-comment-anchor");
      highlights.delete("planmaxx-draft-anchor");
      highlights.delete("planmaxx-hover-anchor");
    };
  }, [articleRef, threads, hoveredAnchor, draftAnchor]);
}

function ensureHighlightStyles() {
  if (document.getElementById("planmaxx-highlight-styles")) return;
  const style = document.createElement("style");
  style.id = "planmaxx-highlight-styles";
  style.textContent = `
    ::highlight(planmaxx-comment-anchor) {
      background: color-mix(in srgb, var(--color-accent) 16%, transparent);
    }
    ::highlight(planmaxx-draft-anchor) {
      background: color-mix(in srgb, var(--color-warning) 32%, transparent);
    }
    ::highlight(planmaxx-hover-anchor) {
      background: color-mix(in srgb, var(--color-accent) 28%, transparent);
    }
  `;
  document.head.appendChild(style);
}

function rangesForAnchor(root: HTMLElement, anchor: Anchor): Range[] {
  const ranges: Range[] = [];
  const startChar = anchor.startChar ?? 0;
  const endChar = anchor.endChar ?? 0;
  const hasCharRange = startChar !== 0 || endChar !== 0;

  if (hasCharRange) {
    const start = lineContent(root, anchor.startLine);
    const end = lineContent(root, anchor.endLine);
    if (!start || !end) return ranges;
    const range = document.createRange();
    setBoundary(range, "start", start, startChar);
    setBoundary(range, "end", end, endChar);
    ranges.push(range);
    return ranges;
  }

  for (let line = anchor.startLine; line <= anchor.endLine; line++) {
    const content = lineContent(root, line);
    if (!content) continue;
    const range = document.createRange();
    range.selectNodeContents(content);
    ranges.push(range);
  }
  return ranges;
}

function lineContent(root: HTMLElement, lineNumber: number): HTMLElement | null {
  return root.querySelector<HTMLElement>(`[data-line-content="${lineNumber}"]`);
}

function setBoundary(
  range: Range,
  side: "start" | "end",
  container: HTMLElement,
  offset: number,
) {
  let remaining = Math.max(0, offset);
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
  let node = walker.nextNode();
  while (node) {
    const length = node.textContent?.length ?? 0;
    if (remaining <= length) {
      if (side === "start") range.setStart(node, remaining);
      else range.setEnd(node, remaining);
      return;
    }
    remaining -= length;
    node = walker.nextNode();
  }
  if (side === "start") range.setStart(container, container.childNodes.length);
  else range.setEnd(container, container.childNodes.length);
}

interface HighlightRegistry {
  set: (name: string, highlight: unknown) => void;
  delete: (name: string) => void;
}

type HighlightConstructor = new (...ranges: Range[]) => unknown;
