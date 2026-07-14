import { ChevronLeft, ChevronRight, ListChecks } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
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

  function activate(next: number) {
    if (next < 0 || next >= stops.length) return;
    const stop = stops[next];
    setIndex(next);
    onActiveChange(stop);
    if (stop.kind === "comment" && stop.threadId) onFocusThread(stop.threadId);
    window.requestAnimationFrame(() => window.requestAnimationFrame(() => scrollToStop(stop)));
  }

  function move(direction: -1 | 1) {
	const next = nextReviewIndex(index, direction, stops.length);
	if (next !== index) activate(next);
  }

  const current = index >= 0 ? stops[index] : null;
  return (
    <nav className="review-navigator" aria-label="Review comments and changes">
      <div className="review-navigator-summary">
        <ListChecks size={14} />
        <span>{current ? reviewStopLabel(current) : reviewStopSummary(stops)}</span>
      </div>
      <span className="sr-only" aria-live="polite">
        {current ? `${reviewStopLabel(current)}. ${index + 1} of ${stops.length}` : reviewStopSummary(stops)}
      </span>
      {stops.length > 0 ? <span className="review-navigator-progress">
        {current ? `${index + 1} of ${stops.length}` : `${stops.length} to review`}
      </span> : null}
      <button type="button" className="btn btn-sm" onClick={() => move(-1)} aria-disabled={index <= 0}>
        <ChevronLeft size={13} /> Previous
      </button>
      <button type="button" className="btn btn-sm btn-primary" onClick={() => move(1)} aria-disabled={stops.length === 0 || index >= stops.length - 1}>
        Next <ChevronRight size={13} />
      </button>
    </nav>
  );
}

function scrollToStop(stop: ReviewStop) {
  const selector = reviewStopSelector(stop);
  const target = selector ? document.querySelector<HTMLElement>(selector) : null;
  target?.scrollIntoView({
    block: "center",
    behavior: reviewScrollBehavior(Boolean(window.matchMedia?.("(prefers-reduced-motion: reduce)").matches)),
  });
}
