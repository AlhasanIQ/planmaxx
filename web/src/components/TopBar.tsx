import {
	AlertTriangle,
  ArrowRight,
  CheckCircle2,
  ChevronDown,
  EyeOff,
  History,
  Loader2,
  Monitor,
  Moon,
	Pause,
	Sparkles,
  Sun,
  XOctagon,
} from "lucide-react";
import type { ResolvedTheme, ThemeMode } from "../lib/theme";

interface Props {
  statusLabel: string;
  statusKind: "idle" | "busy" | "error" | "success";
  statusActor: "planmaxx" | "subagent";
  forIterationCount: number;
  privateCount: number;
  attentionCount: number;
  themeMode: ThemeMode;
  resolvedTheme: ResolvedTheme;
  onThemeModeChange: () => void;
  currentRevisionId: string;
  agentDisplayName: string;
  agentAvailable: boolean;
  agentUnavailableReason?: string;
  onOpenRevisions: () => void;
  onCancel: () => void;
  onCancelIteration: () => void;
  onIterate: () => void;
  onFinalize: () => void;
  disabled: boolean;
  iterationRunning?: boolean;
  finalizeDisabled?: boolean;
  iterateDisabled?: boolean;
}

export function TopBar(props: Props) {
  const {
    statusLabel,
    statusKind,
    statusActor,
    forIterationCount,
    privateCount,
    attentionCount,
    themeMode,
    resolvedTheme,
    onThemeModeChange,
    currentRevisionId,
    agentDisplayName,
    agentAvailable,
    agentUnavailableReason,
    onOpenRevisions,
    onCancel,
    onCancelIteration,
    onIterate,
    onFinalize,
    disabled,
    iterationRunning = false,
    finalizeDisabled = false,
    iterateDisabled = false,
  } = props;
  const ThemeIcon = themeMode === "system" ? Monitor : resolvedTheme === "dark" ? Moon : Sun;
  const themeLabel = themeMode === "system" ? "System" : resolvedTheme === "dark" ? "Dark" : "Light";
  return (
    <header className="sticky top-0 z-10 border-b border-border bg-surface-elevated/80 backdrop-blur">
      <div className="topbar-shell mx-auto flex h-14 max-w-[1600px] items-center gap-3 px-4">
        <div className="flex items-center gap-2.5">
          <span className="grid size-7 place-items-center rounded-md bg-accent text-white font-bold">
            P
          </span>
          <strong className="text-[15px]">PlanMaxx</strong>
        </div>
        <button
          type="button"
          className="btn btn-ghost shrink-0 whitespace-nowrap"
          onClick={onOpenRevisions}
          disabled={disabled || iterationRunning}
          title={`Revisions — current ${currentRevisionId || "none"}`}
          aria-label={`Revisions — current ${currentRevisionId || "none"}`}
        >
          <History size={13} />
          <span className="hidden lg:inline">Revisions</span>
          <strong className="whitespace-nowrap">{currentRevisionId || "none"}</strong>
          <ChevronDown size={12} aria-hidden />
        </button>
        <AgentStatus
          statusLabel={statusLabel}
          statusKind={statusKind}
          statusActor={statusActor}
          agentDisplayName={agentDisplayName}
          agentAvailable={agentAvailable}
          agentUnavailableReason={agentUnavailableReason}
        />
        <div className="ml-2 hidden gap-2 sm:flex">
          <span
            className="pill pill-go"
            title="Active feedback and included /btw answers used for iteration or approval"
          >
            <ArrowRight size={11} /> {forIterationCount} for iteration
          </span>
          <span
            className="pill pill-stay"
            title="Active private notes and private /btw answers stay in this review"
          >
            <EyeOff size={11} /> {privateCount} private
          </span>
          {attentionCount > 0 ? <span className="pill pill-attention" title="Detached feedback needs re-anchoring before it can be used"><AlertTriangle size={11} /> {attentionCount} need attention</span> : null}
        </div>
        <div className="topbar-actions ml-auto flex items-center gap-3">
          <button
            type="button"
            className="btn btn-ghost"
            onClick={onThemeModeChange}
            title={`Theme: ${themeLabel}`}
            aria-label={`Theme: ${themeLabel}`}
          >
            <ThemeIcon size={13} />
            <span className="hidden sm:inline">{themeLabel}</span>
          </button>
          <button type="button" className="btn topbar-action" onClick={onCancel} disabled={disabled || iterationRunning} aria-label="Cancel review">
            <XOctagon size={14} /><span>Cancel</span>
          </button>
          <button
            type="button"
            className="btn topbar-action"
            onClick={iterationRunning ? onCancelIteration : onIterate}
            disabled={disabled || (!iterationRunning && iterateDisabled)}
            aria-label={iterationRunning ? "Stop iteration" : "Iterate"}
          >
            {iterationRunning ? <XOctagon size={14} /> : <Sparkles size={14} />}
            <span>{iterationRunning ? "Stop iteration" : "Iterate"}</span>
          </button>
          <button
            type="button"
            className="btn btn-primary topbar-action"
            onClick={onFinalize}
            disabled={disabled || iterationRunning || finalizeDisabled}
          >
            <CheckCircle2 size={14} /> <span>Finalize</span>
          </button>
        </div>
      </div>
    </header>
  );
}

