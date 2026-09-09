package space

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/regutierrez/hseh/internal/config"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/lockfile"
)

const (
	ActionFocus     = "focus"
	ActionAdopt     = "adopt"
	ActionCreate    = "create"
	ActionReconnect = "reconnect"
)

// OpenResult is the caller-visible outcome of create-or-focus.
type OpenResult struct {
	Action            string
	WorkspaceID       string
	SubmittedCommands int
	DefinitionID      string
	ResolvedDir       string
}

func withOpenLock(ctx context.Context, fn func() error) error {
	return lockfile.WithExclusiveContext(ctx, openLockPath(config.StateDir()), fn)
}

func workspaceGitCheckoutDir(workspace herdr.WorkspaceRow) string {
	if workspace.Worktree == nil {
		return ""
	}
	path := strings.TrimSpace(workspace.Worktree.CheckoutPath)
	if path == "" {
		return ""
	}
	canonical, err := canonicalizeDirPath(path)
	if err != nil {
		return ""
	}
	return canonical
}

func workspaceUniformPaneDir(snapshot herdr.SessionSnapshot, workspaceID string) (string, bool) {
	var dir string
	found := false
	for _, pane := range snapshot.Panes {
		if pane.WorkspaceID != workspaceID {
			continue
		}
		raw := strings.TrimSpace(pane.Cwd)
		if raw == "" {
			return "", false
		}
		canonical, err := canonicalizeDirPath(raw)
		if err != nil {
			return "", false
		}
		if !found {
			dir = canonical
			found = true
			continue
		}
		if canonical != dir {
			return "", false
		}
	}
	if !found {
		return "", false
	}
	return dir, true
}

func workspaceDirectorySignal(snapshot herdr.SessionSnapshot, workspace herdr.WorkspaceRow) (string, bool) {
	if checkout := workspaceGitCheckoutDir(workspace); checkout != "" {
		return checkout, true
	}
	return workspaceUniformPaneDir(snapshot, workspace.WorkspaceID)
}

func eligibleAdoptionWorkspaceIDs(snapshot herdr.SessionSnapshot, history focus.History, state AssociationState, definitionID, resolvedDir string) []string {
	var eligible []string
	for _, workspace := range snapshot.Workspaces {
		if workspaceAssociatedToOtherDefinition(state, workspace.WorkspaceID, definitionID, resolvedDir) {
			continue
		}
		dir, ok := workspaceDirectorySignal(snapshot, workspace)
		if !ok || dir != resolvedDir {
			continue
		}
		eligible = append(eligible, workspace.WorkspaceID)
	}
	indexOf := map[string]int{}
	for i, id := range history.Spaces {
		indexOf[id] = i
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		left, leftOK := indexOf[eligible[i]]
		right, rightOK := indexOf[eligible[j]]
		if leftOK && rightOK {
			return left < right
		}
		if leftOK {
			return true
		}
		if rightOK {
			return false
		}
		return i < j
	})
	return eligible
}

func persistCreatedAssociation(stateDir string, state AssociationState, record AssociationRecord) error {
	state = upsertAssociation(state, record)
	state = removeUnresolvedAssociation(state, record.DefinitionID, record.ResolvedDir)
	return WriteAssociationFile(stateDir, state)
}

func createSpace(ctx context.Context, stateDir string, state AssociationState, def Definition) (OpenResult, error) {
	if err := preflightDirs(def); err != nil {
		return OpenResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return OpenResult{}, err
	}
	workspaceID, rootTabID, rootPaneID, err := herdr.CreateWorkspace(ctx, def.ResolvedDir, def.Name, true)
	if err != nil {
		return OpenResult{}, fmt.Errorf("hseh create %s: workspace.create: %w", def.ID, err)
	}
	record := AssociationRecord{DefinitionID: def.ID, ResolvedDir: def.ResolvedDir, WorkspaceID: workspaceID}
	if err := persistCreatedAssociation(stateDir, state, record); err != nil {
		return OpenResult{Action: ActionCreate, WorkspaceID: workspaceID, DefinitionID: def.ID, ResolvedDir: def.ResolvedDir}, fmt.Errorf("hseh create %s: persist association after workspace.create %s: %w", def.ID, workspaceID, err)
	}
	submitted, err := applyLayout(ctx, def, workspaceID, rootTabID, rootPaneID)
	result := OpenResult{Action: ActionCreate, WorkspaceID: workspaceID, SubmittedCommands: submitted, DefinitionID: def.ID, ResolvedDir: def.ResolvedDir}
	if err != nil {
		return result, err
	}
	return result, nil
}

func openLocked(ctx context.Context, definitionID string) (OpenResult, error) {
	if err := ctx.Err(); err != nil {
		return OpenResult{}, err
	}
	defs, errs := LoadDefinitions(config.SpaceDefinitionsDir())
	def, ok := FindDefinitionByID(defs, definitionID)
	if !ok {
		if len(errs) > 0 {
			return OpenResult{}, fmt.Errorf("hseh open %s: not found (%s)", definitionID, strings.Join(errs, "; "))
		}
		return OpenResult{}, fmt.Errorf("hseh open: definition %s not found", definitionID)
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
	history, err := focus.LoadPruned(snapshot, witness)
	if err != nil {
		return OpenResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return OpenResult{}, err
	}
	if workspaceID, ok := exactLiveAssociation(state, snapshot, def.ID, def.ResolvedDir); ok {
		if err := herdr.FocusWorkspaceContext(ctx, workspaceID); err != nil {
			return OpenResult{}, err
		}
		return OpenResult{Action: ActionFocus, WorkspaceID: workspaceID, DefinitionID: def.ID, ResolvedDir: def.ResolvedDir}, nil
	}
	if identityHasUnresolvedAssociation(state, def.ID, def.ResolvedDir) {
		return OpenResult{}, recoveryNeededError(def.ID)
	}
	eligible := eligibleAdoptionWorkspaceIDs(snapshot, history, state, def.ID, def.ResolvedDir)
	if len(eligible) > 0 {
		workspaceID := eligible[0]
		state = upsertAssociation(state, AssociationRecord{
			DefinitionID: def.ID,
			ResolvedDir:  def.ResolvedDir,
			WorkspaceID:  workspaceID,
		})
		if err := WriteAssociationFile(stateDir, state); err != nil {
			return OpenResult{}, fmt.Errorf("hseh open %s: persist adoption: %w", def.ID, err)
		}
		if err := herdr.FocusWorkspaceContext(ctx, workspaceID); err != nil {
			return OpenResult{}, err
		}
		return OpenResult{Action: ActionAdopt, WorkspaceID: workspaceID, DefinitionID: def.ID, ResolvedDir: def.ResolvedDir}, nil
	}
	return createSpace(ctx, stateDir, state, def)
}

// Open focuses, adopts, or creates a definition in the current Herdr server lifetime.
func Open(ctx context.Context, definitionID string) (OpenResult, error) {
	definitionID = strings.TrimSpace(definitionID)
	if definitionID == "" {
		return OpenResult{}, fmt.Errorf("hseh open: definition id is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var result OpenResult
	err := withOpenLock(ctx, func() error {
		var innerErr error
		result, innerErr = openLocked(ctx, definitionID)
		return innerErr
	})
	return result, err
}
