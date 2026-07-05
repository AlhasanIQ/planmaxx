import { useEffect, useRef } from "react";
import { X } from "lucide-react";

interface ModalProps {
  title: string;
  description?: string;
  onClose: () => void;
  children: React.ReactNode;
  footer?: React.ReactNode;
  size?: "sm" | "md" | "lg";
}

export function Modal({ title, description, onClose, children, footer, size = "md" }: ModalProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const cardRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const previousFocus = document.activeElement as HTMLElement | null;
    const dialog = dialogRef.current;
    if (dialog && !dialog.open) {
      dialog.showModal();
    }
    const node = cardRef.current;
    const focusTarget = node?.querySelector<HTMLElement>(
      "[data-modal-focus], input,textarea,select,button:not([tabindex='-1']),[tabindex]:not([tabindex='-1'])",
    );
    focusTarget?.focus();

    return () => {
      if (dialog?.open) {
        dialog.close();
      }
      previousFocus?.focus?.();
    };
  }, []);

  const width = size === "lg" ? "max-w-[760px]" : size === "sm" ? "max-w-[420px]" : "max-w-[560px]";

  return (
    <dialog
      ref={dialogRef}
      className="modal-overlay"
      aria-label={title}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
    >
      <button
        type="button"
        className="modal-backdrop-hitbox"
        tabIndex={-1}
        aria-label="Close dialog"
        onClick={onClose}
      />
      <div ref={cardRef} className={`modal-card ${width}`}>
        <header className="flex items-start justify-between gap-3 border-b border-border px-5 pt-4 pb-3">
          <div>
            <h2 className="text-base font-semibold text-foreground">{title}</h2>
            {description ? (
              <p className="mt-0.5 text-xs text-foreground-muted">{description}</p>
            ) : null}
          </div>
          <button type="button" className="btn btn-ghost btn-sm -mr-1.5" aria-label="Close" onClick={onClose}>
            <X size={16} />
          </button>
        </header>
        <div className="flex-1 overflow-y-auto px-5 py-4 text-sm">{children}</div>
        {footer ? (
          <footer className="flex items-center justify-end gap-2 border-t border-border bg-surface-muted/40 px-5 py-3">
            {footer}
          </footer>
        ) : null}
      </div>
    </dialog>
  );
}
