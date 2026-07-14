import { memo, useMemo } from "react";
import type React from "react";
import type { HighlightToken } from "../lib/codeHighlight";

export const RenderedLine = memo(function RenderedLine({
  html, lineNumber, isTableRow, anchoredThreadId, codeTokens, onFocusThread,
}: {
  html: string;
  lineNumber?: number;
  isTableRow: boolean;
  anchoredThreadId: string | undefined;
  codeTokens?: HighlightToken[];
  onFocusThread: (threadId: string) => void;
}) {
  const content = useMemo(() => codeTokens
    ? codeTokens.map((token, index) => (
      <span key={index} style={{ color: token.color, fontStyle: token.fontStyle === 1 ? "italic" : undefined, fontWeight: token.fontStyle === 2 ? 700 : undefined, textDecoration: token.fontStyle === 4 ? "underline" : undefined }}>
        {token.content}
      </span>
    ))
    : renderInlineNodes(html || "&nbsp;"), [codeTokens, html]);

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

  const anchorProps = anchoredThreadId ? { onClick: activate, onKeyDown, role: "button" as const, tabIndex: 0 } : {};
  return (
    <div className="line-content" data-line-content={lineNumber} data-structured-row={isTableRow || undefined} {...anchorProps}>
      {content}
    </div>
  );
});

function renderInlineNodes(html: string): React.ReactNode {
  if (typeof document === "undefined") return html;
  const template = document.createElement("template");
  template.innerHTML = html;
  return Array.from(template.content.childNodes).map((node, index) => inlineNodeToReact(node, `inline-${index}`));
}

function inlineNodeToReact(node: ChildNode, key: string): React.ReactNode {
  if (node.nodeType === Node.TEXT_NODE) return node.textContent;
  if (node.nodeType !== Node.ELEMENT_NODE) return null;
  const element = node as HTMLElement;
  const children = Array.from(element.childNodes).map((child, index) => inlineNodeToReact(child, `${key}-${index}`));
  switch (element.tagName.toLowerCase()) {
    case "a":
      return <a key={key} href={element.getAttribute("href") ?? ""} title={element.getAttribute("title") ?? undefined}>{children}</a>;
    case "br": return <br key={key} />;
    case "code": return <code key={key}>{children}</code>;
    case "del": return <del key={key}>{children}</del>;
    case "em": return <em key={key}>{children}</em>;
    case "strong": return <strong key={key}>{children}</strong>;
    case "span":
      return <span key={key} className={element.getAttribute("class") ?? undefined} style={spanStyle(element.getAttribute("style"))}>{children}</span>;
    default: return <span key={key}>{children}</span>;
  }
}

function spanStyle(style: string | null): React.CSSProperties | undefined {
  const padding = /^padding-left:\s*(\d+)px$/i.exec(style ?? "");
  if (padding) return { paddingLeft: `${padding[1]}px` };
  const columns = /^grid-template-columns:repeat\((\d+),\s*minmax\(9rem,\s*1fr\)\);min-width:(\d+)px$/i.exec(style ?? "");
  if (columns) return { gridTemplateColumns: `repeat(${columns[1]}, minmax(9rem, 1fr))`, minWidth: `${columns[2]}px` };
  return undefined;
}
