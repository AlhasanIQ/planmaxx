import { useCallback, useEffect, useMemo, useState } from "react";
import { Search } from "lucide-react";
import { ApiError, api } from "./api";
import { TopBar } from "./components/TopBar";
import { Plan } from "./components/Plan";
import { Threads } from "./components/Threads";
import { HandoffPanel } from "./components/HandoffPanel";
import { ProposalPanel } from "./components/ProposalPanel";
import { RevisionPanel } from "./components/RevisionPanel";
import { ToastStack, type Toast } from "./components/Toasts";
import { CompletedScreen } from "./components/CompletedScreen";
import { PromptDialog } from "./components/dialogs/PromptDialog";
import { ConfirmDialog } from "./components/dialogs/ConfirmDialog";
import { FinalizeDialog } from "./components/dialogs/FinalizeDialog";
import type { Anchor, DiffLine, Digest, Session, Thread, ThreadKind } from "./types";
import { buildDigestDraft, countHandoff } from "./lib/digest";
import { anchorLabel } from "./lib/anchors";
import { sideQuestionContext } from "./lib/selectionContext";
import {
  nextThemeMode,
  prefersDarkFromMatcher,
  readStoredThemeMode,
  resolveThemeMode,
  subscribeToColorSchemeChanges,
  writeStoredThemeMode,
  type ThemeMode,
} from "./lib/theme";

type CompletionState = null | "finalized" | "rejected" | "canceled";
type RevisionDiffState = { from: string; to: string; lines: DiffLine[] };

type DialogState =
  | null
  | { kind: "reply"; threadId: string }
  | { kind: "delete"; threadId: string }
  | { kind: "ask"; thread: Thread }
  | { kind: "finalize"; digest: Digest }
  | { kind: "confirmCancel" };

