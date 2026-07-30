// This script is the only executable code allowed inside an HTML-plan preview.
// Authored scripts and event handlers are removed before it is injected, and a
// per-render CSP nonce prevents the reviewed document from adding executable
// code of its own. The iframe intentionally remains an opaque origin; all
// review interaction crosses the boundary through this small message protocol.
export const htmlPreviewBridgeScript = String.raw`
(() => {
  "use strict";

  const config = window.__PLANMAXX_PREVIEW__ || { lineStarts: [0] };
  const markerPattern = /^planmaxx-text:(\d+):(\d+):(\d+):(0|1)$/;
  const textEntries = [];
  let annotations = [];
  let disabled = false;
  let hoverTarget = null;
  let shadow = null;
  let overlayRoot = null;
  let renderFrame = 0;
  let positionFrame = 0;
  let layoutFrame = 0;
  let lastPositionOffset = -1;
  let pingedAnnotationId = null;
  let pingTimer = 0;
  const inlineSlots = new Map();

  function sourcePosition(offset) {
    const starts = config.lineStarts || [0];
    let low = 0;
    let high = starts.length - 1;
    while (low <= high) {
      const mid = (low + high) >> 1;
      if (starts[mid] <= offset) low = mid + 1;
      else high = mid - 1;
    }
    const index = Math.max(0, high);
    return { line: index + 1, char: Math.max(0, offset - starts[index]) };
  }

  function anchorFromOffsets(start, end) {
    const startOffset = Math.max(0, Number(start) || 0);
    const first = sourcePosition(startOffset);
    const last = sourcePosition(Math.max(startOffset, Number(end) || 0));
    return {
      startLine: first.line,
      startChar: first.char,
      endLine: last.line,
      endChar: last.char,
    };
  }

  function registerTextSources() {
    const walker = document.createTreeWalker(document, NodeFilter.SHOW_COMMENT);
    let comment;
    while ((comment = walker.nextNode())) {
      const match = markerPattern.exec(comment.data || "");
      const text = comment.nextSibling;
      if (!match || !text || text.nodeType !== Node.TEXT_NODE) continue;
      const source = {
        id: match[1],
        start: Number(match[2]),
        end: Number(match[3]),
        exact: match[4] === "1",
      };
      textEntries.push({ node: text, source });
    }
  }

  function sourceSpanForElement(element) {
    const raw = element && element.getAttribute && element.getAttribute("data-planmaxx-source");
    const match = raw && /^(\d+):(\d+)$/.exec(raw);
    return match ? { start: Number(match[1]), end: Number(match[2]) } : null;
  }

  function sourceSpanForRange(range) {
    const entries = [];
    for (const entry of textEntries) {
      const { node, source } = entry;
      try {
        if (!range.intersectsNode(node)) continue;
      } catch {
        continue;
      }
      let startOffset = 0;
      let endOffset = node.data.length;
      if (node === range.startContainer) startOffset = range.startOffset;
      if (node === range.endContainer) endOffset = range.endOffset;
      if (startOffset === endOffset && node === range.startContainer && node === range.endContainer) continue;
      entries.push({ node, source, startOffset, endOffset });
    }
    if (entries.length === 0) return null;
    const first = entries[0];
    const last = entries[entries.length - 1];
    const firstMapping = config.textMappings && config.textMappings[first.source.id];
    const lastMapping = config.textMappings && config.textMappings[last.source.id];
    return {
      start: first.source.exact
        ? first.source.start + first.startOffset
        : firstMapping && firstMapping[first.startOffset] !== undefined
          ? firstMapping[first.startOffset]
          : first.source.start,
      end: last.source.exact
        ? last.source.start + last.endOffset
        : lastMapping && lastMapping[last.endOffset] !== undefined
          ? lastMapping[last.endOffset]
          : last.source.end,
    };
  }

  function post(type, payload = {}) {
    parent.postMessage({ type, ...payload }, "*");
  }

  function ensureOverlay() {
    if (overlayRoot) return overlayRoot;
    const host = document.createElement("div");
    host.setAttribute("data-planmaxx-review-ui", "");
    host.style.setProperty("all", "initial", "important");
    host.style.setProperty("position", "fixed", "important");
    host.style.setProperty("inset", "0", "important");
    host.style.setProperty("z-index", "2147483647", "important");
    host.style.setProperty("pointer-events", "none", "important");
    document.documentElement.appendChild(host);
    shadow = host.attachShadow({ mode: "closed" });
    const style = document.createElement("style");
    style.textContent =
      ":host{all:initial}.root{position:fixed;inset:0;pointer-events:none}" +
      ".mark{position:fixed;box-sizing:border-box;border-radius:3px;background:rgba(65,105,225,.13);box-shadow:inset 0 -2px rgba(65,105,225,.72)}" +
      ".mark.is-active{background:rgba(65,105,225,.25);box-shadow:inset 0 -3px #4169e1,0 0 0 1px rgba(65,105,225,.24)}" +
      ".mark.is-draft{background:rgba(217,119,6,.2);box-shadow:inset 0 -3px #d97706}" +
      ".mark.is-ping{animation:planmaxx-target-ping 720ms cubic-bezier(.2,.8,.2,1) both}" +
      ".target{position:fixed;box-sizing:border-box;border:2px solid #60a5fa;border-radius:5px;background:rgba(96,165,250,.08);box-shadow:0 0 0 1px rgba(15,23,42,.28),0 2px 9px rgba(15,23,42,.18)}" +
      ".target.is-existing{border-color:#34d399;background:rgba(52,211,153,.09)}" +
      ".target-label{position:fixed;box-sizing:border-box;max-width:min(240px,calc(100vw - 8px));overflow:hidden;border-radius:5px;padding:3px 7px;background:#172033;color:#f8fafc;box-shadow:0 2px 8px rgba(15,23,42,.3);font:600 11px/1.35 ui-sans-serif,system-ui,sans-serif;letter-spacing:.01em;text-overflow:ellipsis;white-space:nowrap}" +
      ".target-label.is-existing{background:#065f46}" +
      "@keyframes planmaxx-target-ping{0%{transform:scale(1);filter:brightness(1)}28%{transform:scale(1.045);filter:brightness(1.35);box-shadow:inset 0 -3px #4169e1,0 0 0 5px rgba(65,105,225,.28)}100%{transform:scale(1);filter:brightness(1)}}";
    shadow.appendChild(style);
    overlayRoot = document.createElement("div");
    overlayRoot.className = "root";
    shadow.appendChild(overlayRoot);
    return overlayRoot;
  }

  function textRangesForSourceSpan(span) {
    const ranges = [];
    for (const entry of textEntries) {
      const { node, source } = entry;
      if (source.end <= span.start || source.start >= span.end) continue;
      const range = document.createRange();
      let start = 0;
      let end = node.data.length;
      if (source.exact) {
        start = Math.max(0, Math.min(node.data.length, span.start - source.start));
        end = Math.max(start, Math.min(node.data.length, span.end - source.start));
      } else {
        const mapping = config.textMappings && config.textMappings[source.id];
        if (mapping) {
          start = 0;
          for (let index = 1; index < mapping.length && mapping[index] <= span.start; index++) {
            start = index;
          }
          end = mapping.findIndex((offset) => offset >= span.end);
          if (end < 0) {
            end = node.data.length;
          } else {
            while (end + 1 < mapping.length && mapping[end + 1] === mapping[end]) end++;
          }
        }
      }
      if (start === end) continue;
      range.setStart(node, start);
      range.setEnd(node, end);
      ranges.push(range);
    }
    return ranges;
  }

  function elementForSourceSpan(span) {
    let best = null;
    let bestSize = Number.POSITIVE_INFINITY;
    for (const element of document.querySelectorAll("[data-planmaxx-source]")) {
      const source = sourceSpanForElement(element);
      if (!source || source.start > span.start || source.end < span.end) continue;
      const size = source.end - source.start;
      if (size < bestSize) {
        best = element;
        bestSize = size;
      }
    }
    return best;
  }

  function rectsForSourceSpan(span) {
    const rects = [];
    for (const range of textRangesForSourceSpan(span)) {
      rects.push(...range.getClientRects());
    }
    if (rects.some((rect) => rect.width > 0 && rect.height > 0)) return rects;
    const element = elementForSourceSpan(span);
    return element ? [element.getBoundingClientRect()] : [];
  }

  function rectsForElement(element) {
    if (!element) return [];
    let rects = [...element.getClientRects()];
    if (!rects.some((rect) => rect.width > 0 || rect.height > 0)) {
      try {
        const range = document.createRange();
        range.selectNodeContents(element);
        rects = [...range.getClientRects()];
      } catch {
        rects = [];
      }
    }
    if (!rects.some((rect) => rect.width > 0 || rect.height > 0)) {
      rects = [element.getBoundingClientRect()];
    }
    return rects.map(normalizeTargetRect).filter(Boolean);
  }

  function normalizeTargetRect(rect) {
    if (rect.bottom < 0 || rect.top > window.innerHeight || rect.right < 0 || rect.left > window.innerWidth) return null;
    const centerX = (rect.left + rect.right) / 2;
    const centerY = (rect.top + rect.bottom) / 2;
    const width = Math.max(4, rect.width);
    const height = Math.max(4, rect.height);
    const left = Math.max(1, Math.min(window.innerWidth - 1, centerX - width / 2));
    const top = Math.max(1, Math.min(window.innerHeight - 1, centerY - height / 2));
    return {
      left,
      top,
      right: Math.min(window.innerWidth - 1, left + width),
      bottom: Math.min(window.innerHeight - 1, top + height),
      width: Math.max(1, Math.min(window.innerWidth - 1, left + width) - left),
      height: Math.max(1, Math.min(window.innerHeight - 1, top + height) - top),
    };
  }

  function appendRect(className, rect) {
    if (!overlayRoot || rect.width <= 0 || rect.height <= 0) return;
    const mark = document.createElement("div");
    mark.className = className;
    mark.style.left = rect.left + "px";
    mark.style.top = rect.top + "px";
    mark.style.width = rect.width + "px";
    mark.style.height = rect.height + "px";
    overlayRoot.appendChild(mark);
  }

  function appendHoverTarget(element) {
    if (!overlayRoot || !element) return;
    const span = sourceSpanForElement(element);
    const existing = span && matchingAnnotation(span);
    const rects = rectsForElement(element);
    const className = "target" + (existing ? " is-existing" : "");
    for (const rect of rects) appendRect(className, rect);
    const first = rects[0];
    if (!first) return;
    const label = document.createElement("div");
    label.className = "target-label" + (existing ? " is-existing" : "");
    const tag = "<" + element.tagName.toLowerCase() + ">";
    label.textContent = (existing ? "Open comment · " : "Comment on ") + tag;
    label.style.left = Math.max(4, Math.min(window.innerWidth - 160, first.left)) + "px";
    label.style.top = (first.top >= 30 ? first.top - 26 : Math.min(window.innerHeight - 24, first.bottom + 4)) + "px";
    overlayRoot.appendChild(label);
  }

  function renderOverlays() {
    renderFrame = 0;
    const root = ensureOverlay();
    root.replaceChildren();
    for (const annotation of annotations) {
      const className =
        "mark" + (annotation.active ? " is-active" : "") + (annotation.draft ? " is-draft" : "") +
        (annotation.id === pingedAnnotationId ? " is-ping" : "");
      for (const rect of rectsForSourceSpan(annotation)) appendRect(className, rect);
    }
    if (hoverTarget) appendHoverTarget(hoverTarget);
  }

  function scheduleOverlays() {
    if (renderFrame) return;
    renderFrame = requestAnimationFrame(renderOverlays);
  }

  function slotHostForSourceSpan(span) {
    const element = elementForSourceSpan(span);
    if (!element) return null;
    const table = element.closest("table");
    if (table) return { element: table, inside: false };
    const svg = element.closest("svg");
    if (svg) return { element: svg, inside: false };
    const details = element.closest("details");
    if (details) return { element: details, inside: false };
    const listItem = element.closest("li");
    if (listItem) return { element: listItem, inside: false };
    let candidate = element;
    while (candidate && candidate !== document.body) {
      const display = getComputedStyle(candidate).display;
      if (/^(block|flow-root|flex|grid|list-item|table|table-row-group)$/.test(display)) {
        return { element: candidate, inside: false };
      }
      candidate = candidate.parentElement;
    }
    return { element: document.body, inside: true };
  }

  function makeInlineSlot(annotation) {
    const slot = document.createElement("div");
    slot.setAttribute("data-planmaxx-review-ui", "");
    slot.setAttribute("data-planmaxx-inline-slot", annotation.id);
    slot.setAttribute("aria-hidden", "true");
    slot.style.setProperty("display", "block", "important");
    slot.style.setProperty("box-sizing", "border-box", "important");
    slot.style.setProperty("width", "100%", "important");
    slot.style.setProperty("min-width", "0", "important");
    slot.style.setProperty("clear", "both", "important");
    slot.style.setProperty("pointer-events", "none", "important");
    return slot;
  }

  function syncInlineSlots() {
    const wanted = new Set();
    for (const annotation of annotations) {
      if (!annotation.inlineSlot) continue;
      wanted.add(annotation.id);
      let slot = inlineSlots.get(annotation.id);
      let reveal = false;
      if (!slot || !slot.isConnected) {
        slot = makeInlineSlot(annotation);
        inlineSlots.set(annotation.id, slot);
        reveal = Boolean(annotation.draft);
      }
      const height = Math.max(96, Math.ceil(Number(annotation.slotHeight) || 260));
      slot.style.setProperty("height", height + "px", "important");
      const host = slotHostForSourceSpan(annotation);
      if (!host) continue;
      if (host.inside) {
        if (slot.parentElement !== host.element) {
          host.element.appendChild(slot);
          reveal = reveal || Boolean(annotation.draft);
        }
      } else if (slot.previousElementSibling !== host.element) {
        host.element.insertAdjacentElement("afterend", slot);
        reveal = reveal || Boolean(annotation.draft);
      }
      if (reveal) {
        requestAnimationFrame(() => {
          slot.scrollIntoView({ block: "nearest", inline: "nearest", behavior: "auto" });
          lastPositionOffset = -1;
          schedulePosition();
          scheduleLayout();
        });
      }
    }
    for (const [id, slot] of inlineSlots) {
      if (wanted.has(id)) continue;
      slot.remove();
      inlineSlots.delete(id);
    }
  }

  function unionRect(rects) {
    const visible = rects.filter((rect) => rect && (rect.width > 0 || rect.height > 0));
    if (visible.length === 0) return null;
    const left = Math.min(...visible.map((rect) => rect.left));
    const top = Math.min(...visible.map((rect) => rect.top));
    const right = Math.max(...visible.map((rect) => rect.right));
    const bottom = Math.max(...visible.map((rect) => rect.bottom));
    return { left, top, right, bottom, width: right - left, height: bottom - top };
  }

  function serializableRect(rect) {
    if (!rect) return null;
    return {
      left: Math.round(rect.left * 10) / 10,
      top: Math.round(rect.top * 10) / 10,
      right: Math.round(rect.right * 10) / 10,
      bottom: Math.round(rect.bottom * 10) / 10,
      width: Math.round(rect.width * 10) / 10,
      height: Math.round(rect.height * 10) / 10,
    };
  }

  function publishLayout() {
    layoutFrame = 0;
    post("planmaxx:preview-layout", {
      width: window.innerWidth,
      height: window.innerHeight,
      targets: annotations.map((annotation) => ({
        id: annotation.id,
        target: serializableRect(unionRect(rectsForSourceSpan(annotation))),
        slot: serializableRect(inlineSlots.get(annotation.id)?.getBoundingClientRect() || null),
      })),
    });
  }

  function scheduleLayout() {
    if (layoutFrame) return;
    layoutFrame = requestAnimationFrame(publishLayout);
  }

  function pingAnnotation(id) {
    pingedAnnotationId = id;
    clearTimeout(pingTimer);
    scheduleOverlays();
    pingTimer = setTimeout(() => {
      pingedAnnotationId = null;
      scheduleOverlays();
    }, 760);
  }

  function firstLayoutRect(element) {
    if (!element) return null;
    const style = getComputedStyle(element);
    if (
      style.display === "none" ||
      style.visibility === "hidden" ||
      style.visibility === "collapse" ||
      Number(style.opacity) === 0
    ) {
      return null;
    }
    const rects = [...element.getClientRects()];
    const rect = rects.find((candidate) => candidate.width > 0 || candidate.height > 0) || element.getBoundingClientRect();
    return rect.width > 0 || rect.height > 0 ? rect : null;
  }

  function renderedSourceEntries() {
    const entries = [];
    const body = document.body;
    if (!body) return entries;
    const elements = [];
    if (body.matches("[data-planmaxx-source]")) elements.push(body);
    elements.push(...body.querySelectorAll("[data-planmaxx-source]"));
    for (const element of elements) {
      const span = sourceSpanForElement(element);
      const rect = firstLayoutRect(element);
      if (!span || !rect) continue;
      entries.push({ element, span, rect });
    }
    return entries;
  }

  function currentSourcePosition() {
    const threshold = 8;
    const visible = renderedSourceEntries().filter(
      (entry) => entry.rect.bottom > threshold && entry.rect.top < window.innerHeight,
    );
    const crossingThreshold = visible
      .filter((entry) => entry.rect.top < threshold && entry.rect.bottom > threshold)
      .sort((left, right) => (left.span.end - left.span.start) - (right.span.end - right.span.start));
    if (crossingThreshold.length > 0) return crossingThreshold[0].span.start;
    const afterThreshold = visible
      .filter((entry) => entry.rect.top >= threshold)
      .sort((left, right) => left.rect.top - right.rect.top || (left.span.end - left.span.start) - (right.span.end - right.span.start));
    return afterThreshold[0]?.span.start ?? visible[0]?.span.start ?? null;
  }

  function distanceFromSourceSpan(target, candidate) {
    if (candidate.end < target.start) return target.start - candidate.end;
    if (candidate.start > target.end) return candidate.start - target.end;
    return 0;
  }

  function scrollRectForSourceSpan(span) {
    const entries = renderedSourceEntries();
    const containing = entries
      .filter((entry) => entry.span.start <= span.start && entry.span.end >= span.end)
      .sort((left, right) => (left.span.end - left.span.start) - (right.span.end - right.span.start));
    if (containing.length > 0) return containing[0].rect;
    const nearest = entries.sort((left, right) => {
      const distance = distanceFromSourceSpan(span, left.span) - distanceFromSourceSpan(span, right.span);
      if (distance !== 0) return distance;
      const leftIsAfter = left.span.start >= span.end ? 0 : 1;
      const rightIsAfter = right.span.start >= span.end ? 0 : 1;
      if (leftIsAfter !== rightIsAfter) return leftIsAfter - rightIsAfter;
      return (left.span.end - left.span.start) - (right.span.end - right.span.start);
    })[0];
    return nearest?.rect ?? null;
  }

  function publishScrollPosition() {
    positionFrame = 0;
    const offset = currentSourcePosition();
    if (offset === null) return;
    if (offset === lastPositionOffset) return;
    lastPositionOffset = offset;
    post("planmaxx:preview-position", {
      offset,
      line: sourcePosition(offset).line,
    });
  }

  function schedulePosition() {
    if (positionFrame) return;
    positionFrame = requestAnimationFrame(publishScrollPosition);
  }

  function closestSourceElement(target) {
    if (!(target instanceof Element)) return null;
    if (target.closest("[data-planmaxx-review-ui]")) return null;
    return target.closest("[data-planmaxx-source]");
  }

  function nativeInteractiveTarget(target) {
    return target instanceof Element
      ? target.closest("summary,[contenteditable]:not([contenteditable='false'])")
      : null;
  }

  function editableTarget(target) {
    return target instanceof Element
      ? target.closest("[contenteditable]:not([contenteditable='false'])")
      : null;
  }

  function hasTextSelection() {
    const selection = document.getSelection();
    return Boolean(selection && !selection.isCollapsed && selection.toString().trim());
  }

  function setHoverTarget(next) {
    if (next === hoverTarget) return;
    hoverTarget = next;
    scheduleOverlays();
    if (!next) {
      post("planmaxx:preview-hover", { tagName: "", rectCount: 0, state: "none" });
      return;
    }
    const span = sourceSpanForElement(next);
    const existing = span && matchingAnnotation(span);
    post("planmaxx:preview-hover", {
      tagName: next.tagName.toLowerCase(),
      rectCount: rectsForElement(next).length,
      state: existing ? "existing" : "new",
      anchor: span ? anchorFromOffsets(span.start, span.end) : null,
    });
  }

  function matchingAnnotation(span) {
    return annotations.find(
      (annotation) => !annotation.draft && annotation.start === span.start && annotation.end === span.end,
    );
  }

  function annotationAtPoint(x, y) {
    const matches = [];
    for (const annotation of annotations) {
      if (annotation.draft) continue;
      const hit = rectsForSourceSpan(annotation).some(
        (rect) => x >= rect.left - 2 && x <= rect.right + 2 && y >= rect.top - 2 && y <= rect.bottom + 2,
      );
      if (hit) matches.push(annotation);
    }
    return matches.sort((left, right) => {
      if (Boolean(left.active) !== Boolean(right.active)) return left.active ? -1 : 1;
      return (left.end - left.start) - (right.end - right.start);
    })[0] || null;
  }

  function publishTextSelection(target) {
    if (disabled || editableTarget(target)) return false;
    const selection = document.getSelection();
    if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return false;
    const text = selection.toString().trim();
    if (!text) return false;
    const range = selection.getRangeAt(0);
    const span = sourceSpanForRange(range);
    if (!span || span.end <= span.start) return false;
    post("planmaxx:preview-selection", {
      anchor: anchorFromOffsets(span.start, span.end),
      selectedText: text,
    });
    setHoverTarget(null);
    return true;
  }

  document.addEventListener(
    "pointerover",
    (event) => {
      if (disabled || hasTextSelection() || event.buttons) {
        setHoverTarget(null);
        return;
      }
      const next = closestSourceElement(event.target);
      setHoverTarget(next);
    },
    true,
  );

  document.addEventListener(
    "pointerout",
    (event) => {
      if (hasTextSelection()) {
        setHoverTarget(null);
        return;
      }
      setHoverTarget(closestSourceElement(event.relatedTarget));
    },
    true,
  );

  document.addEventListener(
    "pointermove",
    (event) => {
      if (event.buttons || hasTextSelection()) setHoverTarget(null);
    },
    true,
  );

  document.addEventListener(
    "selectionchange",
    () => {
      if (hasTextSelection()) setHoverTarget(null);
    },
    true,
  );

  document.addEventListener(
    "pointerup",
    (event) => {
      publishTextSelection(event.target);
    },
    true,
  );

  document.addEventListener(
    "keyup",
    (event) => {
      if (event.key !== "Shift" && !event.shiftKey) return;
      publishTextSelection(document.getSelection()?.anchorNode?.parentElement || document.body);
    },
    true,
  );

  document.addEventListener(
    "click",
    (event) => {
      if (disabled || nativeInteractiveTarget(event.target)) return;
      const selection = document.getSelection();
      if (selection && !selection.isCollapsed) return;
      const clickedAnnotation = annotationAtPoint(event.clientX, event.clientY);
      if (clickedAnnotation) {
        event.preventDefault();
        event.stopPropagation();
        pingAnnotation(clickedAnnotation.id);
        post("planmaxx:preview-focus-thread", { threadId: clickedAnnotation.id });
        return;
      }
      const element = closestSourceElement(event.target);
      const span = sourceSpanForElement(element);
      if (!element || !span || span.end <= span.start) return;
      event.preventDefault();
      event.stopPropagation();
      const existing = matchingAnnotation(span);
      if (existing) {
        pingAnnotation(existing.id);
        post("planmaxx:preview-focus-thread", { threadId: existing.id });
        return;
      }
      const text = (element.innerText || element.textContent || "").trim().replace(/\s+/g, " ");
      post("planmaxx:preview-selection", {
        anchor: anchorFromOffsets(span.start, span.end),
        selectedText: text || "<" + element.tagName.toLowerCase() + ">",
      });
    },
    true,
  );

  window.addEventListener("blur", () => setHoverTarget(null));

  function scrollToSourcePosition(message, attempt = 0) {
    const span = { start: Number(message.start) || 0, end: Number(message.end) || 0 };
    const rect = scrollRectForSourceSpan(span);
    if (!rect) {
      if (attempt < 3) {
        requestAnimationFrame(() => scrollToSourcePosition(message, attempt + 1));
      } else {
        post("planmaxx:preview-scroll-miss");
      }
      return;
    }
    window.scrollTo({
      top: Math.max(0, window.scrollY + rect.top - 8),
      behavior: message.behavior === "smooth" ? "smooth" : "auto",
    });
    lastPositionOffset = -1;
    schedulePosition();
  }

  window.addEventListener("message", (event) => {
    if (event.source !== parent) return;
    const message = event.data || {};
    if (message.type === "planmaxx:preview-annotations") {
      annotations = Array.isArray(message.annotations) ? message.annotations : [];
      disabled = Boolean(message.disabled);
      if (disabled) setHoverTarget(null);
      syncInlineSlots();
      scheduleOverlays();
      scheduleLayout();
    }
    if (message.type === "planmaxx:preview-scroll") {
      scrollToSourcePosition(message);
    }
    if (message.type === "planmaxx:preview-scroll-by") {
      window.scrollBy({ top: Number(message.top) || 0, left: Number(message.left) || 0, behavior: "auto" });
    }
    if (message.type === "planmaxx:preview-ping" && message.id) {
      pingAnnotation(message.id);
    }
  });

  window.addEventListener("scroll", () => {
    scheduleOverlays();
    schedulePosition();
    scheduleLayout();
  }, { passive: true });
  window.addEventListener("resize", () => {
    scheduleOverlays();
    schedulePosition();
    scheduleLayout();
  }, { passive: true });
  document.addEventListener("toggle", () => {
    scheduleOverlays();
    schedulePosition();
    scheduleLayout();
  }, true);
  registerTextSources();
  if (typeof ResizeObserver !== "undefined" && document.body) {
    const layoutObserver = new ResizeObserver(() => {
      scheduleOverlays();
      scheduleLayout();
    });
    layoutObserver.observe(document.body);
  }
  scheduleOverlays();
  schedulePosition();
  scheduleLayout();
  post("planmaxx:preview-ready");
})();
`;
