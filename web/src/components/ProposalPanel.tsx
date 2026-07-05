import { CheckCircle2, RotateCcw, Sparkles, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import type { Anchor, SectionProposal } from "../types";
import { anchorLabel } from "../lib/anchors";
import { lineDiff } from "../lib/diff";
import { DiffView } from "./DiffView";

interface Props {
  proposal: SectionProposal;
  disabled: boolean;
  onApply: (proposalId: string) => void;
  onDiscard: (proposalId: string) => void;
  onIterate: (anchor: Anchor, instruction: string) => Promise<boolean>;
}

export function ProposalPanel({
  proposal,
  disabled,
  onApply,
  onDiscard,
  onIterate,
}: Props) {
  const [instruction, setInstruction] = useState("");
  const diff = useMemo(
    () => lineDiff(proposal.originalSection, proposal.proposedSection),
    [proposal.originalSection, proposal.proposedSection],
  );
  const canIterate = instruction.trim().length > 0 && !disabled;

  async function iterateAgain() {
    if (!canIterate) return;
    const ok = await onIterate(proposal.anchor, instruction.trim());
    if (ok) setInstruction("");
  }

  return (
    <section className="proposal-panel">
      <header className="flex items-center gap-2">
        <span className="handoff-arrow" aria-hidden>
          <Sparkles size={14} />
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-[13px] font-semibold tracking-tight">
            Pending proposal
          </h2>
          <p className="text-[11px] text-foreground-muted">
            {anchorLabel(proposal.anchor)}
          </p>
        </div>
      </header>

      {proposal.summary ? (
        <p className="mt-2 rounded-md border border-border bg-surface-muted/40 px-2.5 py-2 text-[12.5px] text-foreground">
          {proposal.summary}
        </p>
      ) : null}

      <div className="mt-3">
        <DiffView lines={diff} />
      </div>

      <label className="mt-3 block text-xs font-semibold text-foreground-muted">
        Refine
        <textarea
          className="field mt-1 min-h-20 resize-y font-sans"
          value={instruction}
          onChange={(event) => setInstruction(event.target.value)}
          placeholder="Ask for a narrower, clearer, or more specific version..."
        />
      </label>

      <div className="mt-3 flex flex-wrap justify-end gap-2">
        <button
          type="button"
          className="btn btn-sm"
          onClick={iterateAgain}
          disabled={!canIterate}
        >
          <RotateCcw size={13} /> Iterate again
        </button>
        <button
          type="button"
          className="btn btn-ghost btn-sm btn-danger"
          onClick={() => onDiscard(proposal.id)}
          disabled={disabled}
        >
          <Trash2 size={13} /> Discard
        </button>
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={() => onApply(proposal.id)}
          disabled={disabled}
        >
          <CheckCircle2 size={13} /> Apply
        </button>
      </div>
    </section>
  );
}
