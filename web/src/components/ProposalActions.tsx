import { useState } from "react";
import { CheckCircle2, RotateCcw, Sparkles, Trash2 } from "lucide-react";
import type { Anchor, PendingProposalSummary } from "../types";
import { anchorLabel } from "../lib/anchors";

export function ProposalActions({
  proposal,
  disabled,
  iterating,
  prominent = false,
  onApply,
  onDiscard,
  onIterate,
}: {
  proposal: PendingProposalSummary;
  disabled: boolean;
  iterating: boolean;
  prominent?: boolean;
  onApply: (proposalId: string) => void;
  onDiscard: (proposalId: string) => void;
  onIterate: (anchor: Anchor, instruction: string) => Promise<boolean>;
}) {
  const [instruction, setInstruction] = useState("");
  const canIterate = instruction.trim().length > 0 && !disabled;

  async function iterateAgain() {
    if (!canIterate) return;
    const ok = await onIterate(proposal.anchor, instruction.trim());
    if (ok) setInstruction("");
  }

  return (
    <section className={`inline-proposal-controls${prominent ? " is-prominent" : ""}`}>
      <div className="flex items-center gap-2">
        <span className="handoff-arrow" aria-hidden><Sparkles size={14} /></span>
        <div>
          <h2 className="text-[13px] font-semibold tracking-tight">
            {proposal.kind === "review" ? "Pending whole-plan iteration" : "Pending proposal"}
          </h2>
          <p className="text-[11px] text-foreground-muted">
            {anchorLabel(proposal.anchor)} · current revision stays unchanged until Apply
          </p>
		  <p className="text-[11px] text-foreground-muted">
			Feedback is locked while this proposal is pending. Apply or discard it to continue editing comments.
		  </p>
        </div>
      </div>
      {proposal.summary ? <p className="inline-proposal-summary">{proposal.summary}</p> : null}
      <label className="mt-3 block text-xs font-semibold text-foreground-muted">
        Refine
        <textarea
          className="field mt-1 min-h-20 resize-y font-sans"
          value={instruction}
          onChange={(event) => setInstruction(event.target.value)}
          disabled={disabled}
          placeholder="Ask for a narrower, clearer, or more specific version..."
        />
      </label>
      <div className="mt-3 flex flex-wrap justify-end gap-2">
        <button type="button" className="btn btn-sm" onClick={iterateAgain} disabled={!canIterate}>
          <RotateCcw size={13} /> {iterating ? "Iterating…" : "Iterate again"}
        </button>
        <button type="button" className="btn btn-ghost btn-sm btn-danger" onClick={() => onDiscard(proposal.id)} disabled={disabled}>
          <Trash2 size={13} /> Discard
        </button>
        <button type="button" className="btn btn-primary btn-sm" onClick={() => onApply(proposal.id)} disabled={disabled}>
          <CheckCircle2 size={13} /> Apply as new revision
        </button>
      </div>
    </section>
  );
}
