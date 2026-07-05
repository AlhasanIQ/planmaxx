import { ArrowRight, CheckCircle2, Eye, EyeOff, Sparkles } from "lucide-react";
import { useMemo } from "react";
import type { Digest } from "../types";

interface Props {
  digest: Digest;
  decisionCount: number;
  noteCount: number;
  promotedCount: number;
  ephemeralCount: number;
  collapsed: boolean;
  onToggle: () => void;
  onFinalize: () => void;
  disabled: boolean;
}

export function HandoffPanel(props: Props) {
  const {
    digest,
    decisionCount,
    noteCount,
    promotedCount,
    ephemeralCount,
    collapsed,
    onToggle,
    onFinalize,
    disabled,
  } = props;

  const hasContent =
    digest.reviewerDecisions.length > 0 || digest.promotedSideAnswers.length > 0;
  const decisionItems = useMemo(
    () => keyedPreviewItems("decision", digest.reviewerDecisions),
    [digest.reviewerDecisions],
  );
  const answerItems = useMemo(
    () => keyedPreviewItems("answer", digest.promotedSideAnswers),
    [digest.promotedSideAnswers],
  );

  return (
    <section className="handoff-panel">
      <header className="flex items-center gap-2">
        <span className="handoff-arrow" aria-hidden>
          <ArrowRight size={14} />
        </span>
        <h2 className="flex-1 text-[13px] font-semibold tracking-tight">
          Handoff to Codex
        </h2>
        <button
          type="button"
          className="btn btn-ghost btn-sm"
          onClick={onToggle}
          aria-label={collapsed ? "Expand preview" : "Collapse preview"}
          title={collapsed ? "Show what Codex will receive" : "Hide preview"}
        >
          {collapsed ? <Eye size={13} /> : <EyeOff size={13} />}
          {collapsed ? "Preview" : "Hide"}
        </button>
      </header>

      <p className="mt-1 text-[11px] text-foreground-muted">
        Live preview of what the next Codex turn will see: the approved plan plus the items below.
      </p>

      <div className="mt-2 flex flex-wrap gap-1.5 text-[11px]">
        <span className="handoff-stat handoff-stat-go">
          {decisionCount} decision{decisionCount === 1 ? "" : "s"} → next turn
        </span>
        <span className="handoff-stat handoff-stat-go">
          {promotedCount} answer{promotedCount === 1 ? "" : "s"} → next turn
        </span>
        <span className="handoff-stat handoff-stat-stay">
          {noteCount} note{noteCount === 1 ? "" : "s"} stay here
        </span>
        <span className="handoff-stat handoff-stat-stay">
          {ephemeralCount} /btw stay here
        </span>
      </div>

      {!collapsed ? (
        <div className="mt-3 rounded-md border border-border bg-surface-muted/40 p-3 text-[12.5px] leading-relaxed">
          {!hasContent ? (
            <p className="text-foreground-muted">
              Nothing extra to send yet. The approved plan alone will be handed off.
            </p>
          ) : (
            <div className="space-y-3">
              {digest.reviewerDecisions.length > 0 ? (
                <section>
                  <h3 className="mb-1 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-foreground-muted">
                    <CheckCircle2 size={11} /> Reviewer decisions
                  </h3>
                  <ul className="space-y-1 pl-1">
                    {decisionItems.map((item) => (
                      <li key={item.key} className="flex gap-2 text-foreground">
                        <span className="text-foreground-muted">•</span>
                        <span className="whitespace-pre-wrap break-words">{item.value}</span>
                      </li>
                    ))}
                  </ul>
                </section>
              ) : null}
              {digest.promotedSideAnswers.length > 0 ? (
                <section>
                  <h3 className="mb-1 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-foreground-muted">
                    <Sparkles size={11} /> Promoted /btw Q+A
                  </h3>
                  <ul className="space-y-1 pl-1">
                    {answerItems.map((item) => (
                      <li key={item.key} className="flex gap-2 text-foreground">
                        <span className="text-foreground-muted">•</span>
                        <span className="whitespace-pre-wrap break-words">{item.value}</span>
                      </li>
                    ))}
                  </ul>
                </section>
              ) : null}
            </div>
          )}
        </div>
      ) : null}

      <div className="mt-3 flex justify-end">
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={onFinalize}
          disabled={disabled}
        >
          <CheckCircle2 size={13} /> Finalize handoff
        </button>
      </div>
    </section>
  );
}

function keyedPreviewItems(prefix: string, values: string[]): Array<{ key: string; value: string }> {
  const seen = new Map<string, number>();
  return values.map((value) => {
    const count = seen.get(value) ?? 0;
    seen.set(value, count + 1);
    return {
      key: `${prefix}-${hashText(value)}-${count}`,
      value,
    };
  });
}

function hashText(value: string): string {
  let hash = 0;
  for (let index = 0; index < value.length; index++) {
    hash = (hash * 31 + value.charCodeAt(index)) | 0;
  }
  return Math.abs(hash).toString(36);
}
