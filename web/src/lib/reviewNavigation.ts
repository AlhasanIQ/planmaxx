import type { ReviewStop } from "../types";

export function reviewStopRange(stop: ReviewStop): string {
  const current = lineRange(stop.afterStart, stop.afterEnd);
  const before = lineRange(stop.beforeStart, stop.beforeEnd);
  if (current && before && current !== before) return `${before} → ${current}`;
  return current || before || "changed region";
}

export function reviewStopLabel(stop: ReviewStop): string {
  const kind = stop.kind === "comment" ? "Feedback" : stop.kind === "feedback" ? "Accepted feedback" : "Change";
  return `${kind} · ${reviewStopRange(stop)}`;
}

export function reviewStopSummary(stops: ReviewStop[]): string {
  const comments = stops.filter((stop) => stop.kind === "comment" || stop.kind === "feedback").length;
  const changes = stops.filter((stop) => stop.kind === "change").length;
  const parts = [];
  if (comments) parts.push(`${comments} feedback item${comments === 1 ? "" : "s"}`);
  if (changes) parts.push(`${changes} other change${changes === 1 ? "" : "s"}`);
  return parts.join(" · ") || "Nothing to review";
}

export function reviewStopSelector(stop: ReviewStop): string | null {
  if (stop.kind === "comment" && stop.threadId) return `[data-thread-id="${escapeSelector(stop.threadId)}"]`;
  if (stop.kind === "feedback" && stop.threadId && stop.revisionId) {
    return `[data-feedback-id="${escapeSelector(`${stop.revisionId}:${stop.threadId}`)}"]`;
  }
  if (stop.clusterId) return `[data-change-cluster="${escapeSelector(stop.clusterId)}"]`;
  return stop.rowId ? `[data-change-row="${escapeSelector(stop.rowId)}"]` : null;
}

export function reviewNavigationIdentity(identity: string, stops: ReviewStop[]): string {
  return `${identity}:${stops.map((stop) => stop.id).join("|")}`;
}

export function nextReviewIndex(index: number, direction: -1 | 1, stopCount: number): number {
  const next = index + direction;
  return next < 0 || next >= stopCount ? index : next;
}

export function reviewScrollBehavior(prefersReducedMotion: boolean): ScrollBehavior {
  return prefersReducedMotion ? "auto" : "smooth";
}

function escapeSelector(value: string): string {
  if (typeof CSS !== "undefined" && typeof CSS.escape === "function") return CSS.escape(value);
  return value.replace(/["\\]/g, "\\$&");
}

function lineRange(start?: number, end?: number): string {
  if (!start) return "";
  return start === end || !end ? `line ${start}` : `lines ${start}–${end}`;
}
