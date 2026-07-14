import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  EyeOff,
  MessageCircleQuestion,
  Pencil,
  Reply,
  Sparkles,
  Trash2,
} from "lucide-react";
import type { SideAnswer, Thread, ThreadIntent } from "../types";
import { relativeTime } from "../lib/time";
import { anchorLabel } from "../lib/anchors";

interface ThreadCardProps {
  thread: Thread;
  sideAnswers: SideAnswer[];
  isFocused: boolean;
  isReviewTarget?: boolean;
  onHover: (id: string | null) => void;
  onSetIntent: (id: string, intent: ThreadIntent) => void;
  onReply: (id: string) => void;
  onDelete: (id: string) => void;
  onEdit: (id: string) => void;
  onCreateFollowUp: (id: string) => void;
  onAskSide: (thread: Thread) => void;
  onIterate: (thread: Thread) => void | Promise<void>;
  onInclude: (answerId: string) => void;
  onKeepPrivate: (answerId: string) => void;
  agentAction?: "asking" | "iterating";
  disabled: boolean;
  sideQuestionsEnabled: boolean;
  presentation?: "inline" | "rail";
}

export function ThreadCard(props: ThreadCardProps) {
  const {
    thread, sideAnswers, isFocused, isReviewTarget, onHover, onSetIntent, onReply, onDelete,
    onEdit, onCreateFollowUp, onAskSide, onIterate, onInclude, onKeepPrivate,
    agentAction, disabled, sideQuestionsEnabled, presentation = "rail",
  } = props;
  const active = thread.lifecycle === "active";
  const detached = thread.lifecycle === "detached";
  const instruction = thread.intent === "instruction";
  const processing = Boolean(agentAction) || disabled;

  return (
    <section
      className={`thread is-flow${presentation === "inline" ? " is-inline-comment" : ""}${isFocused ? " is-focus" : ""}${isReviewTarget ? " is-review-target" : ""} is-${thread.lifecycle} is-${thread.intent}`}
      data-thread-id={thread.id}
      data-thread-intent={thread.intent}
      data-thread-lifecycle={thread.lifecycle}
      onMouseEnter={() => onHover(thread.id)}
      onMouseLeave={() => onHover(null)}
    >
      <header className="thread-card-header">
        <div className="thread-card-heading">
          {!active ? <span className={`thread-card-eyebrow is-${thread.lifecycle}`}>
            {detached ? <><AlertTriangle size={11} /> Needs re-anchor</> : <><CheckCircle2 size={11} /> Addressed feedback</>}
          </span> : null}
          <h3>{anchorLabel(thread.anchor)}</h3>
        </div>
        <div className="thread-card-tools">
          {thread.capabilities.canEdit ? <button type="button" className="btn btn-ghost btn-sm" onClick={() => onEdit(thread.id)} disabled={processing} aria-label={detached ? "Edit and re-anchor feedback" : "Edit comment"} title={detached ? "Edit and re-anchor feedback" : "Edit comment and selected text"}><Pencil size={13} /></button> : null}
          {thread.capabilities.canDelete ? <button type="button" className="btn btn-ghost btn-sm btn-danger" onClick={() => onDelete(thread.id)} disabled={processing} aria-label="Delete thread" title="Delete"><Trash2 size={13} /></button> : null}
        </div>
      </header>

      <div className="thread-card-meta">
        {active ? <>
          <IntentToggle intent={thread.intent} onChange={(intent) => onSetIntent(thread.id, intent)} disabled={processing || !thread.capabilities.canChangeIntent} />
          <span className="thread-intent-copy">{instruction ? "Used when you iterate or approve" : "Private to this review"}</span>
        </> : <span className="thread-intent-copy">{detached ? "Not submitted until re-anchored" : `Recorded${thread.addressedRevisionId ? ` in ${thread.addressedRevisionId}` : " in revision history"}`}</span>}
      </div>

      {detached ? <p className="thread-history-note">The anchored text could not be mapped safely. Edit this feedback to select a current location.</p> : null}
      {thread.lifecycle === "addressed" ? <p className="thread-history-note">This feedback is read-only. Create a follow-up for additional changes.</p> : null}

      <ul className="thread-message-list">
        {thread.messages.map((message) => <li key={message.id} className="thread-message">
          <div className="thread-message-meta"><span className="chip">{message.author}</span><span className="text-[11px] text-foreground-muted">{relativeTime(message.createdAt)}</span></div>
          <p className="whitespace-pre-wrap break-words text-foreground">{message.body}</p>
        </li>)}
      </ul>

      {sideAnswers.length > 0 ? <ul className="thread-side-answer-list">
        {sideAnswers.map((answer) => <li key={answer.id} className={`thread-side-answer${answer.included ? " is-promoted" : ""}`}>
          <div className="flex flex-wrap items-center gap-2 text-[11px]">
            <span className="chip"><Sparkles size={11} /> /btw</span>
            {answer.delivery === "none" ? <span className="pill pill-stay"><EyeOff size={10} /> retained with feedback</span>
              : answer.included ? <span className="pill pill-go"><ArrowRight size={10} /> included</span>
              : <span className="pill pill-stay"><EyeOff size={10} /> private</span>}
          </div>
          <p className="mt-1 font-medium text-foreground">{answer.question}</p>
          <p className="mt-1 whitespace-pre-wrap text-foreground-muted">{answer.answer}</p>
          {answer.capabilities.canInclude || answer.capabilities.canKeepPrivate ? <div className="mt-2 flex justify-end">
            {answer.included ? <button type="button" className="btn btn-ghost btn-sm" onClick={() => onKeepPrivate(answer.id)} disabled={processing}>Keep answer private</button>
              : <button type="button" className="btn btn-sm" onClick={() => onInclude(answer.id)} disabled={processing}><ArrowRight size={12} /> Include answer</button>}
          </div> : null}
        </li>)}
      </ul> : null}

      {agentAction && active ? <div className="btw-thinking mt-3" role="status" aria-live="polite"><Sparkles size={13} /><span>{agentAction === "asking" ? "Codex is considering this /btw…" : "Codex is iterating on this feedback…"}</span></div> : null}

      {active ? <div className="thread-card-actions">
        {thread.capabilities.canReply ? <button type="button" className="btn btn-sm flex-1" onClick={() => onReply(thread.id)} disabled={processing}><Reply size={13} /> {instruction ? "Add iteration feedback" : "Add private note"}</button> : null}
        {sideQuestionsEnabled && thread.capabilities.canAsk ? <button type="button" className="btn btn-sm" onClick={() => onAskSide(thread)} disabled={processing}><MessageCircleQuestion size={13} /> {agentAction === "asking" ? "Asking…" : "Ask /btw"}</button> : null}
        {thread.capabilities.canIterate ? <button type="button" className="btn btn-sm" onClick={() => onIterate(thread)} disabled={processing}><Sparkles size={13} /> {agentAction === "iterating" ? "Iterating…" : "Iterate now"}</button> : null}
      </div> : null}
      {thread.capabilities.canCreateFollowUp ? <div className="thread-card-actions"><button type="button" className="btn btn-sm" onClick={() => onCreateFollowUp(thread.id)} disabled={processing}><Reply size={13} /> Create follow-up</button></div> : null}
    </section>
  );
}

function IntentToggle({ intent, onChange, disabled }: { intent: ThreadIntent; onChange: (intent: ThreadIntent) => void; disabled: boolean }) {
  const instruction = intent === "instruction";
  return <fieldset className="kind-toggle" aria-label="Comment intent">
    <button type="button" className={`kind-pill ${instruction ? "is-active is-go" : ""}`} onClick={() => onChange("instruction")} disabled={disabled} title="Use this feedback when iterating or approving" aria-pressed={instruction}><ArrowRight size={11} /> Use in iteration</button>
    <button type="button" className={`kind-pill ${!instruction ? "is-active is-stay" : ""}`} onClick={() => onChange("private")} disabled={disabled} title="Keep this note private to the review" aria-pressed={!instruction}><EyeOff size={11} /> Private note</button>
  </fieldset>;
}
