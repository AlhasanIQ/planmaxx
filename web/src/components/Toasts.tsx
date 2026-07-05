import { useEffect } from "react";
import { AlertCircle, CheckCircle2, Info, X } from "lucide-react";

export type ToastKind = "info" | "success" | "error";

export interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
}

export function ToastStack({
  toasts,
  onDismiss,
}: {
  toasts: Toast[];
  onDismiss: (id: number) => void;
}) {
  useEffect(() => {
    const timers = toasts.map((t) =>
      window.setTimeout(() => onDismiss(t.id), t.kind === "error" ? 5000 : 2500),
    );
    return () => timers.forEach((t) => window.clearTimeout(t));
  }, [toasts, onDismiss]);

  return (
    <div className="pointer-events-none fixed bottom-4 right-4 z-50 flex flex-col items-end gap-2">
      {toasts.map((t) => {
        const Icon = t.kind === "error" ? AlertCircle : t.kind === "success" ? CheckCircle2 : Info;
        return (
          <div key={t.id} className={`toast ${t.kind === "error" ? "is-error" : t.kind === "success" ? "is-success" : ""}`}>
            <Icon size={16} />
            <span className="text-foreground">{t.message}</span>
            <button
              type="button"
              className="ml-1 text-foreground-muted hover:text-foreground"
              onClick={() => onDismiss(t.id)}
              aria-label="Dismiss"
            >
              <X size={14} />
            </button>
          </div>
        );
      })}
    </div>
  );
}