export function AgentStatus({
  statusLabel,
  statusKind,
  statusActor,
  agentDisplayName,
  agentAvailable,
  agentUnavailableReason,
}: Pick<Props, "statusLabel" | "statusKind" | "statusActor" | "agentDisplayName" | "agentAvailable" | "agentUnavailableReason">) {
  const agentName = agentDisplayName || "Agent";
  const isSubagentOperation = agentAvailable && statusActor === "subagent";
  const isRunning = statusKind === "busy";
  const isSubagentRunning = isSubagentOperation && isRunning;
  const assistanceUnavailable = !agentAvailable;
  const unavailableReason = agentUnavailableReason || "No supported agent harness is attached to this review.";
  const role = isSubagentOperation
    ? "Subagent"
    : isRunning
      ? "PlanMaxx"
      : assistanceUnavailable
        ? "Assistance off"
        : "Main agent";
  const headline = isSubagentOperation
    ? `${agentName} ${isRunning ? "running" : statusKind === "error" ? "failed" : "finished"}`
    : isRunning
      ? "Working"
      : agentAvailable
        ? `${agentName} waiting`
        : "Manual review only";
  const detail = isSubagentOperation
    ? `${statusLabel} Main agent is waiting for review.`
    : isRunning && agentAvailable
      ? `${statusLabel} Main ${agentName} is waiting.`
      : isRunning
        ? `${statusLabel} Assisted /btw and iteration remain unavailable: ${unavailableReason}`
      : agentAvailable
        ? "Waiting for you to finish the review."
        : `${unavailableReason} Agent-backed /btw and iteration are unavailable; comments and manual review still work.`;
  const Icon =
    isRunning
      ? Loader2
      : statusKind === "error"
        ? XOctagon
        : statusKind === "success"
          ? CheckCircle2
          : assistanceUnavailable
            ? AlertTriangle
            : Pause;
  const title = isSubagentOperation
    ? `${headline}. ${statusLabel} The main ${agentName} session remains paused until this review is finished.`
    : agentAvailable
      ? `Main ${agentName} is waiting for this review.${isRunning ? ` PlanMaxx is currently ${statusLabel.toLowerCase()}` : ""}`
      : `${unavailableReason} Agent-backed /btw and iteration are unavailable; comments and manual review still work.`;
  const visualState = isSubagentRunning
    ? "subagent"
    : assistanceUnavailable && !isRunning && statusKind === "idle"
      ? "unavailable"
      : statusKind;
  return (
    <div
      className={`agent-status is-${visualState}`}
      aria-live="polite"
      role="status"
      title={title}
    >
      <Icon size={13} className={isRunning ? "animate-spin" : ""} aria-hidden />
      <span className="agent-status-copy">
        <span className="agent-status-main">
          <span className="agent-status-role">{role}</span>
          <strong>{headline}</strong>
        </span>
        {detail ? <span className="agent-status-detail">{detail}</span> : null}
      </span>
    </div>
  );
}
