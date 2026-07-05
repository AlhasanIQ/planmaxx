import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { MessageSquarePlus, Sparkles } from "lucide-react";
import { renderPlanLines } from "../lib/markdown";
import type { Anchor, Thread } from "../types";
import { anchorLabel, anchorTouchesLine } from "../lib/anchors";
import { inlineCommentComposerPlacement } from "../lib/commentPlacement";

interface PlanProps {
  plan: string;
  threads: Thread[];
  hoveredThreadId: string | null;
  focusedThreadId: string | null;
  editingThread: Thread | null;
  onCreateComment: (anchor: Anchor, body: string, selectedText: string) => Promise<boolean>;
  onUpdateComment: (threadId: string, anchor: Anchor, body: string, selectedText: string) => Promise<boolean>;
  onAskSideFromDraft: (anchor: Anchor, body: string, selectedText: string) => Promise<boolean>;
  onIterateDraft: (anchor: Anchor, instruction: string) => Promise<boolean>;
  onEditDone: () => void;
  onFocusThread: (threadId: string) => void;
}

interface CommentDraft {
  threadId?: string;
  anchor: Anchor;
  selectedText: string;
  body: string;
}

export const Plan = memo(function Plan({
  plan,
  threads,
  hoveredThreadId,
  focusedThreadId,
  editingThread,
  onCreateComment,
  onUpdateComment,
  onAskSideFromDraft,
  onIterateDraft,
  onEditDone,
  onFocusThread,
}: PlanProps) {
  const articleRef = useRef<HTMLElement>(null);
  const lines = useMemo(() => renderPlanLines(plan), [plan]);
  const [draft, setDraft] = useState<CommentDraft | null>(null);
  const [submittingDraft, setSubmittingDraft] = useState(false);

  const hoveredAnchor = useMemo(() => {
    if (!hoveredThreadId) return null;
    const t = threads.find((x) => x.id === hoveredThreadId);
    return t?.anchor ?? null;
  }, [hoveredThreadId, threads]);
  const focusedAnchor = useMemo(() => {
    if (!focusedThreadId) return null;
    const t = threads.find((x) => x.id === focusedThreadId);
    return t?.anchor ?? null;
  }, [focusedThreadId, threads]);
  const activeAnchor = hoveredAnchor ?? focusedAnchor;

  usePlanHighlights(articleRef, threads, activeAnchor, draft?.anchor ?? null);

  useEffect(() => {
    if (!editingThread) return;
    setDraft({
      threadId: editingThread.id,
      anchor: editingThread.anchor,
      selectedText:
        editingThread.selectedText || selectedTextForAnchorInArticle(articleRef.current, editingThread.anchor),
      body: editingThread.messages[0]?.body ?? "",
    });
  }, [editingThread]);

  // Map line -> first thread id anchored to it (for "go to thread" affordance).
  const lineToThread = useMemo(() => {
    const map = new Map<number, string>();
    for (const t of threads) {
      for (let i = t.anchor.startLine; i <= t.anchor.endLine; i++) {
        if (!map.has(i)) map.set(i, t.id);
      }
    }
    return map;
  }, [threads]);

  function openFullLineDraft(lineNumber: number) {
    const anchor = { startLine: lineNumber, endLine: lineNumber };
    setDraft({
      anchor,
      selectedText: selectedTextForAnchorInArticle(articleRef.current, anchor),
      body: "",
    });
  }

  function handlePointerUp(e: React.PointerEvent<HTMLElement>) {
    if ((e.target as HTMLElement | null)?.closest(".inline-comment-composer")) return;
    if ((e.target as HTMLElement | null)?.closest(".draft-boundary-handle")) return;
    const selection = window.getSelection();
    const next = draftFromSelection(selection);
    if (!next) return;
    setDraft(next);
    selection?.removeAllRanges();
  }

  function currentSelectedText(current: CommentDraft): string {
    return selectedTextForAnchorInArticle(articleRef.current, current.anchor) || current.selectedText;
  }

  async function submitDraft() {
    if (!draft || !draft.body.trim()) return;
    setSubmittingDraft(true);
    try {
      const ok = draft.threadId
        ? await onUpdateComment(draft.threadId, draft.anchor, draft.body.trim(), currentSelectedText(draft))
        : await onCreateComment(draft.anchor, draft.body.trim(), currentSelectedText(draft));
      if (!ok) return;
      if (draft.threadId) onEditDone();
      setDraft(null);
    } finally {
      setSubmittingDraft(false);
    }
  }

  async function askSideFromDraft() {
    if (!draft || draft.threadId || !draft.body.trim()) return;
    setSubmittingDraft(true);
    try {
      const ok = await onAskSideFromDraft(draft.anchor, draft.body.trim(), currentSelectedText(draft));
      if (!ok) return;
      setDraft(null);
    } finally {
      setSubmittingDraft(false);
    }
  }

  async function iterateDraft() {
    if (!draft || draft.threadId || !draft.body.trim()) return;
    setSubmittingDraft(true);
    try {
      const ok = await onIterateDraft(draft.anchor, draft.body.trim());
      if (!ok) return;
      setDraft(null);
    } finally {
      setSubmittingDraft(false);
    }
  }

  function cancelDraft() {
    setDraft(null);
    if (draft?.threadId) onEditDone();
  }

  const updateDraftAnchor = useCallback((anchor: Anchor) => {
    setDraft((current) =>
      current
        ? {
            ...current,
            anchor,
            selectedText: selectedTextForAnchorInArticle(articleRef.current, anchor),
          }
        : current,
    );
  }, []);
  const draftComposerPlacement = draft
    ? inlineCommentComposerPlacement(draft.anchor.endLine, lines.length)
    : null;

  return (
    <article
      ref={articleRef}
      className="relative overflow-hidden rounded-[var(--radius-card)] border border-border bg-surface-elevated shadow-[var(--shadow-soft)]"
      onPointerUp={handlePointerUp}
    >
      <div className="plan-body py-2">
        {lines.map((line, idx) => {
          const lineNumber = idx + 1;
          const inDraft = draft ? anchorTouchesLine(draft.anchor, lineNumber) : false;
          const inHoverAnchor = activeAnchor && anchorTouchesLine(activeAnchor, lineNumber);
          const anchoredThreadId = lineToThread.get(lineNumber);
          return (
            <div key={lineNumber}>
              <div
                data-line={lineNumber}
                className={`line-row${line.kind === "blank" ? " is-blank" : ""}${
                  inDraft ? " is-anchored" : ""
                }${inHoverAnchor ? " is-hover-anchor" : ""}`}
              >
                <div className="line-number">{lineNumber}</div>
                <div className="pin-cell">
                  <button
                    type="button"
                    className={`pin-btn${anchoredThreadId ? " has-anchor" : ""}`}
                    title={
                      anchoredThreadId
                        ? "Open existing thread"
                        : `Comment on line ${lineNumber}`
                    }
                    aria-label={
                      anchoredThreadId
                        ? "Open existing thread"
                        : `Comment on line ${lineNumber}`
                    }
                    onMouseDown={(event) => event.stopPropagation()}
                    onClick={(event) => {
                      event.stopPropagation();
                      if (anchoredThreadId) {
                        onFocusThread(anchoredThreadId);
                      } else {
                        openFullLineDraft(lineNumber);
                      }
                    }}
                  >
                    <MessageSquarePlus size={14} />
                  </button>
                </div>
                <PlanLineContent
                  html={line.html}
                  lineNumber={lineNumber}
                  anchoredThreadId={anchoredThreadId}
                  onFocusThread={onFocusThread}
                />
              </div>

              {draft && draftComposerPlacement?.afterLine === lineNumber ? (
                <InlineCommentComposer
                  draft={draft}
                  spacerLines={draftComposerPlacement.spacerLines}
                  submitting={submittingDraft}
                  setDraft={setDraft}
                  onCancel={cancelDraft}
                  onSubmit={submitDraft}
                  onAskSide={askSideFromDraft}
                  onIterate={iterateDraft}
                />
              ) : null}
            </div>
          );
        })}
      </div>
      <DraftBoundaryHandles
        articleRef={articleRef}
        anchor={draft?.anchor ?? null}
        onChange={updateDraftAnchor}
      />
    </article>
  );
});

