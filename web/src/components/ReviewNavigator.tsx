import { ChevronLeft, ChevronRight, GitPullRequestArrow, MessageSquareText } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import type { ReviewStop } from "../types";
import { nextReviewIndex, reviewNavigationIdentity, reviewScrollBehavior, reviewStopLabel, reviewStopSelector, reviewStopSummary } from "../lib/reviewNavigation";

export function ReviewNavigator({
  identity,
  stops,
  onFocusThread,
  onActiveChange,
}: {
  identity: string;
  stops: ReviewStop[];
  onFocusThread: (threadId: string) => void;
  onActiveChange: (stop: ReviewStop | null) => void;
}) {
  const [index, setIndex] = useState(-1);
  const signature = useMemo(() => reviewNavigationIdentity(identity, stops), [identity, stops]);

  useEffect(() => {
    setIndex(-1);
    onActiveChange(null);
  }, [signature, onActiveChange]);

  const activate = useCallback((next: number) => {
    if (next < 0 || next >= stops.length) return;
    const stop = stops[next];
    setIndex(next);
    onActiveChange(stop);
    if (stop.kind === "comment" && stop.threadId) onFocusThread(stop.threadId);
    window.requestAnimationFrame(() => window.requestAnimationFrame(() => scrollToStop(stop)));
  }, [onActiveChange, onFocusThread, stops]);

  const move = useCallback((direction: -1 | 1) => {
	const next = nextReviewIndex(index, direction, stops.length);
	if (next !== index) activate(next);
  }, [activate, index, stops.length]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!event.altKey || (event.key !== "ArrowUp" && event.key !== "ArrowDown")) return;
      const target = event.target as HTMLElement | null;
      if (target?.closest("input, textarea, select, [contenteditable=true], dialog")) return;
      event.preventDefault();
      move(event.key === "ArrowUp" ? -1 : 1);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [move]);

  const current = index >= 0 ? stops[index] : null;
  return (
    <nav className="review-navigator" aria-label="Review comments and changes">
      <div className="review-navigator-summary">
        <span className={`review-navigator-kind is-${current?.kind ?? "idle"}`} aria-hidden="true">
          {current?.kind === "comment" || current?.kind === "feedback" ? <MessageSquareText size={15} /> : <GitPullRequestArrow size={15} />}
        </span>
        <span className="review-navigator-copy">
          <strong>{current ? reviewStopLabel(current) : "Review queue"}</strong>
          <small>{reviewStopSummary(stops)}</small>
        </span>
      </div>
      <span className="sr-only" aria-live="polite">
        {current ? `${reviewStopLabel(current)}. ${index + 1} of ${stops.length}` : reviewStopSummary(stops)}
      </span>
      {stops.length > 0 ? <span className="review-navigator-progress" aria-hidden="true">
        {`${Math.max(0, index + 1)} / ${stops.length}`}
      </span> : null}
      <button type="button" className="review-navigator-button" onClick={() => move(-1)} disabled={index <= 0} aria-label="Previous review item" title="Previous review item (Alt+↑)">
        <ChevronLeft size={17} />
      </button>
      <button type="button" className="review-navigator-button is-next" onClick={() => move(1)} disabled={stops.length === 0 || index >= stops.length - 1} aria-label="Next review item" title="Next review item (Alt+↓)">
        <ChevronRight size={17} />
      </button>
    </nav>
  );
}

function scrollToStop(stop: ReviewStop) {
  const selector = reviewStopSelector(stop);
  const targets = selector ? [...document.querySelectorAll<HTMLElement>(selector)] : [];
  if (targets.length === 0) return;
  const first = targets[0].getBoundingClientRect();
  const last = targets[targets.length - 1].getBoundingClientRect();
  const center = (first.top + last.bottom) / 2;
  window.scrollTo({
    top: Math.max(0, window.scrollY + center - window.innerHeight / 2),
    behavior: reviewScrollBehavior(Boolean(window.matchMedia?.("(prefers-reduced-motion: reduce)").matches)),
  });
}
