import type { ChangeView, Revision } from "../../types";
import { Modal } from "../Modal";
import { RevisionPanel } from "../RevisionPanel";

interface Props {
  currentRevisionId: string;
  revisions: Revision[];
  diff: ChangeView | null;
  loading: boolean;
  error: string | null;
  disabled: boolean;
  onCompare: (from: string, to: string) => void;
  onClearCompare: () => void;
  onRestore: (revisionId: string) => void;
  onClose: () => void;
}

export function RevisionDialog({ currentRevisionId, revisions, onClose, ...panelProps }: Props) {
  return (
    <Modal
      title="Revisions"
      description={`Checked out: ${currentRevisionId || "none"}`}
      size="lg"
      onClose={onClose}
    >
      <RevisionPanel
        {...panelProps}
        currentRevisionId={currentRevisionId}
        revisions={revisions}
        showHeader={false}
      />
    </Modal>
  );
}
