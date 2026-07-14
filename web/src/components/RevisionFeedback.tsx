import { MessageSquareText } from "lucide-react";
import type { RevisionFeedback } from "../types";

export function RevisionFeedbackSummary({ feedback }: { feedback: RevisionFeedback[] }) {
  const byRevision = new Map<string, RevisionFeedback[]>();
  for (const entry of feedback) {
    const entries = byRevision.get(entry.revisionId) ?? [];
    entries.push(entry);
    byRevision.set(entry.revisionId, entries);
  }
  return (
    <section className="comparison-feedback-summary" aria-label="Feedback behind compared revisions">
      <div className="comparison-feedback-title"><MessageSquareText size={14} /> Feedback behind these changes</div>
      {[...byRevision].map(([revisionId, entries]) => (
        <div key={revisionId} className="comparison-feedback-revision">
          <span>{revisionId}</span>
          <RevisionFeedbackList feedback={entries} />
        </div>
      ))}
    </section>
  );
}

export function RevisionFeedbackList({ feedback }: { feedback: RevisionFeedback[] }) {
  return (
    <section className="comparison-feedback-list" aria-label="Feedback that led to this change">
      <div className="comparison-feedback-title"><MessageSquareText size={13} /> Feedback applied to this change</div>
      {feedback.map((entry) => (
        <article key={`${entry.revisionId}-${entry.threadId}`} className="comparison-feedback-card">
          {entry.selectedText ? <p className="comparison-feedback-selection">“{entry.selectedText}”</p> : null}
          {entry.messages.map((message) => (
            <p key={message.id} className="comparison-feedback-message">{message.body}</p>
          ))}
        </article>
      ))}
    </section>
  );
}
