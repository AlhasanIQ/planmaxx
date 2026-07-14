import { CheckCircle2, ListChecks, Sparkles } from "lucide-react";
import { useState } from "react";
import type { ReactNode } from "react";
import type { Digest } from "../../types";
import { digestForIteration } from "../../lib/digest";
import { Modal } from "../Modal";

export type SubmissionMode = "iterate" | "finalize";

export function SubmissionReviewDialog({ mode, initial, detachedCount, onCancel, onSubmit }: {
  mode: SubmissionMode;
  initial: Digest;
  detachedCount: number;
  onCancel: () => void;
  onSubmit: (digest: Digest) => void;
}) {
  const prepared = mode === "iterate" ? digestForIteration(initial, initial.summary) : initial;
  const [summary, setSummary] = useState(prepared.summary);
  const iterate = mode === "iterate";
  const digest = (): Digest => ({ ...initial, summary: summary.trim() });

  return (
    <Modal
      title={iterate ? "Review iteration" : "Review approval"}
      description={iterate
        ? "Review the feedback Codex will use. Your current revision stays unchanged until you apply the proposal."
        : "Quickly review the context submitted with the approved plan."}
      size="lg"
      onClose={onCancel}
      footer={
        <>
          <button type="button" className="btn" onClick={onCancel}>Cancel</button>
          <button type="button" className="btn btn-primary" onClick={() => onSubmit(digest())}>
            {iterate ? <Sparkles size={14} /> : <CheckCircle2 size={14} />}
            {iterate ? "Create proposal" : "Approve and submit"}
          </button>
        </>
      }
    >
      <div className="space-y-4 submission-review">
        <label className="block">
          <span className="text-xs font-semibold text-foreground-muted">
            {iterate ? "Iteration direction" : "Approval note"}
          </span>
          <textarea
            value={summary}
            onChange={(event) => setSummary(event.target.value)}
            rows={3}
            className="field mt-1 resize-y"
            data-modal-focus
          />
        </label>

        {detachedCount > 0 ? (
          <p className="submission-attention" role="note">
            {detachedCount} feedback item{detachedCount === 1 ? " needs" : "s need"} re-anchoring and will not be submitted.
          </p>
        ) : null}

        <DigestSection
          icon={<ListChecks size={14} />}
          title="Feedback for Codex"
          items={initial.reviewerDecisions}
          empty="No active iteration feedback."
        />
        <DigestSection
          icon={<Sparkles size={14} />}
          title="Included /btw context"
          items={initial.promotedSideAnswers}
          empty="No /btw answers are included."
        />
        <p className="text-xs text-foreground-muted">
          To change what is included, close this dialog and use the controls on its comment card.
        </p>
      </div>
    </Modal>
  );
}

function DigestSection({ icon, title, items, empty }: { icon: ReactNode; title: string; items: string[]; empty: string }) {
  return (
    <section className="submission-digest-section">
      <h3>{icon}{title}<span>{items.length}</span></h3>
      {items.length ? (
        <ul>{items.map((item, index) => <li key={`${index}:${item}`}>{item}</li>)}</ul>
      ) : <p>{empty}</p>}
    </section>
  );
}
