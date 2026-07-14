import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ApiError, api } from "./api";
import { TopBar } from "./components/TopBar";
import { Plan, type CommentView } from "./components/Plan";
import { RevisionDialog } from "./components/dialogs/RevisionDialog";
import { ToastStack, type Toast } from "./components/Toasts";
import { CompletedScreen } from "./components/CompletedScreen";
import { PromptDialog } from "./components/dialogs/PromptDialog";
import { ConfirmDialog } from "./components/dialogs/ConfirmDialog";
import { SubmissionReviewDialog, type SubmissionMode } from "./components/dialogs/SubmissionReviewDialog";
import type { Anchor, Digest, RevisionComparison, Session, Thread, ThreadIntent } from "./types";
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
import { useRevisionComparison } from "./hooks/useRevisionComparison";

type CompletionState = null | "finalized" | "canceled";
type ThreadAgentAction = "asking" | "iterating";

type DialogState =
  | null
  | { kind: "reply"; threadId: string }
  | { kind: "delete"; threadId: string }
  | { kind: "ask"; thread: Thread }
  | { kind: "submission"; mode: SubmissionMode; digest: Digest }
  | { kind: "revisions" }
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
  const operationInFlightRef = useRef(false);
  const [filter, setFilter] = useState("");
  const [dialog, setDialog] = useState<DialogState>(null);
  const [hoveredThreadId, setHoveredThreadId] = useState<string | null>(null);
  const [focusedThreadId, setFocusedThreadId] = useState<string | null>(null);
  const [sideQuestionsEnabled, setSideQuestionsEnabled] = useState(true);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const theme = useTheme();
  const [editingThreadId, setEditingThreadId] = useState<string | null>(null);
  const [threadAgentActions, setThreadAgentActions] = useState<Record<string, ThreadAgentAction>>({});
  const [iteratingSection, setIteratingSection] = useState(false);
  const iteratingSectionRef = useRef(false);
  const initialComparisonRequestedRef = useRef(false);

  const pushToast = useCallback((kind: Toast["kind"], message: string) => {
    setToasts((prev) => [...prev, { id: Date.now() + Math.random(), kind, message }]);
  }, []);
  const dismissToast = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);
	const {
	  diff: revisionDiff,
	  loading: revisionDiffLoading,
	  error: revisionDiffError,
	  compare: compareRevision,
	  reload: reloadRevisionComparison,
	  clear: clearRevisionDiff,
	  suppress: suppressRevisionDiff,
	} = useRevisionComparison((message) => pushToast("error", message));

	const reloadVisibleComparison = useCallback(async () => {
	  if (!revisionDiff) return;
	  await reloadRevisionComparison(revisionDiff.baseId, revisionDiff.targetId);
	}, [reloadRevisionComparison, revisionDiff]);

  const refresh = useCallback(async () => {
    try {
      const next = await api.getState();
      setSession(next);
      setLoadError(null);
      setStatus({ label: "Codex paused — review in progress", kind: "idle" });
	  if (next.pendingProposal) {
	  suppressRevisionDiff();
	  }
      if (!initialComparisonRequestedRef.current) {
        initialComparisonRequestedRef.current = true;
        const currentRevision = next.revisions.find((revision) => revision.id === next.currentRevisionId);
        if (!next.pendingProposal && currentRevision?.parentId) {
          void handleCompareRevision(currentRevision.parentId, currentRevision.id);
        }
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Failed to load review";
      setLoadError(msg);
      setStatus({ label: msg, kind: "error" });
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const updateThreadIntent = useCallback(async (threadId: string, intent: ThreadIntent) => {
    const ok = await withBusy("Updating feedback…", () => api.setThreadIntent(threadId, intent));
    if (ok) await refresh();
  }, [refresh]);

  const openSubmission = useCallback(async (mode: SubmissionMode) => {
    if (!session) return;
    if (session.pendingProposal) {
      pushToast("error", "Apply or discard the pending proposal first");
      return;
    }
    const digest = await withBusy(mode === "iterate" ? "Preparing iteration review…" : "Preparing approval review…", () => api.digestDraft());
    if (digest) setDialog({ kind: "submission", mode, digest });
  }, [pushToast, session]);
  const openFinalize = useCallback(() => openSubmission("finalize"), [openSubmission]);
  const openIterate = useCallback(() => openSubmission("iterate"), [openSubmission]);
  const counts = session?.counts ?? { activeInstructions: 0, activePrivateNotes: 0, includedAnswers: 0, privateAnswers: 0, detachedFeedback: 0, addressedHistory: 0 };

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
    if (operationInFlightRef.current) return null;
    operationInFlightRef.current = true;
    setBusy(true);
    setStatus({ label, kind: "busy" });
    try {
      const result = await fn();
      setStatus({ label: "Codex paused — review in progress", kind: "idle" });
      return result;
    } catch (e) {
      if (isSourceChangeConflict(e)) {
        await refresh();
        setDialog(null);
        setEditingThreadId(null);
        pushToast("error", "The plan changed outside PlanMaxx. Review state was refreshed.");
        return null;
      }
      const msg = e instanceof Error ? e.message : "Request failed";
      setStatus({ label: msg, kind: "error" });
      pushToast("error", msg);
      return null;
    } finally {
      operationInFlightRef.current = false;
      setBusy(false);
    }
  }

  async function handleCreateThread(anchor: Anchor, body: string, selectedText: string, intent: ThreadIntent = "instruction"): Promise<boolean> {
    const result = await withBusy("Adding comment…", async () => {
      const thread = await api.createThread(anchor, body, selectedText, intent);
      return thread;
    });
    if (result) {
	  await refresh();
	  await reloadVisibleComparison();
      setFocusedThreadId(result.id);
      pushToast("success", intent === "instruction" ? "Feedback added — ready for iteration" : "Private note added");
      return true;
    }
    return false;
  }

  async function handleCreateThreadAndAsk(anchor: Anchor, body: string, selectedText: string): Promise<boolean> {
    if (!session) return false;
    const result = await withBusy("Adding /btw comment…", async () =>
      api.createThread(anchor, body, selectedText, "instruction"),
    );
	if (!result) return false;

	const nextSession = { ...session, threads: [...session.threads, result] };
	await refresh();
	await reloadVisibleComparison();
    setFocusedThreadId(result.id);
    return askSideQuestion(result, body, nextSession);
  }

  async function handleIterateDraft(anchor: Anchor, instruction: string, selectedText: string): Promise<boolean> {
    const created = await withBusy("Saving feedback…", () => api.createThread(anchor, instruction, selectedText, "instruction"));
    if (!created) return false;
    await refresh();
    setFocusedThreadId(created.id);
    return handleIterateSection(anchor, instruction, created.id);
  }

  async function handleIterateSection(anchor: Anchor, instruction: string, threadId?: string): Promise<boolean> {
    if (iteratingSectionRef.current) return false;
    iteratingSectionRef.current = true;
    setIteratingSection(true);
    try {
      const result = await withBusy("Iterating section…", () =>
        api.proposeSection(threadId, anchor, instruction),
      );
      if (!result) return false;
	  await refresh();
		suppressRevisionDiff();
      pushToast("success", "Proposal ready");
      return true;
    } finally {
      iteratingSectionRef.current = false;
      setIteratingSection(false);
    }
  }

  async function handleIterateThread(thread: Thread) {
    const instruction = thread.messages.at(-1)?.body.trim() || "Revise this section according to this thread.";
    setThreadAgentActions((prev) => ({ ...prev, [thread.id]: "iterating" }));
    try {
      await handleIterateSection(thread.anchor, instruction, thread.id);
    } finally {
      setThreadAgentActions((prev) => {
        const next = { ...prev };
        delete next[thread.id];
        return next;
      });
    }
  }

  async function handleUpdateThread(threadId: string, anchor: Anchor, body: string, selectedText: string): Promise<boolean> {
    const ok = await withBusy("Saving comment…", () => api.editThread(threadId, anchor, body, selectedText));
    if (ok) {
	  await refresh();
	  await reloadVisibleComparison();
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
	  await reloadVisibleComparison();
      pushToast("info", "Thread deleted");
    }
  }

  async function handleAsk(thread: Thread, question: string) {
    setDialog(null);
    return askSideQuestion(thread, question, session);
  }

  async function askSideQuestion(thread: Thread, question: string, sourceSession: Session | null): Promise<boolean> {
    if (!sourceSession) return false;
    if (operationInFlightRef.current) return false;
    operationInFlightRef.current = true;
    setBusy(true);
    setStatus({ label: "Asking Codex (ephemeral /btw)…", kind: "busy" });
    setFocusedThreadId(thread.id);
    setThreadAgentActions((prev) => ({ ...prev, [thread.id]: "asking" }));
    try {
	  await api.sideQuestion(thread.id, question, sideQuestionContext(sourceSession, thread));
	  await refresh();
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
      setThreadAgentActions((prev) => {
        const next = { ...prev };
        delete next[thread.id];
        return next;
      });
      operationInFlightRef.current = false;
      setBusy(false);
    }
  }

  async function handlePromote(answerId: string) {
    const ok = await withBusy("Including answer…", () => api.promote(answerId));
    if (ok) {
      await refresh();
      pushToast("success", "/btw answer will be used for iteration or approval");
    }
  }
  async function handleUnpromote(answerId: string) {
    const ok = await withBusy("Keeping answer private…", () => api.unpromote(answerId));
    if (ok) {
      await refresh();
      pushToast("info", "Answer kept private");
    }
  }

  async function handleCreateFollowUp(threadId: string) {
    const thread = await withBusy("Creating follow-up…", () => api.createFollowUp(threadId));
    if (!thread) return;
    await refresh();
    setFocusedThreadId(thread.id);
    pushToast("success", "Follow-up created as active feedback");
  }

  async function handleApplyProposal(proposalId: string) {
    const revision = await withBusy("Applying proposal…", () => api.applyProposal(proposalId));
    if (!revision) return;
    if (revision.parentId) {
      await Promise.all([
        refresh(),
        handleCompareRevision(revision.parentId, revision.id),
      ]);
    } else {
      await refresh();
	  suppressRevisionDiff();
    }
    pushToast("success", "Proposal applied");
  }

  async function handleDiscardProposal(proposalId: string) {
    const ok = await withBusy("Discarding proposal…", () => api.discardProposal(proposalId));
    if (!ok) return;
	await refresh();
    pushToast("info", "Proposal discarded");
  }

  async function handleCompareRevision(from: string, to: string) {
	await compareRevision(from, to);
  }

  function handleClearRevisionDiff() {
	clearRevisionDiff();
  }

  async function handleRestoreRevision(revisionId: string) {
    const restored = await withBusy("Restoring revision…", () => api.restoreRevision(revisionId));
    if (!restored) return;
	clearRevisionDiff();
    await refresh();
    pushToast("success", `Restored ${revisionId} as ${restored.id}`);
  }

  async function handleFinalize(digest: Digest) {
    setDialog(null);
    const ok = await withBusy("Finalizing…", () => api.finalize(digest));
    if (ok) {
      setCompletion("finalized");
      setStatus({ label: "Finalized — handoff sent", kind: "success" });
    }
  }

  async function handlePlanIteration(digest: Digest) {
    if (!session) return;
    setDialog(null);
    if (iteratingSectionRef.current) return;
    iteratingSectionRef.current = true;
    setIteratingSection(true);
    try {
      const result = await withBusy("Iterating complete plan…", () => api.proposeReview(digest));
      if (!result) return;
	  await refresh();
	  suppressRevisionDiff();
      pushToast("success", "Whole-plan proposal ready — apply it to create a new revision");
    } finally {
      iteratingSectionRef.current = false;
      setIteratingSection(false);
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
    threadAgentActions,
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
    handleClearRevisionDiff,
    handleRestoreRevision,
    handleCreateThread,
    handleCreateThreadAndAsk,
	 handleCreateFollowUp,
    handleDelete,
    handleDiscardProposal,
    handleFinalize,
    handleIterateSection,
	 handleIterateDraft,
    handleIterateThread,
    handlePromote,
    handlePlanIteration,
    handleReply,
    handleUnpromote,
    handleUpdateThread,
    hoveredThreadId,
    loadError,
    openFinalize,
	openIterate,
    refresh,
    revisionDiff,
    revisionDiffError,
    revisionDiffLoading,
    iteratingSection,
    session,
    setDialog,
    setEditingThreadId,
    setFilter,
    setFocusedThreadId,
    setHoveredThreadId,
    sideQuestionsEnabled,
    status,
    theme,
    toasts,
	updateThreadIntent,
  };
}

function isSourceChangeConflict(error: unknown): error is ApiError {
  return (
    error instanceof ApiError &&
    error.status === 409 &&
    (error.message.includes("source plan changed outside PlanMaxx") || error.message.includes("review state changed in another PlanMaxx session"))
  );
}

type ReviewController = ReturnType<typeof useReviewController>;

export default function App() {
  const controller = useReviewController();
  return <ReviewScreen controller={controller} />;
}

function ReviewScreen({ controller }: { controller: ReviewController }) {
  const {
    threadAgentActions,
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
    handleClearRevisionDiff,
    handleRestoreRevision,
    handleCreateThread,
    handleCreateThreadAndAsk,
	handleCreateFollowUp,
    handleDelete,
    handleDiscardProposal,
    handleFinalize,
    handleIterateSection,
	handleIterateDraft,
    handleIterateThread,
    handlePromote,
    handlePlanIteration,
    handleReply,
    handleUnpromote,
    handleUpdateThread,
    hoveredThreadId,
    loadError,
    openFinalize,
	openIterate,
    refresh,
    revisionDiff,
    revisionDiffError,
    revisionDiffLoading,
    iteratingSection,
    session,
    setDialog,
    setEditingThreadId,
    setFilter,
    setFocusedThreadId,
    setHoveredThreadId,
    sideQuestionsEnabled,
    status,
    theme,
    toasts,
	updateThreadIntent,
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
  const [commentView, setCommentView] = useState<CommentView>("inline");
  const revisionComparison = useMemo<RevisionComparison | null>(() => {
	if (!session || session.pendingProposal || !revisionDiff) return null;
	return revisionDiff;
	}, [revisionDiff, session]);

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
		forIterationCount={counts.activeInstructions + counts.includedAnswers}
		privateCount={counts.activePrivateNotes + counts.privateAnswers}
		attentionCount={counts.detachedFeedback}
        themeMode={theme.mode}
        resolvedTheme={theme.resolved}
        onThemeModeChange={changeThemeMode}
        currentRevisionId={session.currentRevisionId}
        onOpenRevisions={() => setDialog({ kind: "revisions" })}
        onCancel={() => setDialog({ kind: "confirmCancel" })}
		onIterate={openIterate}
        onFinalize={openFinalize}
        disabled={busy}
		iterateDisabled={!session.capabilities.canIterate}
		finalizeDisabled={!session.capabilities.canFinalize}
      />

      <main className={`mx-auto w-full px-4 py-5 ${commentView === "alongside" ? "max-w-[1600px]" : "max-w-[1240px]"}`}>
        <Plan
          plan={session.plan}
          planFormat={session.planFormat}
          theme={theme.resolved}
          proposal={session.pendingProposal}
		  proposalChange={session.activeChange}
          comparison={revisionComparison}
          comparisonLoading={revisionDiffLoading}
          onClearComparison={handleClearRevisionDiff}
          threads={session.threads}
          sideAnswers={session.sideAnswers}
          hoveredThreadId={hoveredThreadId}
          focusedThreadId={focusedThreadId}
          editingThread={session.threads.find((t) => t.id === editingThreadId) ?? null}
          commentView={commentView}
          commentFilter={filter}
          onCommentViewChange={setCommentView}
          onCommentFilterChange={setFilter}
		  onCreateComment={handleCreateThread}
          onUpdateComment={handleUpdateThread}
          onAskSideFromDraft={handleCreateThreadAndAsk}
		  onIterateDraft={handleIterateDraft}
          disabled={busy || Boolean(session.pendingProposal)}
		  proposalDisabled={busy || !session.capabilities.canApplyProposal}
          proposalIterating={iteratingSection}
          onApplyProposal={handleApplyProposal}
          onDiscardProposal={handleDiscardProposal}
          onIterateProposal={(anchor, instruction) => handleIterateSection(anchor, instruction)}
          onEditDone={clearEditingThread}
          onFocusThread={focusThreadTemporarily}
          onHoverThread={setHoveredThreadId}
		  onSetThreadIntent={updateThreadIntent}
          onReplyThread={(id) => setDialog({ kind: "reply", threadId: id })}
          onDeleteThread={(id) => setDialog({ kind: "delete", threadId: id })}
          onEditThread={(id) => {
            setEditingThreadId(id);
            setFocusedThreadId(id);
          }}
		  onCreateFollowUp={handleCreateFollowUp}
          onAskSide={(thread) => setDialog({ kind: "ask", thread })}
          onIterateThread={handleIterateThread}
		  onIncludeAnswer={handlePromote}
		  onKeepAnswerPrivate={handleUnpromote}
          threadAgentActions={threadAgentActions}
          sideQuestionsEnabled={sideQuestionsEnabled}
        />

      </main>

      {dialog?.kind === "revisions" && (
        <RevisionDialog
          currentRevisionId={session.currentRevisionId}
          revisions={session.revisions}
          diff={revisionDiff}
          loading={revisionDiffLoading}
          error={revisionDiffError}
          disabled={busy || !session.capabilities.canRestoreRevision}
          onCompare={(from, to) => {
            setDialog(null);
            void handleCompareRevision(from, to);
          }}
          onClearCompare={() => {
            setDialog(null);
            handleClearRevisionDiff();
          }}
          onRestore={(revisionId) => {
            setDialog(null);
            void handleRestoreRevision(revisionId);
          }}
          onClose={() => setDialog(null)}
        />
      )}

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
          description={`Anchored to ${anchorLabel(dialog.thread.anchor)}. Pre-filled with the latest comment in this thread; edit if you want to ask something different. Answers stay private unless you include them.`}
          label="Question"
          submitLabel="Ask /btw"
          placeholder="What should we ask Codex on the side?"
          initialValue={dialog.thread.messages.at(-1)?.body ?? ""}
          onCancel={() => setDialog(null)}
          onSubmit={(value) => handleAsk(dialog.thread, value)}
        />
      )}
      {dialog?.kind === "submission" && (
        <SubmissionReviewDialog
          mode={dialog.mode}
          initial={dialog.digest}
		  detachedCount={session.counts.detachedFeedback}
          onCancel={() => setDialog(null)}
		  onSubmit={dialog.mode === "iterate" ? handlePlanIteration : handleFinalize}
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
  const isInstruction = thread?.intent !== "private";
  return (
    <PromptDialog
      title={isInstruction ? "Add iteration feedback" : "Add private note"}
      description={
        isInstruction
          ? "This reply will be used when you iterate or approve."
          : "This reply stays private unless you change the feedback intent."
      }
      label="Note"
      submitLabel={isInstruction ? "Add feedback" : "Save private note"}
      placeholder="Type a note…"
      onCancel={onCancel}
      onSubmit={onSubmit}
    />
  );
}
