package main

import (
	"context"
	"fmt"
	"strings"
)

func parseRecoverArgs(args []string) (definitionID, workspaceID string, create bool, err error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" || strings.HasPrefix(args[0], "-") {
		return "", "", false, fmt.Errorf("hseh recover: definition id is required")
	}
	definitionID = strings.TrimSpace(args[0])
	seenCreate := false
	seenWorkspace := false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--create":
			if seenCreate {
				return "", "", false, fmt.Errorf("hseh recover: unknown argument %s", arg)
			}
			seenCreate = true
			create = true
		case arg == "--workspace":
			if seenWorkspace {
				return "", "", false, fmt.Errorf("hseh recover: unknown argument %s", arg)
			}
			seenWorkspace = true
			if i+1 >= len(args) {
				return "", "", false, fmt.Errorf("hseh recover: --workspace requires a live workspace id")
			}
			i++
			workspaceID = strings.TrimSpace(args[i])
		case strings.HasPrefix(arg, "--workspace="):
			if seenWorkspace {
				return "", "", false, fmt.Errorf("hseh recover: unknown argument %s", arg)
			}
			seenWorkspace = true
			workspaceID = strings.TrimSpace(arg[len("--workspace="):])
		default:
			return "", "", false, fmt.Errorf("hseh recover: unknown argument %s", arg)
		}
	}
	if seenWorkspace && workspaceID == "" {
		return "", "", false, fmt.Errorf("hseh recover: --workspace requires a live workspace id")
	}
	if seenCreate == seenWorkspace {
		return "", "", false, fmt.Errorf("hseh recover: exactly one of --workspace <live-workspace-id> or --create is required")
	}
	return definitionID, workspaceID, create, nil
}

// RecoverReusableSpace reconnects or creates an identity left unresolved after a Herdr restart.
func RecoverReusableSpace(ctx context.Context, definitionID, workspaceID string, create bool) (SpaceOpenResult, error) {
	definitionID = strings.TrimSpace(definitionID)
	workspaceID = strings.TrimSpace(workspaceID)
	if definitionID == "" {
		return SpaceOpenResult{}, fmt.Errorf("hseh recover: definition id is required")
	}
	if create == (workspaceID != "") {
		return SpaceOpenResult{}, fmt.Errorf("hseh recover: exactly one of --workspace <live-workspace-id> or --create is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var result SpaceOpenResult
	err := withSpaceOpenLock(ctx, func() error {
		var innerErr error
		result, innerErr = recoverReusableSpaceLocked(ctx, definitionID, workspaceID, create)
		return innerErr
	})
	return result, err
}

func recoverReusableSpaceLocked(ctx context.Context, definitionID, workspaceID string, create bool) (SpaceOpenResult, error) {
	if err := ctx.Err(); err != nil {
		return SpaceOpenResult{}, err
	}
	defs, errs := LoadSpaceDefinitions(spaceDefinitionsDir())
	def, ok := findSpaceDefinitionByID(defs, definitionID)
	if !ok {
		if len(errs) > 0 {
			return SpaceOpenResult{}, fmt.Errorf("hseh recover %s: not found (%s)", definitionID, strings.Join(errs, "; "))
		}
		return SpaceOpenResult{}, fmt.Errorf("hseh recover: definition %s not found", definitionID)
	}
	if err := ctx.Err(); err != nil {
		return SpaceOpenResult{}, err
	}
	snapshot, witness, err := LoadHerdrSessionSnapshotContext(ctx)
	if err != nil {
		return SpaceOpenResult{}, err
	}
	stateDir := pluginStateDir()
	state, err := loadReconciledSpaceAssociationState(stateDir, witness)
	if err != nil {
		return SpaceOpenResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return SpaceOpenResult{}, err
	}
	if liveID, exact := exactLiveAssociation(state, snapshot, def.ID, def.ResolvedDir); exact {
		if !create && workspaceID != liveID {
			return SpaceOpenResult{}, fmt.Errorf("hseh recover: definition %s is already associated with %s", def.ID, liveID)
		}
		if err := FocusHerdrWorkspaceContext(ctx, liveID); err != nil {
			return SpaceOpenResult{}, err
		}
		return SpaceOpenResult{Action: spaceOpenActionFocus, WorkspaceID: liveID, DefinitionID: def.ID, ResolvedDir: def.ResolvedDir}, nil
	}
	if !identityHasUnresolvedSpaceAssociation(state, def.ID, def.ResolvedDir) {
		return SpaceOpenResult{}, fmt.Errorf("hseh recover: definition %s does not need recovery", def.ID)
	}
	if create {
		return createReusableSpace(ctx, stateDir, state, def)
	}
	if !liveWorkspaceIDs(snapshot)[workspaceID] {
		return SpaceOpenResult{}, fmt.Errorf("hseh recover: workspace %s is not live", workspaceID)
	}
	if workspaceAssociatedToOtherDefinition(state, workspaceID, def.ID, def.ResolvedDir) {
		return SpaceOpenResult{}, fmt.Errorf("hseh recover: workspace %s is associated with another definition", workspaceID)
	}
	if err := ctx.Err(); err != nil {
		return SpaceOpenResult{}, err
	}
	record := SpaceAssociationRecord{DefinitionID: def.ID, ResolvedDir: def.ResolvedDir, WorkspaceID: workspaceID}
	if err := persistCreatedAssociation(stateDir, state, record); err != nil {
		return SpaceOpenResult{}, fmt.Errorf("hseh recover %s: persist reconnection: %w", def.ID, err)
	}
	if err := FocusHerdrWorkspaceContext(ctx, workspaceID); err != nil {
		return SpaceOpenResult{}, err
	}
	return SpaceOpenResult{Action: spaceOpenActionReconnect, WorkspaceID: workspaceID, DefinitionID: def.ID, ResolvedDir: def.ResolvedDir}, nil
}

func runRecoverReusableSpace(args []string) error {
	definitionID, workspaceID, create, err := parseRecoverArgs(args)
	if err != nil {
		return err
	}
	result, err := RecoverReusableSpace(context.Background(), definitionID, workspaceID, create)
	if err != nil {
		return err
	}
	fmt.Printf("recovered %s %s %s\n", result.Action, result.DefinitionID, result.WorkspaceID)
	return nil
}
