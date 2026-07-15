package review

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/session"
)

// ImportLegacyRevisionHistory reconstructs visible session history from a
// former project-local bundle or shared bare repository while preserving IDs.
func ImportLegacyRevisionHistory(sourcePath, ref string, seed session.Session) (session.Session, bool, error) {
	repo, err := materializeBundle(sourcePath)
	if err != nil {
		return seed, false, err
	}
	defer os.RemoveAll(repo)
	if oid, err := refOID(repo, ref); err != nil {
		return seed, false, err
	} else if oid == "" {
		return seed, false, nil
	}
	out, err := gitOutput(repo, nil, "log", "--reverse", "--first-parent", "--format=%H%x00%aI%x00%s", ref)
	if err != nil {
		return seed, false, fmt.Errorf("read legacy revision history: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return seed, false, nil
	}
	imported := seed
	imported.Revisions = nil
	imported.PendingProposal = nil
	for index, line := range lines {
		fields := strings.SplitN(line, "\x00", 3)
		if len(fields) != 3 {
			return seed, false, fmt.Errorf("legacy revision metadata is malformed at commit %d", index+1)
		}
		createdAt, err := time.Parse(time.RFC3339, fields[1])
		if err != nil {
			return seed, false, fmt.Errorf("parse legacy revision time: %w", err)
		}
		plan, err := readPlanAt(repo, fields[0])
		if err != nil {
			return seed, false, err
		}
		id := "rev-" + strconv.Itoa(index+1)
		parentID := ""
		source := session.RevisionSourceInitial
		if index > 0 {
			parentID = "rev-" + strconv.Itoa(index)
			source = session.RevisionSourceTurn
			if strings.Contains(strings.ToLower(fields[2]), "outside planmaxx") {
				source = session.RevisionSourceExternal
			}
		}
		imported.Revisions = append(imported.Revisions, session.Revision{
			ID: id, CommitID: fields[0], ParentID: parentID, Source: source,
			CreatedAt: createdAt, Plan: plan, Summary: fields[2],
		})
		imported.CurrentRevisionID = id
		imported.Plan = plan
	}
	imported.RestoreCounters()
	if err := imported.Validate(); err != nil {
		return seed, false, fmt.Errorf("validate imported legacy history: %w", err)
	}
	return imported, true, nil
}
