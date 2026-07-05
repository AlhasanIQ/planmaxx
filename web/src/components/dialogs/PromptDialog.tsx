import { useState } from "react";
import { Modal } from "../Modal";

interface Props {
  title: string;
  description?: string;
  label: string;
  placeholder?: string;
  initialValue?: string;
  submitLabel?: string;
  multiline?: boolean;
  onCancel: () => void;
  onSubmit: (value: string) => void;
  validate?: (value: string) => string | null;
}

export function PromptDialog({
  title,
  description,
  label,
  placeholder,
  initialValue = "",
  submitLabel = "Save",
  multiline = true,
  onCancel,
  onSubmit,
  validate,
}: Props) {
  const [value, setValue] = useState(initialValue);
  const [err, setErr] = useState<string | null>(null);

  function submit() {
    const trimmed = value.trim();
    if (!trimmed) {
      setErr("Cannot be empty");
      return;
    }
    if (validate) {
      const e = validate(trimmed);
      if (e) {
        setErr(e);
        return;
      }
    }
    onSubmit(trimmed);
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      submit();
    }
  }

  return (
    <Modal
      title={title}
      description={description ?? "Cmd/Ctrl + Enter to submit. Esc to cancel."}
      onClose={onCancel}
      footer={
        <>
          <button type="button" className="btn" onClick={onCancel}>
            Cancel
          </button>
          <button type="button" className="btn-primary btn" onClick={submit}>
            {submitLabel}
          </button>
        </>
      }
    >
      <label className="block text-xs font-semibold text-foreground-muted">
        {label}
        {multiline ? (
          <textarea
            value={value}
            placeholder={placeholder}
            onChange={(e) => {
              setErr(null);
              setValue(e.target.value);
            }}
            onKeyDown={onKeyDown}
            rows={5}
            data-modal-focus
            className="field mt-1 resize-y"
          />
        ) : (
          <input
            value={value}
            placeholder={placeholder}
            onChange={(e) => {
              setErr(null);
              setValue(e.target.value);
            }}
            onKeyDown={onKeyDown}
            data-modal-focus
            className="field mt-1"
          />
        )}
      </label>
      {err ? <p className="mt-2 text-xs text-danger">{err}</p> : null}
    </Modal>
  );
}
