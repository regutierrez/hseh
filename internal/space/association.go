package space

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/regutierrez/hseh/internal/config"
	"github.com/regutierrez/hseh/internal/herdr"
)

// AssociationRecord is one definition-id + resolved-dir to live workspace link.
type AssociationRecord struct {
	DefinitionID string `json:"definition_id"`
	ResolvedDir  string `json:"resolved_dir"`
	WorkspaceID  string `json:"workspace_id"`
}

// AssociationState is session-scoped association provenance for one proven server.
// Unresolved holds identities from earlier server generations that need explicit recovery.
type AssociationState struct {
	Witness    herdr.ContinuityWitness `json:"witness"`
	Records    []AssociationRecord     `json:"records"`
	Unresolved []AssociationRecord     `json:"unresolved"`
}

// AssociationFilePath is where a session's associations are persisted.
func AssociationFilePath(stateDir string) string {
	return filepath.Join(config.SessionHistoryDir(stateDir), "associations.json")
}

func openLockPath(stateDir string) string {
	return filepath.Join(config.SessionHistoryDir(stateDir), "space-open.lock")
}

func AssociationIdentityKey(definitionID, resolvedDir string) string {
	return definitionID + "\n" + resolvedDir
}

func LoadAssociationFile(stateDir string) (AssociationState, error) {
	payload, err := os.ReadFile(AssociationFilePath(stateDir))
	if err != nil {
		if os.IsNotExist(err) {
			return AssociationState{}, nil
		}
		return AssociationState{}, fmt.Errorf("hseh association: read: %w", err)
	}
	var state AssociationState
	if err := json.Unmarshal(payload, &state); err != nil {
		return AssociationState{}, fmt.Errorf("hseh association: corrupt state: %w", err)
	}
	return state, nil
}

func WriteAssociationFile(stateDir string, state AssociationState) error {
	dir := config.SessionHistoryDir(stateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("hseh association: mkdir: %w", err)
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("hseh association: encode: %w", err)
	}
	temporaryPath := filepath.Join(dir, fmt.Sprintf("associations.%d.tmp", os.Getpid()))
	if err := os.WriteFile(temporaryPath, append(payload, '\n'), 0o600); err != nil {
		return fmt.Errorf("hseh association: write: %w", err)
	}
	if err := os.Rename(temporaryPath, AssociationFilePath(stateDir)); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("hseh association: rename: %w", err)
	}
	return nil
}

func associationWitnessMismatch(state AssociationState, live herdr.ContinuityWitness) bool {
	fresh := state.Witness.PeerPID == 0 && state.Witness.PeerStartTime == "" && len(state.Records) == 0
	if fresh {
		return false
	}
	return !herdr.SameContinuityWitness(state.Witness, live)
}

// sameIdentity reports whether two records name the same definition id + resolved dir.
func sameIdentity(a, b AssociationRecord) bool {
	return a.DefinitionID == b.DefinitionID && a.ResolvedDir == b.ResolvedDir
}

func hasUnresolvedAssociation(state AssociationState, identity AssociationRecord) bool {
	for _, record := range state.Unresolved {
		if sameIdentity(record, identity) {
			return true
		}
	}
	return false
}

func appendUnresolvedAssociation(state AssociationState, record AssociationRecord) AssociationState {
	if !hasUnresolvedAssociation(state, record) {
		state.Unresolved = append(state.Unresolved, record)
	}
	return state
}

func removeUnresolvedAssociation(state AssociationState, identity AssociationRecord) AssociationState {
	kept := []AssociationRecord{}
	for _, record := range state.Unresolved {
		if !sameIdentity(record, identity) {
			kept = append(kept, record)
		}
	}
	state.Unresolved = kept
	return state
}

// ReconcileAssociationState folds mismatched current records into unresolved in memory.
// It does not write. Picker reads must not persist a witness reset.
func ReconcileAssociationState(state AssociationState, live herdr.ContinuityWitness) AssociationState {
	if state.Records == nil {
		state.Records = []AssociationRecord{}
	}
	if state.Unresolved == nil {
		state.Unresolved = []AssociationRecord{}
	}
	if !associationWitnessMismatch(state, live) {
		state.Witness = live
		return state
	}
	for _, record := range state.Records {
		state = appendUnresolvedAssociation(state, record)
	}
	state.Records = []AssociationRecord{}
	state.Witness = live
	return state
}

func LoadReconciledAssociationState(stateDir string, live herdr.ContinuityWitness) (AssociationState, error) {
	state, err := LoadAssociationFile(stateDir)
	if err != nil {
		return AssociationState{}, err
	}
	return ReconcileAssociationState(state, live), nil
}

// RecoverUsage is the `hseh recover` argument contract, shared by the CLI parser and Recover.
const RecoverUsage = "exactly one of --workspace <live-workspace-id> or --create is required"

// RecoveryHintLines is the tag plus the exact commands that resolve a stale identity.
func RecoveryHintLines(definitionID string) []string {
	return []string{
		"recovery needed",
		"hseh recover " + definitionID + " --workspace <live-workspace-id>",
		"hseh recover " + definitionID + " --create",
	}
}

func recoveryNeededError(definitionID string) error {
	hints := RecoveryHintLines(definitionID)
	return fmt.Errorf("hseh open: definition %s needs recovery after Herdr restart; use `%s` or `%s`", definitionID, hints[1], hints[2])
}

func upsertAssociation(state AssociationState, record AssociationRecord) AssociationState {
	replaced := false
	for i, existing := range state.Records {
		if sameIdentity(existing, record) {
			state.Records[i] = record
			replaced = true
			break
		}
	}
	if !replaced {
		state.Records = append(state.Records, record)
	}
	return state
}

func liveWorkspaceIDs(snapshot herdr.SessionSnapshot) map[string]bool {
	live := map[string]bool{}
	for _, workspace := range snapshot.Workspaces {
		if workspace.WorkspaceID != "" {
			live[workspace.WorkspaceID] = true
		}
	}
	return live
}

func workspaceAssociatedToOtherDefinition(state AssociationState, workspaceID string, identity AssociationRecord) bool {
	for _, record := range state.Records {
		if record.WorkspaceID == workspaceID && !sameIdentity(record, identity) {
			return true
		}
	}
	return false
}

func exactLiveAssociation(state AssociationState, snapshot herdr.SessionSnapshot, identity AssociationRecord) (string, bool) {
	live := liveWorkspaceIDs(snapshot)
	for _, record := range state.Records {
		if sameIdentity(record, identity) && live[record.WorkspaceID] {
			return record.WorkspaceID, true
		}
	}
	return "", false
}

func AssociatedLiveDefinitionKeys(state AssociationState, snapshot herdr.SessionSnapshot) map[string]bool {
	live := liveWorkspaceIDs(snapshot)
	keys := map[string]bool{}
	for _, record := range state.Records {
		if live[record.WorkspaceID] {
			keys[AssociationIdentityKey(record.DefinitionID, record.ResolvedDir)] = true
		}
	}
	return keys
}