function InlineCommentComposer({
  draft,
  spacerLines,
  submitting,
  setDraft,
  onCancel,
  onSubmit,
  onAskSide,
  onIterate,
}: {
  draft: CommentDraft;
  spacerLines: number;
  submitting: boolean;
  setDraft: (draft: CommentDraft) => void;
  onCancel: () => void;
  onSubmit: () => void;
  onAskSide: () => void;
  onIterate: () => void;
}) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const canSubmit = draft.body.trim().length > 0;
  const isEditing = Boolean(draft.threadId);

  useEffect(() => {
    textareaRef.current?.focus();
  }, []);

  function onBodyKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      onSubmit();
    }
    if (e.key === "Escape") {
      e.preventDefault();
      onCancel();
    }
  }

  return (
    <div
      className="inline-comment-composer"
      style={spacerLines > 0 ? { marginTop: `calc(8px + ${spacerLines} * 1.7em)` } : undefined}
    >
      <div className="inline-comment-header">
        <span>{anchorLabel(draft.anchor)}</span>
      </div>
      <label className="block text-xs font-semibold text-foreground-muted">
        Comment
        <textarea
          ref={textareaRef}
          value={draft.body}
          onChange={(e) => setDraft({ ...draft, body: e.target.value })}
          onKeyDown={onBodyKeyDown}
          rows={3}
          placeholder="Leave a comment for this selection..."
          className="field mt-1 resize-y font-sans"
        />
      </label>
      <div className="flex justify-end gap-2">
        <button type="button" className="btn" onClick={onCancel}>
          Cancel
        </button>
        <button
          type="button"
          className="btn btn-primary"
          onClick={onSubmit}
          disabled={!canSubmit || submitting}
        >
          {submitting ? "Saving..." : isEditing ? "Save comment" : "Add comment"}
        </button>
        {!isEditing ? (
          <button
            type="button"
            className="btn"
            onClick={onAskSide}
            disabled={!canSubmit || submitting}
            title="Save this comment and ask Codex about the selected text on the side"
          >
            /btw
          </button>
        ) : null}
        {!isEditing ? (
          <button
            type="button"
            className="btn"
            onClick={onIterate}
            disabled={!canSubmit || submitting}
            title="Ask Codex to rewrite only the selected section"
          >
            <Sparkles size={13} /> Iterate section
          </button>
        ) : null}
      </div>
    </div>
  );
}

