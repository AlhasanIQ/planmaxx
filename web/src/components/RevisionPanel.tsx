import { GitCompareArrows, History, Loader2, RotateCcw } from "lucide-react";
import { useMemo } from "react";
import type { ChangeView, Revision } from "../types";
import { anchorLabel } from "../lib/anchors";

interface Props {
  currentRevisionId: string;
  revisions: Revision[];
  diff: ChangeView | null;
  loading: boolean;
  error: string | null;
  disabled: boolean;
  onCompare: (from: string, to: string) => void;
  onClearCompare: () => void;
  onRestore: (revisionId: string) => void;
  showHeader?: boolean;
}

export function RevisionPanel({
  currentRevisionId,
  revisions,
  diff,
  loading,
  error,
  disabled,
  onCompare,
  onClearCompare,
  onRestore,
  showHeader = true,
}: Props) {
  const orderedRevisions = useMemo(() => [...revisions].reverse(), [revisions]);

  return (
    <section className={`revision-panel${showHeader ? "" : " is-dialog"}`}>
      {showHeader ? <header className="flex items-center gap-2">
        <span className="handoff-arrow" aria-hidden>
          <History size={14} />
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-[13px] font-semibold tracking-tight">
            Revisions
          </h2>
          <p className="text-[11px] text-foreground-muted">
            Checked out: {currentRevisionId || "none"}
          </p>
        </div>
      </header> : null}

      <ol className={`${showHeader ? "mt-3 " : ""}space-y-1.5`}>
        {orderedRevisions.map((revision) => (
          <li
            key={revision.id}
            className={`revision-item${revision.id === currentRevisionId ? " is-current" : ""}`}
          >
            <button
              type="button"
              className="revision-button"
              onClick={() => onCompare(revision.id, currentRevisionId)}
              disabled={disabled || loading || revision.id === currentRevisionId}
              title={revision.id === currentRevisionId ? "Checked-out revision" : `Compare ${revision.id} with ${currentRevisionId}`}
            >
              <span className="revision-dot" aria-hidden />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5">
                  <span className="font-semibold">{revision.id}</span>
                  <span className="revision-source">{revision.source}</span>
                </div>
                <p className="truncate text-[11px] text-foreground-muted">
                  {revision.summary || (revision.anchor ? anchorLabel(revision.anchor) : "No summary")}
                </p>
              </div>
            </button>
            {revision.id !== currentRevisionId ? (
              <button
                type="button"
                className="icon-button"
                onClick={() => onRestore(revision.id)}
                disabled={disabled || loading}
                title={`Restore ${revision.id} as a new revision`}
                aria-label={`Restore ${revision.id} as a new revision`}
              >
                <RotateCcw size={13} />
              </button>
            ) : null}
          </li>
        ))}
      </ol>

      {revisions.length > 1 ? (
        <p className="mt-3 flex items-center gap-1.5 text-[12px] text-foreground-muted">
          {loading ? <Loader2 size={13} className="animate-spin" /> : <GitCompareArrows size={13} />}
          {loading ? "Loading comparison…" : "Click a revision to compare it with the checked-out plan."}
        </p>
      ) : (
        <p className="mt-3 text-[12px] text-foreground-muted">
          No revision diff yet.
        </p>
      )}

      {error ? <p className="mt-2 text-[12px] text-danger">{error}</p> : null}
      {diff ? (
        <div className="revision-comparison-summary mt-3">
          <div className="flex items-center justify-between gap-2 text-[11px] text-foreground-muted">
            <span>Showing {diff.baseId} → {diff.targetId} in the plan.</span>
            <button type="button" className="btn btn-ghost btn-sm" onClick={onClearCompare} disabled={disabled}>
              Hide changes
            </button>
          </div>
        </div>
      ) : null}
    </section>
  );
}
