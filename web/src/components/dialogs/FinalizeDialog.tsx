import { useMemo, useState } from "react";
import { ArrowRight, CheckCircle2, ListChecks, Sparkles } from "lucide-react";
import { Modal } from "../Modal";
import type { Digest } from "../../types";

interface Props {
  initial: Digest;
  onCancel: () => void;
  onIterate: (digest: Digest) => void;
  onSubmit: (digest: Digest) => void;
}

export function FinalizeDialog({ initial, onCancel, onIterate, onSubmit }: Props) {
  const [summary, setSummary] = useState(() => initial.summary);
  const [decisions, setDecisions] = useState(() =>
    editableItems("decision", initial.reviewerDecisions ?? []),
  );
  const [answers, setAnswers] = useState(() =>
    editableItems("answer", initial.promotedSideAnswers ?? []),
  );

  const trimmedDecisions = useMemo(
    () => trimmedItems(decisions),
    [decisions],
  );
  const trimmedAnswers = useMemo(
    () => trimmedItems(answers),
    [answers],
  );

  function digest() {
    return {
      summary: summary.trim(),
      reviewerDecisions: trimmedDecisions.map((item) => item.value),
      promotedSideAnswers: trimmedAnswers.map((item) => item.value),
    };
  }

  return (
    <Modal
      title="Finalize review"
      description="Approve to hand off. Iterate creates a whole-plan proposal from this feedback and returns you to review."
      size="lg"
      onClose={onCancel}
      footer={
        <>
          <button type="button" className="btn" onClick={() => onIterate(digest())}>
            <Sparkles size={14} /> Iterate plan
          </button>
          <button type="button" className="btn btn-primary" onClick={() => onSubmit(digest())}>
            <CheckCircle2 size={14} /> Approve
          </button>
        </>
      }
    >
      <div className="space-y-4">
        <label className="block">
          <span className="text-xs font-semibold uppercase tracking-wide text-foreground-muted">
            Optional explanation summary
          </span>
          <textarea
            value={summary}
            onChange={(e) => {
              setSummary(e.target.value);
            }}
            rows={3}
            className="field mt-1 resize-y"
            data-modal-focus
          />
        </label>

        <section>
          <div className="mb-1.5 flex items-center gap-1.5">
            <ListChecks size={14} className="text-foreground-muted" />
            <h3 className="text-xs font-semibold uppercase tracking-wide text-foreground-muted">
              Reviewer decisions
            </h3>
            <span className="text-[11px] text-foreground-muted">
              from threads marked “Decision”
            </span>
          </div>
          <Editable items={decisions} setItems={setDecisions} placeholder="Add a reviewer decision…" />
        </section>

        <section>
          <div className="mb-1.5 flex items-center gap-1.5">
            <Sparkles size={14} className="text-foreground-muted" />
            <h3 className="text-xs font-semibold uppercase tracking-wide text-foreground-muted">
              Promoted /btw Q+A
            </h3>
            <span className="text-[11px] text-foreground-muted">
              ephemeral side questions you opted in
            </span>
          </div>
          {answers.length === 0 ? (
            <p className="text-xs text-foreground-muted">No promoted /btw Q+A.</p>
          ) : (
            <Editable items={answers} setItems={setAnswers} placeholder="Promoted /btw Q+A…" />
          )}
        </section>

        <section className="rounded-md border border-border bg-surface-muted/40 p-3">
          <div className="mb-2 flex items-center gap-1.5">
            <ArrowRight size={13} className="text-accent" />
            <h3 className="text-xs font-semibold uppercase tracking-wider text-foreground-muted">
              Exactly what Codex will receive
            </h3>
          </div>
            <Preview
              summary={summary.trim()}
              decisions={trimmedDecisions}
              answers={trimmedAnswers}
            />
        </section>
      </div>
    </Modal>
  );
}

function Preview({
  summary,
  decisions,
  answers,
}: {
  summary: string;
  decisions: EditableItem[];
  answers: EditableItem[];
}) {
  const hasContent = decisions.length > 0 || answers.length > 0;
  return (
    <div className="text-[12.5px] leading-relaxed">
      <p className="text-foreground">
        <strong className="font-semibold">Summary:</strong>{" "}
        {summary || <span className="text-foreground-muted">Not provided</span>}
      </p>
      {!hasContent ? (
        <p className="mt-2 text-foreground-muted">
          The plan will be handed off with no extra reviewer items.
        </p>
      ) : (
        <div className="mt-2 space-y-2">
          {decisions.length > 0 ? (
            <div>
              <p className="text-[11px] font-semibold uppercase tracking-wider text-foreground-muted">
                Reviewer decisions ({decisions.length})
              </p>
              <ul className="mt-0.5 space-y-0.5 pl-1">
                {decisions.map((item) => (
                  <li key={item.id} className="flex gap-2">
                    <span className="text-foreground-muted">•</span>
                    <span className="whitespace-pre-wrap break-words">{item.value}</span>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
          {answers.length > 0 ? (
            <div>
              <p className="text-[11px] font-semibold uppercase tracking-wider text-foreground-muted">
                Promoted /btw Q+A ({answers.length})
              </p>
              <ul className="mt-0.5 space-y-0.5 pl-1">
                {answers.map((item) => (
                  <li key={item.id} className="flex gap-2">
                    <span className="text-foreground-muted">•</span>
                    <span className="whitespace-pre-wrap break-words">{item.value}</span>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
        </div>
      )}
    </div>
  );
}

function Editable({
  items,
  setItems,
  placeholder,
}: {
  items: EditableItem[];
  setItems: (next: EditableItem[]) => void;
  placeholder: string;
}) {
  return (
    <ul className="space-y-1.5">
      {items.map((item) => (
        <li key={item.id} className="flex items-start gap-2">
          <textarea
            value={item.value}
            onChange={(e) => {
              const next = items.map((current) =>
                current.id === item.id ? { ...current, value: e.target.value } : current,
              );
              setItems(next);
            }}
            rows={1}
            aria-label={`Edit ${placeholder}`}
            className="field resize-y"
          />
          <button
            type="button"
            className="btn btn-ghost btn-sm mt-1.5"
            onClick={() => setItems(items.filter((current) => current.id !== item.id))}
            aria-label="Remove entry"
          >
            Remove
          </button>
        </li>
      ))}
      <li>
        <button
          type="button"
          className="btn btn-sm"
          onClick={() => setItems([...items, newEditableItem("item", "")])}
        >
          + Add
        </button>
        <span className="ml-2 text-xs text-foreground-muted">{placeholder}</span>
      </li>
    </ul>
  );
}

interface EditableItem {
  id: string;
  value: string;
}

function editableItems(prefix: string, values: string[]): EditableItem[] {
  return values.map((value, index) => ({
    id: `${prefix}-${index}-${hashText(value)}`,
    value,
  }));
}

function newEditableItem(prefix: string, value: string): EditableItem {
  return {
    id: `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`,
    value,
  };
}

function trimmedItems(items: EditableItem[]): EditableItem[] {
  return items.flatMap((item) => {
    const value = item.value.trim();
    return value ? [{ ...item, value }] : [];
  });
}

function hashText(value: string): string {
  let hash = 0;
  for (let index = 0; index < value.length; index++) {
    hash = (hash * 31 + value.charCodeAt(index)) | 0;
  }
  return Math.abs(hash).toString(36);
}
