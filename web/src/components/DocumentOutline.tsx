import { BookOpen, ChevronRight, X } from "lucide-react";
import type { CSSProperties } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import type { OutlineItem } from "../lib/documentOutline";

export function DocumentOutline({ items, onNavigate }: { items: OutlineItem[]; onNavigate: (item: OutlineItem) => void }) {
  const [open, setOpen] = useState(false);
  const [activeId, setActiveId] = useState(items[0]?.id ?? "");
  const rootRef = useRef<HTMLElement>(null);

  useEffect(() => {
    setActiveId((current) => items.some((item) => item.id === current) ? current : items[0]?.id ?? "");
  }, [items]);

  useEffect(() => {
    let frame = 0;
    const update = () => {
      frame = 0;
      let active = items[0];
      for (const item of items) {
        const target = document.querySelector<HTMLElement>(`[data-document-line="${item.line}"]`);
        if (!target) continue;
        if (target.getBoundingClientRect().top <= 150) active = item;
        else break;
      }
      if (active) setActiveId(active.id);
    };
    const schedule = () => {
      if (!frame) frame = window.requestAnimationFrame(update);
    };
    update();
    window.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    return () => {
      window.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      if (frame) window.cancelAnimationFrame(frame);
    };
  }, [items]);

  useEffect(() => {
    if (!open) return;
    const closeOnOutsideClick = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("pointerdown", closeOnOutsideClick);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("pointerdown", closeOnOutsideClick);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [open]);

  const activeItem = useMemo(() => items.find((item) => item.id === activeId) ?? items[0], [activeId, items]);
  if (items.length === 0) return null;

  return (
    <aside ref={rootRef} className={`document-outline-island${open ? " is-open" : ""}`} aria-label="Document outline">
      <button
        type="button"
        className="document-outline-trigger"
        onClick={() => setOpen((current) => !current)}
        aria-expanded={open}
        aria-controls="document-outline-panel"
        title="Browse document sections"
      >
        <BookOpen size={15} aria-hidden="true" />
        <span>{activeItem?.title ?? "Outline"}</span>
        <span className="document-outline-count">{items.length}</span>
      </button>
      {open ? (
        <div id="document-outline-panel" className="document-outline-panel">
          <header>
            <div>
              <strong>Document outline</strong>
              <span>{items.length} {items.length === 1 ? "section" : "sections"}</span>
            </div>
            <button type="button" className="document-outline-close" onClick={() => setOpen(false)} aria-label="Close document outline">
              <X size={15} />
            </button>
          </header>
          <nav aria-label="Document sections">
            {items.map((item) => (
              <button
                key={item.id}
                type="button"
                className={`document-outline-item${item.id === activeId ? " is-active" : ""}`}
                style={{ "--outline-indent": `${Math.min(3, Math.max(0, item.level - 1)) * 10}px` } as CSSProperties}
                onClick={() => {
                  setActiveId(item.id);
                  onNavigate(item);
                  if (window.matchMedia("(max-width: 720px)").matches) setOpen(false);
                }}
                aria-current={item.id === activeId ? "location" : undefined}
                title={`${item.title} · line ${item.line}`}
              >
                <ChevronRight size={12} aria-hidden="true" />
                <span>{item.title}</span>
                <small>{item.line}</small>
              </button>
            ))}
          </nav>
        </div>
      ) : null}
    </aside>
  );
}
