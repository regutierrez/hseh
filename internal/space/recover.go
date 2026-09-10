package space

import (
	"context"
	"fmt"
	"strings"

	"github.com/regutierrez/hseh/internal/config"
	"github.com/regutierrez/hseh/internal/herdr"
)

// Recover reconnects or creates an identity left unresolved after a Herdr restart.
// Exactly one of workspaceID or create must be given.
func Recover(ctx context.Context, definitionID, workspaceID string, create bool) (OpenResult, error) {
	definitionID = strings.TrimSpace(definitionID)
	workspaceID = strings.TrimSpace(workspaceID)
	if definitionID == "" {
		return OpenResult{}, fmt.Errorf("hseh recover: definition id is required")
	}
	if create == (workspaceID != "") {
		return OpenResult{}, fmt.Errorf("hseh recover: %s", RecoverUsage)
	}
	var result OpenResult
	err := withOpenLock(ctx, func() error {
		var innerErr error
		result, innerErr = recoverLocked(ctx, definitionID, workspaceID, create)
		return innerErr
	})
	return result, err
}

func recoverLocked(ctx context.Context, definitionID, workspaceID string, create bool) (OpenResult, error) {
	if err := ctx.Err(); err != nil {
		return OpenResult{}, err
	}
	def, err := loadDefinition("recover", definitionID)
	if err != nil {
		return OpenResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return OpenResult{}, err
	}
	snapshot, witness, err := herdr.LoadSessionSnapshotContext(ctx)
	if err != nil {
		return OpenResult{}, err
	}
	stateDir := config.StateDir()
	state, err := LoadReconciledAssociationState(stateDir, witness)
	if err != nil {
		return OpenResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return OpenResult{}, err
	}
	if liveID, exact := exactLiveAssociation(state, snapshot, def.identity()); exact {
		if !create && workspaceID != liveID {
			return OpenResult{}, fmt.Errorf("hseh recover: definition %s is already associated with %s", def.ID, liveID)
		}
		if err := herdr.FocusWorkspaceContext(ctx, liveID); err != nil {
			return OpenResult{}, err
		}
		return OpenResult{Action: ActionFocus, WorkspaceID: liveID, DefinitionID: def.ID}, nil
	}
	if !hasUnresolvedAssociation(state, def.identity()) {
		return OpenResult{}, fmt.Errorf("hseh recover: definition %s does not need recovery", def.ID)
	}
	if create {
		return createSpace(ctx, stateDir, state, def)
	}
	if !liveWorkspaceIDs(snapshot)[workspaceID] {
		return OpenResult{}, fmt.Errorf("hseh recover: workspace %s is not live", workspaceID)
	}
	if workspaceAssociatedToOtherDefinition(state, workspaceID, def.identity()) {
		return OpenResult{}, fmt.Errorf("hseh recover: workspace %s is associated with another definition", workspaceID)
	}
	if err := ctx.Err(); err != nil {
		return OpenResult{}, err
	}
	record := def.identity()
	record.WorkspaceID = workspaceID
	if err := persistCreatedAssociation(stateDir, state, record); err != nil {
		return OpenResult{}, fmt.Errorf("hseh recover %s: persist reconnection: %w", def.ID, err)
	}
	if err := herdr.FocusWorkspaceContext(ctx, workspaceID); err != nil {
		return OpenResult{}, err
	}
	return OpenResult{Action: ActionReconnect, WorkspaceID: workspaceID, DefinitionID: def.ID}, nil
}
