package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SpaceAssociationRecord is one definition-id + resolved-dir to live workspace link.
type SpaceAssociationRecord struct {
	DefinitionID string `json:"definition_id"`
	ResolvedDir  string `json:"resolved_dir"`
	WorkspaceID  string `json:"workspace_id"`
}

// SpaceAssociationState is session-scoped association provenance for one proven server.
// Unresolved holds identities from earlier server generations that need explicit recovery.
type SpaceAssociationState struct {
	Witness    ServerContinuityWitness  `json:"witness"`
	Records    []SpaceAssociationRecord `json:"records"`
	Unresolved []SpaceAssociationRecord `json:"unresolved"`
}

func spaceAssociationStatePath(stateDir string) string {
	return filepath.Join(sessionHistoryDir(stateDir), "associations.json")
}

func spaceOpenLockPath(stateDir string) string {
	return filepath.Join(sessionHistoryDir(stateDir), "space-open.lock")
}

func associationIdentityKey(definitionID, resolvedDir string) string {
	return definitionID + "\n" + resolvedDir
}

func loadSpaceAssociationFile(stateDir string) (SpaceAssociationState, error) {
	payload, err := os.ReadFile(spaceAssociationStatePath(stateDir))
	if err != nil {
		if os.IsNotExist(err) {
			return SpaceAssociationState{}, nil
		}
		return SpaceAssociationState{}, fmt.Errorf("hseh association: read: %w", err)
	}
	var state SpaceAssociationState
	if err := json.Unmarshal(payload, &state); err != nil {
		return SpaceAssociationState{}, fmt.Errorf("hseh association: corrupt state: %w", err)
	}
	if state.Records == nil {
		state.Records = []SpaceAssociationRecord{}
	}
	if state.Unresolved == nil {
		state.Unresolved = []SpaceAssociationRecord{}
	}
	return state, nil
}

func writeSpaceAssociationFile(stateDir string, state SpaceAssociationState) error {
	dir := sessionHistoryDir(stateDir)
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
	if err := os.Rename(temporaryPath, spaceAssociationStatePath(stateDir)); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("hseh association: rename: %w", err)
	}
	return nil
}

func spaceAssociationWitnessEmpty(witness ServerContinuityWitness) bool {
	return witness.PeerPID == 0 && witness.PeerStartTime == ""
}

func spaceAssociationWitnessMismatch(state SpaceAssociationState, live ServerContinuityWitness) bool {
	if spaceAssociationWitnessEmpty(state.Witness) && len(state.Records) == 0 {
		return false
	}
	return !sameServerContinuityWitness(state.Witness, live)
}

func appendUnresolvedSpaceAssociation(state SpaceAssociationState, record SpaceAssociationRecord) SpaceAssociationState {
	key := associationIdentityKey(record.DefinitionID, record.ResolvedDir)
	for _, existing := range state.Unresolved {
		if associationIdentityKey(existing.DefinitionID, existing.ResolvedDir) == key {
			return state
		}
	}
	state.Unresolved = append(state.Unresolved, record)
	return state
}

func removeUnresolvedSpaceAssociation(state SpaceAssociationState, definitionID, resolvedDir string) SpaceAssociationState {
	key := associationIdentityKey(definitionID, resolvedDir)
	kept := []SpaceAssociationRecord{}
	for _, record := range state.Unresolved {
		if associationIdentityKey(record.DefinitionID, record.ResolvedDir) == key {
			continue
		}
		kept = append(kept, record)
	}
	state.Unresolved = kept
	return state
}

func identityHasUnresolvedSpaceAssociation(state SpaceAssociationState, definitionID, resolvedDir string) bool {
	key := associationIdentityKey(definitionID, resolvedDir)
	for _, record := range state.Unresolved {
		if associationIdentityKey(record.DefinitionID, record.ResolvedDir) == key {
			return true
		}
	}
	return false
}

// reconcileSpaceAssociationState folds mismatched current records into unresolved in memory.
// It does not write. Picker reads must not persist a witness reset.
func reconcileSpaceAssociationState(state SpaceAssociationState, live ServerContinuityWitness) SpaceAssociationState {
	if state.Records == nil {
		state.Records = []SpaceAssociationRecord{}
	}
	if state.Unresolved == nil {
		state.Unresolved = []SpaceAssociationRecord{}
	}
	if !spaceAssociationWitnessMismatch(state, live) {
		state.Witness = live
		return state
	}
	for _, record := range state.Records {
		state = appendUnresolvedSpaceAssociation(state, record)
	}
	state.Records = []SpaceAssociationRecord{}
	state.Witness = live
	return state
}

func loadReconciledSpaceAssociationState(stateDir string, live ServerContinuityWitness) (SpaceAssociationState, error) {
	state, err := loadSpaceAssociationFile(stateDir)
	if err != nil {
		return SpaceAssociationState{}, err
	}
	return reconcileSpaceAssociationState(state, live), nil
}

func spaceAssociationRecoveryHintLines(definitionID string) []string {
	return []string{
		"recovery needed",
		"hseh recover " + definitionID + " --workspace <live-workspace-id>",
		"hseh recover " + definitionID + " --create",
	}
}

func spaceAssociationRecoveryNeededError(definitionID string) error {
	return fmt.Errorf("hseh open: definition %s needs recovery after Herdr restart; use `hseh recover %s --workspace <live-workspace-id>` or `hseh recover %s --create`", definitionID, definitionID, definitionID)
}

func upsertSpaceAssociation(state SpaceAssociationState, record SpaceAssociationRecord) SpaceAssociationState {
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

func liveWorkspaceIDs(snapshot HerdrSessionSnapshot) map[string]bool {
	live := map[string]bool{}
	for _, workspace := range snapshot.Workspaces {
		if workspace.WorkspaceID != "" {
			live[workspace.WorkspaceID] = true
		}
	}
	return live
}

func workspaceAssociatedToOtherDefinition(state SpaceAssociationState, workspaceID, definitionID, resolvedDir string) bool {
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

func exactLiveAssociation(state SpaceAssociationState, snapshot HerdrSessionSnapshot, definitionID, resolvedDir string) (string, bool) {
	live := liveWorkspaceIDs(snapshot)
	for _, record := range state.Records {
		if record.DefinitionID == definitionID && record.ResolvedDir == resolvedDir && live[record.WorkspaceID] {
			return record.WorkspaceID, true
		}
	}
	return "", false
}

func associatedLiveDefinitionKeys(state SpaceAssociationState, snapshot HerdrSessionSnapshot) map[string]bool {
	live := liveWorkspaceIDs(snapshot)
	keys := map[string]bool{}
	for _, record := range state.Records {
		if live[record.WorkspaceID] {
			keys[associationIdentityKey(record.DefinitionID, record.ResolvedDir)] = true
		}
	}
	return keys
}
