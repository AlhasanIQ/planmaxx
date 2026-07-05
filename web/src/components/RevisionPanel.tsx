import { GitCompareArrows, History, Loader2 } from "lucide-react";
import { useMemo, useState } from "react";
import type { DiffLine, Revision } from "../types";
import { anchorLabel } from "../lib/anchors";
import { DiffView } from "./DiffView";

interface RevisionDiffState {
  from: string;
  to: string;
  lines: DiffLine[];
}

interface Props {
  currentRevisionId: string;
  revisions: Revision[];
  diff: RevisionDiffState | null;
  loading: boolean;
  error: string | null;
  disabled: boolean;
  onCompare: (from: string, to: string) => void;
}

export function RevisionPanel({
  currentRevisionId,
  revisions,
  diff,
  loading,
  error,
  disabled,
  onCompare,
}: Props) {
  const currentIndex = Math.max(0, revisions.findIndex((revision) => revision.id === currentRevisionId));
  const previousRevision = currentIndex > 0 ? revisions[currentIndex - 1] : revisions[0];
  const defaultFrom = previousRevision?.id ?? "";
  const defaultTo = currentRevisionId;
  const selectionKey = `${defaultFrom}\u0000${defaultTo}`;
  const [selection, setSelection] = useState(() => ({
    from: defaultFrom,
    key: selectionKey,
    to: defaultTo,
  }));
  const from = selection.key === selectionKey ? selection.from : defaultFrom;
  const to = selection.key === selectionKey ? selection.to : defaultTo;
  const canCompare = revisions.length > 1 && from !== "" && to !== "" && from !== to && !disabled;

  const orderedRevisions = useMemo(() => [...revisions].reverse(), [revisions]);
  const setFrom = (nextFrom: string) => {
    setSelection({ from: nextFrom, key: selectionKey, to });
  };
  const setTo = (nextTo: string) => {
    setSelection({ from, key: selectionKey, to: nextTo });
  };

  return (
    <section className="revision-panel">
      <header className="flex items-center gap-2">
        <span className="handoff-arrow" aria-hidden>
          <History size={14} />
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-[13px] font-semibold tracking-tight">
            Revisions
          </h2>
          <p className="text-[11px] text-foreground-muted">
            Current: {currentRevisionId || "none"}
          </p>
        </div>
      </header>

      <ol className="mt-3 space-y-1.5">
        {orderedRevisions.slice(0, 4).map((revision) => (
          <li
            key={revision.id}
            className={`revision-item${revision.id === currentRevisionId ? " is-current" : ""}`}
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
          </li>
        ))}
      </ol>

      {revisions.length > 1 ? (
        <>
          <div className="mt-3 grid grid-cols-2 gap-2">
            <label className="block text-[11px] font-semibold text-foreground-muted">
              From
              <select className="field mt-1 h-8 py-1 text-xs" value={from} onChange={(e) => setFrom(e.target.value)}>
                {revisions.map((revision) => (
                  <option key={revision.id} value={revision.id}>
                    {revision.id} · {revision.source}
                  </option>
                ))}
              </select>
            </label>
            <label className="block text-[11px] font-semibold text-foreground-muted">
              To
              <select className="field mt-1 h-8 py-1 text-xs" value={to} onChange={(e) => setTo(e.target.value)}>
                {revisions.map((revision) => (
                  <option key={revision.id} value={revision.id}>
                    {revision.id} · {revision.source}
                  </option>
                ))}
              </select>
            </label>
          </div>

          <div className="mt-2 flex justify-end">
            <button
              type="button"
              className="btn btn-sm"
              onClick={() => onCompare(from, to)}
              disabled={!canCompare || loading}
            >
              {loading ? <Loader2 size={13} className="animate-spin" /> : <GitCompareArrows size={13} />}
              Compare
            </button>
          </div>
        </>
      ) : (
        <p className="mt-3 text-[12px] text-foreground-muted">
          No revision diff yet.
        </p>
      )}

      {error ? <p className="mt-2 text-[12px] text-danger">{error}</p> : null}
      {diff ? (
        <div className="mt-3">
          <DiffView lines={diff.lines} />
        </div>
      ) : null}
    </section>
  );
}