function useReviewController() {
  const [session, setSession] = useState<Session | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [status, setStatus] = useState<{ label: string; kind: "idle" | "busy" | "error" | "success" }>({
    label: "Loading…",
    kind: "busy",
  });
  const [completion, setCompletion] = useState<CompletionState>(null);
  const [busy, setBusy] = useState(false);
  const [filter, setFilter] = useState("");
  const [dialog, setDialog] = useState<DialogState>(null);
  const [hoveredThreadId, setHoveredThreadId] = useState<string | null>(null);
  const [focusedThreadId, setFocusedThreadId] = useState<string | null>(null);
  const [sideQuestionsEnabled, setSideQuestionsEnabled] = useState(true);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const theme = useTheme();
  const [handoffCollapsed, setHandoffCollapsed] = useState(false);
  const [editingThreadId, setEditingThreadId] = useState<string | null>(null);
  const [askingThreadIds, setAskingThreadIds] = useState<Record<string, boolean>>({});
  const [revisionDiff, setRevisionDiff] = useState<RevisionDiffState | null>(null);
  const [revisionDiffLoading, setRevisionDiffLoading] = useState(false);
  const [revisionDiffError, setRevisionDiffError] = useState<string | null>(null);

  const pushToast = useCallback((kind: Toast["kind"], message: string) => {
    setToasts((prev) => [...prev, { id: Date.now() + Math.random(), kind, message }]);
  }, []);
  const dismissToast = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const refresh = useCallback(async () => {
    try {
      const next = await api.getState();
      setSession(next);
      setLoadError(null);
      setStatus({ label: "Codex paused — review in progress", kind: "idle" });
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Failed to load review";
      setLoadError(msg);
      setStatus({ label: msg, kind: "error" });
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const updateThreadKind = useCallback(
    async (threadId: string, kind: ThreadKind) => {
      if (!session) return;
      const previous = session;
      setSession({
        ...session,
        threads: session.threads.map((t) => (t.id === threadId ? { ...t, kind } : t)),
      });
      try {
        await api.setThreadKind(threadId, kind);
      } catch (e) {
        setSession(previous);
        const msg = e instanceof Error ? e.message : "Failed to update thread kind";
        pushToast("error", msg);
      }
    },
    [pushToast, session],
  );

  const liveDigest = useMemo(() => (session ? buildDigestDraft(session) : null), [session]);
  const counts = useMemo(
    () =>
      session
        ? countHandoff(session)
        : { decisions: 0, notes: 0, promoted: 0, ephemeral: 0 },
    [session],
  );

  const openFinalize = useCallback(() => {
    if (!session || !liveDigest) return;
    if (session.pendingProposal) {
      pushToast("error", "Apply or discard the pending proposal before finalizing");
      return;
    }
    setDialog({ kind: "finalize", digest: liveDigest });
  }, [liveDigest, pushToast, session]);

  // Keyboard shortcuts.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (completion) return;
      const tag = (e.target as HTMLElement | null)?.tagName?.toLowerCase();
      const editing = tag === "input" || tag === "textarea" || tag === "select";
      if (editing) return;
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        document.getElementById("thread-filter")?.focus();
      }
      if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key === "Enter") {
        e.preventDefault();
        openFinalize();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [completion, openFinalize]);

  async function withBusy<T>(label: string, fn: () => Promise<T>): Promise<T | null> {
    setBusy(true);
    setStatus({ label, kind: "busy" });
    try {
      const result = await fn();
      setStatus({ label: "Codex paused — review in progress", kind: "idle" });
      return result;
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Request failed";
      setStatus({ label: msg, kind: "error" });
      pushToast("error", msg);
      return null;
    } finally {
      setBusy(false);
    }
  }

  async function handleCreateThread(anchor: Anchor, body: string, selectedText: string): Promise<boolean> {
    const result = await withBusy("Adding comment…", async () => {
      const thread = await api.createThread(anchor, body, selectedText);
      return thread;
    });
    if (result && session) {
      setSession({ ...session, threads: [...session.threads, result] });
      setFocusedThreadId(result.id);
      pushToast("success", "Comment added — sent to next turn");
      return true;
    }
    return false;
  }

  async function handleCreateThreadAndAsk(anchor: Anchor, body: string, selectedText: string): Promise<boolean> {
    if (!session) return false;
    const result = await withBusy("Adding /btw comment…", async () =>
      api.createThread(anchor, body, selectedText),
    );
    if (!result) return false;

    const nextSession = { ...session, threads: [...session.threads, result] };
    setSession(nextSession);
    setFocusedThreadId(result.id);
    return askSideQuestion(result, body, nextSession);
  }

  async function handleIterateSection(anchor: Anchor, instruction: string, threadId?: string): Promise<boolean> {
    const result = await withBusy("Iterating section…", () =>
      api.proposeSection(threadId, anchor, instruction),
    );
    if (!result) return false;
    setSession((current) => (current ? { ...current, pendingProposal: result } : current));
    setRevisionDiff(null);
    pushToast("success", "Proposal ready");
    return true;
  }

  function handleIterateThread(thread: Thread) {
    const instruction = thread.messages.at(-1)?.body.trim() || "Revise this section according to this thread.";
    void handleIterateSection(thread.anchor, instruction, thread.id);
  }

  async function handleUpdateThread(threadId: string, anchor: Anchor, body: string, selectedText: string): Promise<boolean> {
    const ok = await withBusy("Saving comment…", () => api.editThread(threadId, anchor, body, selectedText));
    if (ok && session) {
      setSession({
        ...session,
        threads: session.threads.map((t) =>
          t.id === threadId
            ? {
                ...t,
                anchor,
                selectedText,
                messages: t.messages.map((message, index) =>
                  index === 0 ? { ...message, body } : message,
                ),
              }
            : t,
        ),
      });
      setFocusedThreadId(threadId);
      pushToast("success", "Comment updated");
      return true;
    }
    return false;
  }

  async function handleReply(threadId: string, body: string) {
    setDialog(null);
    const ok = await withBusy("Saving reply…", () => api.reply(threadId, body));
    if (ok) {
      await refresh();
      pushToast("success", "Reply saved");
    }
  }

  async function handleDelete(threadId: string) {
    setDialog(null);
    const ok = await withBusy("Deleting thread…", () => api.deleteThread(threadId));
    if (ok) {
      await refresh();
      pushToast("info", "Thread deleted");
    }
  }

  async function handleMove(threadId: string, x: number, y: number) {
    const ok = await withBusy("Moving comment…", () => api.moveThread(threadId, x, y));
    if (ok && session) {
      setSession({
        ...session,
        threads: session.threads.map((t) =>
          t.id === threadId ? { ...t, position: { x, y } } : t,
        ),
      });
    }
  }

  async function handleAsk(thread: Thread, question: string) {
    setDialog(null);
    return askSideQuestion(thread, question, session);
  }

  async function askSideQuestion(thread: Thread, question: string, sourceSession: Session | null): Promise<boolean> {
    if (!sourceSession) return false;
    setBusy(true);
    setStatus({ label: "Asking Codex (ephemeral /btw)…", kind: "busy" });
    setFocusedThreadId(thread.id);
    setAskingThreadIds((prev) => ({ ...prev, [thread.id]: true }));
    try {
      const answer = await api.sideQuestion(thread.id, question, sideQuestionContext(sourceSession, thread));
      setSession((current) =>
        current
          ? {
              ...current,
              sideAnswers: [
                ...current.sideAnswers.filter((a) => a.id !== answer.id),
                answer,
              ],
            }
          : current,
      );
      pushToast("success", "/btw answer received (stays here unless you opt in)");
      setStatus({ label: "Codex paused — review in progress", kind: "idle" });
      return true;
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Side questions unavailable";
      pushToast("error", msg);
      setStatus({ label: msg, kind: "error" });
      if (e instanceof ApiError && e.status === 503) {
        setSideQuestionsEnabled(false);
      }
      return false;
    } finally {
      setAskingThreadIds((prev) => {
        const next = { ...prev };
        delete next[thread.id];
        return next;
      });
      setBusy(false);
    }
  }

  async function handlePromote(answerId: string) {
    const ok = await withBusy("Adding answer to handoff…", () => api.promote(answerId));
    if (ok) {
      await refresh();
      pushToast("success", "/btw answer → next turn");
    }
  }
  async function handleUnpromote(answerId: string) {
    const ok = await withBusy("Removing from handoff…", () => api.unpromote(answerId));
    if (ok) {
      await refresh();
      pushToast("info", "Answer kept ephemeral");
    }
  }

  async function handleApplyProposal(proposalId: string) {
    const revision = await withBusy("Applying proposal…", () => api.applyProposal(proposalId));
    if (!revision) return;
    setRevisionDiff(null);
    await refresh();
    pushToast("success", "Proposal applied");
  }

  async function handleDiscardProposal(proposalId: string) {
    const ok = await withBusy("Discarding proposal…", () => api.discardProposal(proposalId));
    if (!ok) return;
    setSession((current) => (current ? { ...current, pendingProposal: null } : current));
    pushToast("info", "Proposal discarded");
  }

  async function handleCompareRevision(from: string, to: string) {
    setRevisionDiffLoading(true);
    setRevisionDiffError(null);
    try {
      const result = await api.revisionDiff(from, to);
      setRevisionDiff(result);
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Failed to load revision diff";
      setRevisionDiffError(msg);
      pushToast("error", msg);
    } finally {
      setRevisionDiffLoading(false);
    }
  }

  async function handleFinalize(digest: Digest) {
    setDialog(null);
    const ok = await withBusy("Finalizing…", () => api.finalize(digest));
    if (ok) {
      setCompletion("finalized");
      setStatus({ label: "Finalized — handoff sent", kind: "success" });
    }
  }

  async function handleReject(digest: Digest) {
    setDialog(null);
    const ok = await withBusy("Rejecting…", () => api.reject(digest));
    if (ok) {
      setCompletion("rejected");
      setStatus({ label: "Rejected — handoff sent", kind: "success" });
    }
  }

  async function handleCancel() {
    setDialog(null);
    const ok = await withBusy("Canceling…", () => api.cancel());
    if (ok) {
      setCompletion("canceled");
      setStatus({ label: "Canceled", kind: "idle" });
    }
  }

  return {
    askingThreadIds,
    busy,
    completion,
    counts,
    dialog,
    dismissToast,
    editingThreadId,
    filter,
    focusedThreadId,
    handleApplyProposal,
    handleAsk,
    handleCancel,
    handleCompareRevision,
    handleCreateThread,
    handleCreateThreadAndAsk,
    handleDelete,
    handleDiscardProposal,
    handleFinalize,
    handleIterateSection,
    handleIterateThread,
    handleMove,
    handlePromote,
    handleReject,
    handleReply,
    handleUnpromote,
    handleUpdateThread,
    handoffCollapsed,
    hoveredThreadId,
    liveDigest,
    loadError,
    openFinalize,
    refresh,
    revisionDiff,
    revisionDiffError,
    revisionDiffLoading,
    session,
    setDialog,
    setEditingThreadId,
    setFilter,
    setFocusedThreadId,
    setHandoffCollapsed,
    setHoveredThreadId,
    sideQuestionsEnabled,
    status,
    theme,
    toasts,
    updateThreadKind,
  };
}

type ReviewController = ReturnType<typeof useReviewController>;

export default function App() {
  const controller = useReviewController();
  return <ReviewScreen controller={controller} />;
}

function ReviewScreen({ controller }: { controller: ReviewController }) {
  const {
    askingThreadIds,
    busy,
    completion,
    counts,
    dialog,
    dismissToast,
    editingThreadId,
    filter,
    focusedThreadId,
    handleApplyProposal,
    handleAsk,
    handleCancel,
    handleCompareRevision,
    handleCreateThread,
    handleCreateThreadAndAsk,
    handleDelete,
    handleDiscardProposal,
    handleFinalize,
    handleIterateSection,
    handleIterateThread,
    handleMove,
    handlePromote,
    handleReject,
    handleReply,
    handleUnpromote,
    handleUpdateThread,
    handoffCollapsed,
    hoveredThreadId,
    liveDigest,
    loadError,
    openFinalize,
    refresh,
    revisionDiff,
    revisionDiffError,
    revisionDiffLoading,
    session,
    setDialog,
    setEditingThreadId,
    setFilter,
    setFocusedThreadId,
    setHandoffCollapsed,
    setHoveredThreadId,
    sideQuestionsEnabled,
    status,
    theme,
    toasts,
    updateThreadKind,
  } = controller;
  const setThemeMode = theme.setMode;
  const changeThemeMode = useCallback(() => {
    setThemeMode((mode) => nextThemeMode(mode));
  }, [setThemeMode]);
  const clearEditingThread = useCallback(() => {
    setEditingThreadId(null);
  }, [setEditingThreadId]);
  const focusThreadTemporarily = useCallback(
    (id: string) => {
      setFocusedThreadId(id);
      setHoveredThreadId(id);
      window.setTimeout(() => setHoveredThreadId(null), 1200);
    },
    [setFocusedThreadId, setHoveredThreadId],
  );
  const toggleHandoffCollapsed = useCallback(() => {
    setHandoffCollapsed((value) => !value);
  }, [setHandoffCollapsed]);

  if (completion) {
    return <CompletedScreen state={completion} />;
  }

  if (!session) {
    return (
      <div className="grid min-h-full place-items-center text-sm text-foreground-muted">
        {loadError ? (
          <div className="text-center">
            <p className="text-danger">{loadError}</p>
            <button type="button" className="btn mt-3" onClick={refresh}>
              Retry
            </button>
          </div>
        ) : (
          "Loading review…"
        )}
      </div>
    );
  }

  return (
    <div className="flex min-h-full flex-col">
      <TopBar
        statusLabel={status.label}
        statusKind={status.kind}
        decisionCount={counts.decisions}
        promotedCount={counts.promoted}
        noteCount={counts.notes}
        ephemeralCount={counts.ephemeral}
        themeMode={theme.mode}
        resolvedTheme={theme.resolved}
        onThemeModeChange={changeThemeMode}
        onCancel={() => setDialog({ kind: "confirmCancel" })}
        onFinalize={openFinalize}
        disabled={busy}
        finalizeDisabled={Boolean(session.pendingProposal)}
      />

      <main className="mx-auto grid w-full max-w-[1240px] grid-cols-1 gap-5 px-4 py-5 lg:grid-cols-[minmax(0,1fr)_360px]">
        <Plan
          plan={session.plan}
          threads={session.threads}
          hoveredThreadId={hoveredThreadId}
          focusedThreadId={focusedThreadId}
          editingThread={session.threads.find((t) => t.id === editingThreadId) ?? null}
          onCreateComment={handleCreateThread}
          onUpdateComment={handleUpdateThread}
          onAskSideFromDraft={handleCreateThreadAndAsk}
          onIterateDraft={(anchor, instruction) => handleIterateSection(anchor, instruction)}
          onEditDone={clearEditingThread}
          onFocusThread={focusThreadTemporarily}
        />

        <aside className="min-w-0 space-y-3">
          {session.pendingProposal ? (
            <ProposalPanel
              proposal={session.pendingProposal}
              disabled={busy}
              onApply={handleApplyProposal}
              onDiscard={handleDiscardProposal}
              onIterate={(anchor, instruction) => handleIterateSection(anchor, instruction)}
            />
          ) : null}
          {liveDigest ? (
            <HandoffPanel
              digest={liveDigest}
              decisionCount={counts.decisions}
              noteCount={counts.notes}
              promotedCount={counts.promoted}
              ephemeralCount={counts.ephemeral}
              collapsed={handoffCollapsed}
              onToggle={toggleHandoffCollapsed}
              onFinalize={openFinalize}
              disabled={busy || Boolean(session.pendingProposal)}
            />
          ) : null}
          <RevisionPanel
            currentRevisionId={session.currentRevisionId}
            revisions={session.revisions}
            diff={revisionDiff}
            loading={revisionDiffLoading}
            error={revisionDiffError}
            disabled={busy}
            onCompare={handleCompareRevision}
          />
          <label
            htmlFor="thread-filter"
            className="block text-xs font-semibold uppercase tracking-wide text-foreground-muted"
          >
            Threads
          </label>
          <div className="relative">
            <Search size={14} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-foreground-muted" />
            <input
              id="thread-filter"
              type="search"
              className="field pl-8"
              placeholder="Filter threads (⌘/Ctrl+K)"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              autoComplete="off"
            />
          </div>
          <Threads
            threads={session.threads}
            sideAnswers={session.sideAnswers}
            filter={filter}
            focusedThreadId={focusedThreadId}
            hoveredThreadId={hoveredThreadId}
            onHover={setHoveredThreadId}
            onMove={handleMove}
            onSetKind={updateThreadKind}
            onReply={(id) => setDialog({ kind: "reply", threadId: id })}
            onDelete={(id) => setDialog({ kind: "delete", threadId: id })}
            onEdit={(id) => {
              setEditingThreadId(id);
              setFocusedThreadId(id);
            }}
            onAskSide={(t) => setDialog({ kind: "ask", thread: t })}
            onIterate={handleIterateThread}
            onPromote={handlePromote}
            onUnpromote={handleUnpromote}
            askingThreadIds={askingThreadIds}
            sideQuestionsEnabled={sideQuestionsEnabled}
          />
        </aside>
      </main>

      {dialog?.kind === "reply" && (
        <ReplyDialog
          thread={session.threads.find((t) => t.id === dialog.threadId)}
          onCancel={() => setDialog(null)}
          onSubmit={(value) => handleReply(dialog.threadId, value)}
        />
      )}
      {dialog?.kind === "delete" && (
        <ConfirmDialog
          title="Delete this thread?"
          body="The comments and any side answers anchored to this thread will be removed."
          confirmLabel="Delete"
          danger
          onCancel={() => setDialog(null)}
          onConfirm={() => handleDelete(dialog.threadId)}
        />
      )}
      {dialog?.kind === "ask" && (
        <PromptDialog
          title="Ask Codex an ephemeral /btw question"
          description={`Anchored to ${anchorLabel(dialog.thread.anchor)}. Pre-filled with the latest comment in this thread; edit if you want to ask something different. Answers stay out of the handoff until you opt them in.`}
          label="Question"
          submitLabel="Ask /btw"
          placeholder="What should we ask Codex on the side?"
          initialValue={dialog.thread.messages.at(-1)?.body ?? ""}
          onCancel={() => setDialog(null)}
          onSubmit={(value) => handleAsk(dialog.thread, value)}
        />
      )}
      {dialog?.kind === "finalize" && (
        <FinalizeDialog
          initial={dialog.digest}
          onCancel={() => setDialog(null)}
          onReject={handleReject}
          onSubmit={handleFinalize}
        />
      )}
      {dialog?.kind === "confirmCancel" && (
        <ConfirmDialog
          title="Cancel this review?"
          body="The CLI will exit with a non-zero status and no handoff will be printed."
          confirmLabel="Cancel review"
          danger
          onCancel={() => setDialog(null)}
          onConfirm={handleCancel}
        />
      )}

      <ToastStack toasts={toasts} onDismiss={dismissToast} />
    </div>
  );
}

function useTheme() {
  const [mode, setMode] = useState<ThemeMode>(() => readStoredThemeMode());
  const [prefersDark, setPrefersDark] = useState(() => {
    if (typeof window === "undefined") return false;
    return prefersDarkFromMatcher(window.matchMedia?.bind(window));
  });
  const resolved = resolveThemeMode(mode, prefersDark);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const media = colorSchemeMediaQuery();
    if (!media) return;
    const syncTheme = () => setPrefersDark(media.matches);

    return subscribeToColorSchemeChanges(media, syncTheme);
  }, []);

  useEffect(() => {
    writeStoredThemeMode(mode);
  }, [mode]);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", resolved === "dark");
  }, [resolved]);

  return { mode, resolved, setMode };
}

function colorSchemeMediaQuery(): MediaQueryList | null {
  if (typeof window === "undefined") {
    return null;
  }
  try {
    return window.matchMedia?.("(prefers-color-scheme: dark)") ?? null;
  } catch {
    return null;
  }
}

function ReplyDialog({
  thread,
  onCancel,
  onSubmit,
}: {
  thread: Thread | undefined;
  onCancel: () => void;
  onSubmit: (value: string) => void;
}) {
  const isDecision = (thread?.kind ?? "decision") === "decision";
  return (
    <PromptDialog
      title={isDecision ? "Add note for next turn" : "Add private note"}
      description={
        isDecision
          ? "This note is included in the next handoff because the thread is marked Decision."
          : "This note stays private unless you switch the thread to Decision."
      }
      label="Note"
      submitLabel={isDecision ? "Send to next turn" : "Save private note"}
      placeholder="Type a note…"
      onCancel={onCancel}
      onSubmit={onSubmit}
    />
  );
}
