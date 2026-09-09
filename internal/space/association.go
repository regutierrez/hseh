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

func associationStatePath(stateDir string) string {
	return filepath.Join(config.SessionHistoryDir(stateDir), "associations.json")
}

func openLockPath(stateDir string) string {
	return filepath.Join(config.SessionHistoryDir(stateDir), "space-open.lock")
}

func AssociationIdentityKey(definitionID, resolvedDir string) string {
	return definitionID + "\n" + resolvedDir
}

func LoadAssociationFile(stateDir string) (AssociationState, error) {
	payload, err := os.ReadFile(associationStatePath(stateDir))
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
	if state.Records == nil {
		state.Records = []AssociationRecord{}
	}
	if state.Unresolved == nil {
		state.Unresolved = []AssociationRecord{}
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
	if err := os.Rename(temporaryPath, associationStatePath(stateDir)); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("hseh association: rename: %w", err)
	}
	return nil
}

func associationWitnessEmpty(witness herdr.ContinuityWitness) bool {
	return witness.PeerPID == 0 && witness.PeerStartTime == ""
}

func associationWitnessMismatch(state AssociationState, live herdr.ContinuityWitness) bool {
	if associationWitnessEmpty(state.Witness) && len(state.Records) == 0 {
		return false
	}
	return !herdr.SameContinuityWitness(state.Witness, live)
}

func appendUnresolvedAssociation(state AssociationState, record AssociationRecord) AssociationState {
	key := AssociationIdentityKey(record.DefinitionID, record.ResolvedDir)
	for _, existing := range state.Unresolved {
		if AssociationIdentityKey(existing.DefinitionID, existing.ResolvedDir) == key {
			return state
		}
	}
	state.Unresolved = append(state.Unresolved, record)
	return state
}

func removeUnresolvedAssociation(state AssociationState, definitionID, resolvedDir string) AssociationState {
	key := AssociationIdentityKey(definitionID, resolvedDir)
	kept := []AssociationRecord{}
	for _, record := range state.Unresolved {
		if AssociationIdentityKey(record.DefinitionID, record.ResolvedDir) == key {
			continue
		}
		kept = append(kept, record)
	}
	state.Unresolved = kept
	return state
}

func identityHasUnresolvedAssociation(state AssociationState, definitionID, resolvedDir string) bool {
	key := AssociationIdentityKey(definitionID, resolvedDir)
	for _, record := range state.Unresolved {
		if AssociationIdentityKey(record.DefinitionID, record.ResolvedDir) == key {
			return true
		}
	}
	return false
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

func RecoveryHintLines(definitionID string) []string {
	return []string{
		"recovery needed",
		"hseh recover " + definitionID + " --workspace <live-workspace-id>",
		"hseh recover " + definitionID + " --create",
	}
}

func recoveryNeededError(definitionID string) error {
	return fmt.Errorf("hseh open: definition %s needs recovery after Herdr restart; use `hseh recover %s --workspace <live-workspace-id>` or `hseh recover %s --create`", definitionID, definitionID, definitionID)
}

func upsertAssociation(state AssociationState, record AssociationRecord) AssociationState {
	replaced := false
	for i, existing := range state.Records {
		if existing.DefinitionID == record.DefinitionID && existing.ResolvedDir == record.ResolvedDir {
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

func workspaceAssociatedToOtherDefinition(state AssociationState, workspaceID, definitionID, resolvedDir string) bool {
	for _, record := range state.Records {
		if record.WorkspaceID != workspaceID {
			continue
		}
		if record.DefinitionID == definitionID && record.ResolvedDir == resolvedDir {
			continue
		}
		return true
	}
	return false
}

func exactLiveAssociation(state AssociationState, snapshot herdr.SessionSnapshot, definitionID, resolvedDir string) (string, bool) {
	live := liveWorkspaceIDs(snapshot)
	for _, record := range state.Records {
		if record.DefinitionID == definitionID && record.ResolvedDir == resolvedDir && live[record.WorkspaceID] {
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
