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
	actionFocus     = "focus"
	actionAdopt     = "adopt"
	actionCreate    = "create"
	actionReconnect = "reconnect"
)

// OpenResult is the caller-visible outcome of create-or-focus.
type OpenResult struct {
	Action       string
	WorkspaceID  string
	DefinitionID string
}

// loadDefinition finds one definition by id; cmd names the command for error messages.
func loadDefinition(cmd, definitionID string) (Definition, error) {
	defs, errs := LoadDefinitions(config.SpaceDefinitionsDir())
	def, ok := findDefinitionByID(defs, definitionID)
	if ok {
		return def, nil
	}
	if len(errs) > 0 {
		return Definition{}, fmt.Errorf("hseh %s %s: not found (%s)", cmd, definitionID, strings.Join(errs, "; "))
	}
	return Definition{}, fmt.Errorf("hseh %s: definition %s not found", cmd, definitionID)
}

func withOpenLock(ctx context.Context, fn func() (OpenResult, error)) (OpenResult, error) {
	var result OpenResult
	err := lockfile.WithExclusiveContext(ctx, openLockPath(config.StateDir()), func() error {
		var innerErr error
		result, innerErr = fn()
		return innerErr
	})
	return result, err
}

func workspaceDirectorySignal(snapshot herdr.SessionSnapshot, workspace herdr.WorkspaceRow) (string, bool) {
	if workspace.Worktree != nil {
		path := strings.TrimSpace(workspace.Worktree.CheckoutPath)
		if path != "" {
			if canonical, err := canonicalizeDirPath(path); err == nil {
				return canonical, true
			}
		}
	}
	var dir string
	found := false
	for _, pane := range snapshot.Panes {
		if pane.WorkspaceID != workspace.WorkspaceID {
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

func eligibleAdoptionWorkspaceIDs(snapshot herdr.SessionSnapshot, history focus.History, state AssociationState, identity AssociationRecord) []string {
	var eligible []string
	for _, workspace := range snapshot.Workspaces {
		if workspaceAssociatedToOtherDefinition(state, workspace.WorkspaceID, identity) {
			continue
		}
		dir, ok := workspaceDirectorySignal(snapshot, workspace)
		if !ok || dir != identity.ResolvedDir {
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
	state = removeUnresolvedAssociation(state, record)
	return WriteAssociationFile(stateDir, state)
}

// createSpace checks every directory the layout needs before touching Herdr, so an
// authoring mistake cannot leave a half-built workspace behind.
func createSpace(ctx context.Context, stateDir string, state AssociationState, def Definition) (OpenResult, error) {
	if _, err := requireExistingDir(def.ResolvedDir); err != nil {
		return OpenResult{}, fmt.Errorf("hseh create %s: %w", def.ID, err)
	}
	dirs, err := resolvePaneDirs(def.ResolvedDir, def.Tabs)
	if err != nil {
		return OpenResult{}, fmt.Errorf("hseh create %s: %w", def.ID, err)
	}
	if err := ctx.Err(); err != nil {
		return OpenResult{}, err
	}
	workspaceID, rootTabID, rootPaneID, err := herdr.CreateWorkspace(ctx, def.ResolvedDir, def.Name, true)
	if err != nil {
		return OpenResult{}, fmt.Errorf("hseh create %s: workspace.create: %w", def.ID, err)
	}
	record := def.identity()
	record.WorkspaceID = workspaceID
	if err := persistCreatedAssociation(stateDir, state, record); err != nil {
		return OpenResult{Action: actionCreate, WorkspaceID: workspaceID, DefinitionID: def.ID}, fmt.Errorf("hseh create %s: persist association after workspace.create %s: %w", def.ID, workspaceID, err)
	}
	err = applyLayout(ctx, def, dirs, workspaceID, rootTabID, rootPaneID)
	return OpenResult{Action: actionCreate, WorkspaceID: workspaceID, DefinitionID: def.ID}, err
}

func openLocked(ctx context.Context, definitionID string) (OpenResult, error) {
	def, err := loadDefinition("open", definitionID)
	if err != nil {
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
	if workspaceID, ok := exactLiveAssociation(state, snapshot, def.identity()); ok {
		if err := herdr.FocusWorkspaceContext(ctx, workspaceID); err != nil {
			return OpenResult{}, err
		}
		return OpenResult{Action: actionFocus, WorkspaceID: workspaceID, DefinitionID: def.ID}, nil
	}
	if hasUnresolvedAssociation(state, def.identity()) {
		return OpenResult{}, recoveryNeededError(def.ID)
	}
	eligible := eligibleAdoptionWorkspaceIDs(snapshot, history, state, def.identity())
	if len(eligible) > 0 {
		record := def.identity()
		record.WorkspaceID = eligible[0]
		state = upsertAssociation(state, record)
		if err := WriteAssociationFile(stateDir, state); err != nil {
			return OpenResult{}, fmt.Errorf("hseh open %s: persist adoption: %w", def.ID, err)
		}
		if err := herdr.FocusWorkspaceContext(ctx, record.WorkspaceID); err != nil {
			return OpenResult{}, err
		}
		return OpenResult{Action: actionAdopt, WorkspaceID: record.WorkspaceID, DefinitionID: def.ID}, nil
	}
	return createSpace(ctx, stateDir, state, def)
}

// Open focuses, adopts, or creates a definition in the current Herdr server lifetime.
func Open(ctx context.Context, definitionID string) (OpenResult, error) {
	definitionID = strings.TrimSpace(definitionID)
	if definitionID == "" {
		return OpenResult{}, fmt.Errorf("hseh open: definition id is required")
	}
	return withOpenLock(ctx, func() (OpenResult, error) { return openLocked(ctx, definitionID) })
}