const PlanLineContent = memo(function PlanLineContent({
  html,
  lineNumber,
  anchoredThreadId,
  onFocusThread,
}: {
  html: string;
  lineNumber: number;
  anchoredThreadId: string | undefined;
  onFocusThread: (threadId: string) => void;
}) {
  const content = useMemo(() => renderInlineNodes(html || "&nbsp;"), [html]);

  function activate(event: React.MouseEvent | React.KeyboardEvent) {
    if (!anchoredThreadId) return;
    const selection = window.getSelection();
    if (selection && !selection.isCollapsed) return;
    event.stopPropagation();
    onFocusThread(anchoredThreadId);
  }

  function onKeyDown(event: React.KeyboardEvent) {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    activate(event);
  }

  if (anchoredThreadId) {
    const anchoredLineProps = {
      onClick: activate,
      onKeyDown,
      role: "button" as const,
      tabIndex: 0,
    };

    return (
      <div
        className="line-content"
        data-line-content={lineNumber}
        {...anchoredLineProps}
      >
        {content}
      </div>
    );
  }

  return (
    <div className="line-content" data-line-content={lineNumber}>
      {content}
    </div>
  );
});

function renderInlineNodes(html: string): React.ReactNode {
  if (typeof document === "undefined") {
    return html;
  }
  const template = document.createElement("template");
  template.innerHTML = html;
  return Array.from(template.content.childNodes).map((node, index) =>
    inlineNodeToReact(node, `inline-${index}`),
  );
}

function inlineNodeToReact(node: ChildNode, key: string): React.ReactNode {
  if (node.nodeType === Node.TEXT_NODE) {
    return node.textContent;
  }
  if (node.nodeType !== Node.ELEMENT_NODE) {
    return null;
  }

  const element = node as HTMLElement;
  const children = Array.from(element.childNodes).map((child, index) =>
    inlineNodeToReact(child, `${key}-${index}`),
  );
  switch (element.tagName.toLowerCase()) {
    case "a": {
      const href = element.getAttribute("href") ?? "";
      const title = element.getAttribute("title") ?? undefined;
      return (
        <a key={key} href={href} title={title}>
          {children}
        </a>
      );
    }
    case "br":
      return <br key={key} />;
    case "code":
      return <code key={key}>{children}</code>;
    case "del":
      return <del key={key}>{children}</del>;
    case "em":
      return <em key={key}>{children}</em>;
    case "strong":
      return <strong key={key}>{children}</strong>;
    case "span":
      return (
        <span
          key={key}
          className={element.getAttribute("class") ?? undefined}
          style={spanStyle(element.getAttribute("style"))}
        >
          {children}
        </span>
      );
    default:
      return <span key={key}>{children}</span>;
  }
}

function spanStyle(style: string | null): React.CSSProperties | undefined {
  const match = /^padding-left:\s*(\d+)px$/i.exec(style ?? "");
  if (!match) return undefined;
  return { paddingLeft: `${match[1]}px` };
}

function DraftBoundaryHandles({
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

function draftFromSelection(selection: Selection | null): CommentDraft | null {
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return null;
  const range = selection.getRangeAt(0);
  const startLine = lineContentForNode(range.startContainer);
  const endLine = lineContentForNode(range.endContainer);
  if (!startLine || !endLine || startLine.closest(".inline-comment-composer")) return null;

  const startLineNumber = Number(startLine.dataset.lineContent);
  const endLineNumber = Number(endLine.dataset.lineContent);
  if (!Number.isInteger(startLineNumber) || !Number.isInteger(endLineNumber)) return null;

  const startChar = textOffset(startLine, range.startContainer, range.startOffset);
  const endChar = textOffset(endLine, range.endContainer, range.endOffset);
  const anchor = {
    startLine: startLineNumber,
    startChar,
    endLine: endLineNumber,
    endChar,
  };
  const quote = textForAnchorContents(startLine, endLine, anchor).trim();
  if (!quote || compareAnchorPoints(anchor.startLine, anchor.startChar, anchor.endLine, anchor.endChar) === 0) {
    return null;
  }

  return {
    anchor,
    selectedText: quote,
    body: "",
  };
}

function selectedTextForAnchorInArticle(root: HTMLElement | null, anchor: Anchor): string {
  if (!root) return "";
  const startContent = lineContent(root, anchor.startLine);
  const endContent = lineContent(root, anchor.endLine);
  if (!startContent || !endContent) return "";
  return textForAnchorContents(startContent, endContent, anchor);
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

function usePlanHighlights(
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

    const threadRanges = threads.flatMap((thread) => rangesForAnchor(root, thread.anchor));
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
