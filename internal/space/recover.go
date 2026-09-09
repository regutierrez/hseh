package space

import (
	"context"
	"fmt"
	"strings"

	"github.com/regutierrez/hseh/internal/config"
	"github.com/regutierrez/hseh/internal/herdr"
)

// Recover reconnects or creates an identity left unresolved after a Herdr restart.
func Recover(ctx context.Context, definitionID, workspaceID string, create bool) (OpenResult, error) {
	definitionID = strings.TrimSpace(definitionID)
	workspaceID = strings.TrimSpace(workspaceID)
	if definitionID == "" {
		return OpenResult{}, fmt.Errorf("hseh recover: definition id is required")
	}
	if create == (workspaceID != "") {
		return OpenResult{}, fmt.Errorf("hseh recover: exactly one of --workspace <live-workspace-id> or --create is required")
	}
	if ctx == nil {
		ctx = context.Background()
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
	defs, errs := LoadDefinitions(config.SpaceDefinitionsDir())
	def, ok := FindDefinitionByID(defs, definitionID)
	if !ok {
		if len(errs) > 0 {
			return OpenResult{}, fmt.Errorf("hseh recover %s: not found (%s)", definitionID, strings.Join(errs, "; "))
		}
		return OpenResult{}, fmt.Errorf("hseh recover: definition %s not found", definitionID)
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
	if liveID, exact := exactLiveAssociation(state, snapshot, def.ID, def.ResolvedDir); exact {
		if !create && workspaceID != liveID {
			return OpenResult{}, fmt.Errorf("hseh recover: definition %s is already associated with %s", def.ID, liveID)
		}
		if err := herdr.FocusWorkspaceContext(ctx, liveID); err != nil {
			return OpenResult{}, err
		}
		return OpenResult{Action: ActionFocus, WorkspaceID: liveID, DefinitionID: def.ID, ResolvedDir: def.ResolvedDir}, nil
	}
	if !identityHasUnresolvedAssociation(state, def.ID, def.ResolvedDir) {
		return OpenResult{}, fmt.Errorf("hseh recover: definition %s does not need recovery", def.ID)
	}
	if create {
		return createSpace(ctx, stateDir, state, def)
	}
	if !liveWorkspaceIDs(snapshot)[workspaceID] {
		return OpenResult{}, fmt.Errorf("hseh recover: workspace %s is not live", workspaceID)
	}
	if workspaceAssociatedToOtherDefinition(state, workspaceID, def.ID, def.ResolvedDir) {
		return OpenResult{}, fmt.Errorf("hseh recover: workspace %s is associated with another definition", workspaceID)
	}
	if err := ctx.Err(); err != nil {
		return OpenResult{}, err
	}
	record := AssociationRecord{DefinitionID: def.ID, ResolvedDir: def.ResolvedDir, WorkspaceID: workspaceID}
	if err := persistCreatedAssociation(stateDir, state, record); err != nil {
		return OpenResult{}, fmt.Errorf("hseh recover %s: persist reconnection: %w", def.ID, err)
	}
	if err := herdr.FocusWorkspaceContext(ctx, workspaceID); err != nil {
		return OpenResult{}, err
	}
	return OpenResult{Action: ActionReconnect, WorkspaceID: workspaceID, DefinitionID: def.ID, ResolvedDir: def.ResolvedDir}, nil
}
