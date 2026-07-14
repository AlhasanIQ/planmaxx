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
  forIterationCount: number;
  privateCount: number;
  attentionCount: number;
  themeMode: ThemeMode;
  resolvedTheme: ResolvedTheme;
  onThemeModeChange: () => void;
  currentRevisionId: string;
  onOpenRevisions: () => void;
  onCancel: () => void;
  onIterate: () => void;
  onFinalize: () => void;
  disabled: boolean;
  finalizeDisabled?: boolean;
  iterateDisabled?: boolean;
}

export function TopBar(props: Props) {
  const {
    statusLabel,
    statusKind,
    forIterationCount,
    privateCount,
    attentionCount,
    themeMode,
    resolvedTheme,
    onThemeModeChange,
    currentRevisionId,
    onOpenRevisions,
    onCancel,
    onIterate,
    onFinalize,
    disabled,
    finalizeDisabled = false,
    iterateDisabled = false,
  } = props;
  const ThemeIcon = themeMode === "system" ? Monitor : resolvedTheme === "dark" ? Moon : Sun;
  const themeLabel = themeMode === "system" ? "System" : resolvedTheme === "dark" ? "Dark" : "Light";
  return (
    <header className="sticky top-0 z-10 border-b border-border bg-surface-elevated/80 backdrop-blur">
      <div className="mx-auto flex h-14 max-w-[1240px] items-center gap-3 px-4">
        <div className="flex items-center gap-2.5">
          <span className="grid size-7 place-items-center rounded-md bg-accent text-white font-bold">
            P
          </span>
          <strong className="text-[15px]">PlanMaxx</strong>
        </div>
        <button
          type="button"
          className="btn btn-ghost"
          onClick={onOpenRevisions}
          disabled={disabled}
          title={`Revisions — current ${currentRevisionId || "none"}`}
          aria-label={`Revisions — current ${currentRevisionId || "none"}`}
        >
          <History size={13} />
          <span className="hidden lg:inline">Revisions</span>
          <strong>{currentRevisionId || "none"}</strong>
          <ChevronDown size={12} aria-hidden />
        </button>
        <span
          className="codex-paused hidden md:inline-flex"
          title="Your Codex session is blocked on this review"
        >
          <Pause size={11} /> Codex paused
        </span>
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
        <div className="ml-auto flex items-center gap-3">
          <StatusBadge kind={statusKind} label={statusLabel} />
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
          <button type="button" className="btn" onClick={onCancel} disabled={disabled}>
            Cancel
          </button>
          <button
            type="button"
            className="btn"
            onClick={onIterate}
            disabled={disabled || iterateDisabled}
          >
            <Sparkles size={14} /> Iterate
          </button>
          <button
            type="button"
            className="btn btn-primary"
            onClick={onFinalize}
            disabled={disabled || finalizeDisabled}
          >
            <CheckCircle2 size={14} /> Finalize
          </button>
        </div>
      </div>
    </header>
  );
}

function StatusBadge({ kind, label }: { kind: Props["statusKind"]; label: string }) {
  const Icon =
    kind === "busy" ? Loader2 : kind === "error" ? XOctagon : kind === "success" ? CheckCircle2 : null;
  const color =
    kind === "error"
      ? "text-danger"
      : kind === "success"
        ? "text-success"
        : "text-foreground-muted";
  return (
    <div className={`hidden items-center gap-1.5 text-xs md:flex ${color}`} aria-live="polite">
      {Icon ? <Icon size={12} className={kind === "busy" ? "animate-spin" : ""} /> : null}
      <span>{label}</span>
    </div>
  );
}
