package review

import (
	"bytes"
	"encoding/json"

	"github.com/AlhasanIQ/planmaxx/internal/session"
)

// migrateLoadedSession is the ordered compatibility pipeline for session
// semantics that are newer than the autosave envelope itself. Every step is
// idempotent; callers persist only when the exported session state changed.
func migrateLoadedSession(value *session.Session, status string, reopenTerminal bool) (string, bool, error) {
	terminal := reopenTerminal && isTerminalAutosaveStatus(status)
	before, err := json.Marshal(value)
	if err != nil {
		return status, false, err
	}

	migrateLegacyReviewProposal(value)
	rebuildPendingProposal(value)
	value.RepairInvalidOpenAnchors()
	if terminal {
		status = "active"
		value.SetDigest(session.Digest{})
	}
	value.RestoreCounters()

	after, err := json.Marshal(value)
	if err != nil {
		return status, false, err
	}
	return status, !bytes.Equal(before, after) || terminal, nil
}
