import { ArrowRight, CheckCircle2, X } from "lucide-react";

interface Props {
  state: "finalized" | "canceled";
}

export function CompletedScreen({ state }: Props) {
  const canceled = state === "canceled";

  return (
    <div className="grid min-h-full place-items-center px-4 py-16">
      <div className="max-w-md text-center">
        <div
          className={`mx-auto mb-4 grid h-14 w-14 place-items-center rounded-full ${
            canceled
              ? "bg-surface-muted text-foreground-muted"
            : "bg-accent text-white"
          }`}
        >
          {canceled ? <X size={28} /> : <CheckCircle2 size={28} />}
        </div>
        <h1 className="text-xl font-semibold">
          {canceled ? "Review canceled" : "Review finalized"}
        </h1>
        {!canceled ? (
          <p
            className="mt-2 inline-flex items-center gap-1.5 rounded-full bg-accent/10 px-3 py-1 text-xs font-medium text-accent"
          >
            <ArrowRight size={12} /> Handoff sent to Codex
          </p>
        ) : null}
        <p className="mt-3 text-sm text-foreground-muted">
          {canceled
            ? "PlanMaxx exited without handing off. You can close this window."
            : "Codex has resumed with the approved plan and your reviewer items. You can close this window."}
        </p>
      </div>
    </div>
  );
}
