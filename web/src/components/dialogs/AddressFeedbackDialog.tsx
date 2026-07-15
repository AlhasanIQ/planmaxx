import { useMemo, useState } from "react";
import type { Revision, Thread } from "../../types";
import { Modal } from "../Modal";

export function AddressFeedbackDialog({ thread, revisions, onCancel, onConfirm }: {
  thread: Thread;
  revisions: Revision[];
  onCancel: () => void;
  onConfirm: (revisionId: string) => void;
}) {
  const candidates = useMemo(() => eligibleRevisions(thread, revisions), [revisions, thread]);
  const suggested = useMemo(() => suggestRevision(thread, candidates), [candidates, thread]);
  const [revisionId, setRevisionId] = useState(suggested?.id ?? candidates[0]?.id ?? "");
  return (
    <Modal
      title="Record feedback as addressed"
      description="Use this only after confirming that a revision applied the feedback."
      onClose={onCancel}
      size="sm"
      footer={<>
        <button type="button" className="btn" onClick={onCancel}>Cancel</button>
        <button type="button" className="btn btn-primary" disabled={!revisionId} onClick={() => onConfirm(revisionId)}>Record as addressed</button>
      </>}
    >
      <p className="text-foreground-muted">
        The original comment and selection will be preserved as read-only evidence on the chosen revision. It will not be sent to a future iteration.
      </p>
      <label className="mt-4 block text-xs font-semibold text-foreground-muted">
        Revision that applied this feedback
        <select className="field mt-1" value={revisionId} onChange={(event) => setRevisionId(event.target.value)} data-modal-focus>
          {candidates.map((revision) => <option key={revision.id} value={revision.id}>
            {revision.id} · {revisionLabel(revision)}{revision.id === suggested?.id ? " · suggested" : ""}
          </option>)}
        </select>
      </label>
      {candidates.length === 0 ? <p className="submission-attention mt-3">No later revision is available for this feedback.</p> : null}
    </Modal>
  );
}

export function eligibleRevisions(thread: Thread, revisions: Revision[]): Revision[] {
  const createdAt = Date.parse(thread.messages[0]?.createdAt ?? "");
  return revisions.filter((revision) => {
    if (!revision.parentId) return false;
    const revisionTime = Date.parse(revision.createdAt);
    return !Number.isFinite(createdAt) || !Number.isFinite(revisionTime) || revisionTime >= createdAt;
  });
}

export function suggestRevision(thread: Thread, revisions: Revision[]): Revision | undefined {
  const createdAt = Date.parse(thread.messages[0]?.createdAt ?? "");
  const afterComment = revisions.filter((revision) => {
    const revisionTime = Date.parse(revision.createdAt);
    return !Number.isFinite(createdAt) || !Number.isFinite(revisionTime) || revisionTime >= createdAt;
  });
  return afterComment.find((revision) => revision.source === "external") ?? afterComment[0];
}

function revisionLabel(revision: Revision): string {
  if (revision.source === "external") return "External source change";
  if (revision.source === "iteration") return "Applied iteration";
  if (revision.source === "immediate") return "Immediate revision";
  return revision.summary || "Revision";
}
