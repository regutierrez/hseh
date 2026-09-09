package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const (
	spaceOpenActionFocus     = "focus"
	spaceOpenActionAdopt     = "adopt"
	spaceOpenActionCreate    = "create"
	spaceOpenActionReconnect = "reconnect"
)

// SpaceOpenResult is the caller-visible outcome of create-or-focus.
type SpaceOpenResult struct {
	Action            string
	WorkspaceID       string
	SubmittedCommands int
	DefinitionID      string
	ResolvedDir       string
}

func withSpaceOpenLock(ctx context.Context, fn func() error) error {
	return withExclusiveFileLockContext(ctx, spaceOpenLockPath(pluginStateDir()), fn)
}

func workspaceGitCheckoutDir(workspace HerdrWorkspaceRow) string {
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

func workspaceUniformPaneDir(snapshot HerdrSessionSnapshot, workspaceID string) (string, bool) {
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

func workspaceDirectorySignal(snapshot HerdrSessionSnapshot, workspace HerdrWorkspaceRow) (string, bool) {
	if checkout := workspaceGitCheckoutDir(workspace); checkout != "" {
		return checkout, true
	}
	return workspaceUniformPaneDir(snapshot, workspace.WorkspaceID)
}

func eligibleAdoptionWorkspaceIDs(snapshot HerdrSessionSnapshot, history FocusHistory, state SpaceAssociationState, definitionID, resolvedDir string) []string {
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

func persistCreatedAssociation(stateDir string, state SpaceAssociationState, record SpaceAssociationRecord) error {
	state = upsertSpaceAssociation(state, record)
	state = removeUnresolvedSpaceAssociation(state, record.DefinitionID, record.ResolvedDir)
	return writeSpaceAssociationFile(stateDir, state)
}

func createReusableSpace(ctx context.Context, stateDir string, state SpaceAssociationState, def SpaceDefinition) (SpaceOpenResult, error) {
	if err := preflightSpaceDefinitionDirs(def); err != nil {
		return SpaceOpenResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return SpaceOpenResult{}, err
	}
	workspaceID, rootTabID, rootPaneID, err := createHerdrWorkspace(ctx, def.ResolvedDir, def.Name, true)
	if err != nil {
		return SpaceOpenResult{}, fmt.Errorf("hseh create %s: workspace.create: %w", def.ID, err)
	}
	record := SpaceAssociationRecord{DefinitionID: def.ID, ResolvedDir: def.ResolvedDir, WorkspaceID: workspaceID}
	if err := persistCreatedAssociation(stateDir, state, record); err != nil {
		return SpaceOpenResult{Action: spaceOpenActionCreate, WorkspaceID: workspaceID, DefinitionID: def.ID, ResolvedDir: def.ResolvedDir}, fmt.Errorf("hseh create %s: persist association after workspace.create %s: %w", def.ID, workspaceID, err)
	}
	submitted, err := layoutReusableSpace(ctx, def, workspaceID, rootTabID, rootPaneID)
	result := SpaceOpenResult{Action: spaceOpenActionCreate, WorkspaceID: workspaceID, SubmittedCommands: submitted, DefinitionID: def.ID, ResolvedDir: def.ResolvedDir}
	if err != nil {
		return result, err
	}
	return result, nil
}

func openReusableSpaceLocked(ctx context.Context, definitionID string) (SpaceOpenResult, error) {
	if err := ctx.Err(); err != nil {
		return SpaceOpenResult{}, err
	}
	defs, errs := LoadSpaceDefinitions(spaceDefinitionsDir())
	def, ok := findSpaceDefinitionByID(defs, definitionID)
	if !ok {
		if len(errs) > 0 {
			return SpaceOpenResult{}, fmt.Errorf("hseh open %s: not found (%s)", definitionID, strings.Join(errs, "; "))
		}
		return SpaceOpenResult{}, fmt.Errorf("hseh open: definition %s not found", definitionID)
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
	history, err := LoadPrunedFocusHistory(snapshot, witness)
	if err != nil {
		return SpaceOpenResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return SpaceOpenResult{}, err
	}
	if workspaceID, ok := exactLiveAssociation(state, snapshot, def.ID, def.ResolvedDir); ok {
		if err := FocusHerdrWorkspaceContext(ctx, workspaceID); err != nil {
			return SpaceOpenResult{}, err
		}
		return SpaceOpenResult{Action: spaceOpenActionFocus, WorkspaceID: workspaceID, DefinitionID: def.ID, ResolvedDir: def.ResolvedDir}, nil
	}
	if identityHasUnresolvedSpaceAssociation(state, def.ID, def.ResolvedDir) {
		return SpaceOpenResult{}, spaceAssociationRecoveryNeededError(def.ID)
	}
	eligible := eligibleAdoptionWorkspaceIDs(snapshot, history, state, def.ID, def.ResolvedDir)
	if len(eligible) > 0 {
		workspaceID := eligible[0]
		state = upsertSpaceAssociation(state, SpaceAssociationRecord{
			DefinitionID: def.ID,
			ResolvedDir:  def.ResolvedDir,
			WorkspaceID:  workspaceID,
		})
		if err := writeSpaceAssociationFile(stateDir, state); err != nil {
			return SpaceOpenResult{}, fmt.Errorf("hseh open %s: persist adoption: %w", def.ID, err)
		}
		if err := FocusHerdrWorkspaceContext(ctx, workspaceID); err != nil {
			return SpaceOpenResult{}, err
		}
		return SpaceOpenResult{Action: spaceOpenActionAdopt, WorkspaceID: workspaceID, DefinitionID: def.ID, ResolvedDir: def.ResolvedDir}, nil
	}
	return createReusableSpace(ctx, stateDir, state, def)
}

// OpenReusableSpace focuses, adopts, or creates a definition in the current Herdr server lifetime.
func OpenReusableSpace(ctx context.Context, definitionID string) (SpaceOpenResult, error) {
	definitionID = strings.TrimSpace(definitionID)
	if definitionID == "" {
		return SpaceOpenResult{}, fmt.Errorf("hseh open: definition id is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var result SpaceOpenResult
	err := withSpaceOpenLock(ctx, func() error {
		var innerErr error
		result, innerErr = openReusableSpaceLocked(ctx, definitionID)
		return innerErr
	})
	return result, err
}

func runOpenReusableSpace(definitionID string) error {
	result, err := OpenReusableSpace(context.Background(), definitionID)
	if err != nil {
		return err
	}
	fmt.Printf("opened %s %s %s\n", result.Action, result.DefinitionID, result.WorkspaceID)
	return nil
}

func pickerSelectionID(kind, target string) string {
	return kind + ":" + target
}

func definitionPickerItem(def SpaceDefinition, needsRecovery bool) PickerItem {
	name := sanitizePickerDisplayText(def.Name)
	desc := sanitizePickerDisplayText(def.Description)
	dir := sanitizePickerDisplayText(def.ResolvedDir)
	if dir == "" {
		dir = sanitizePickerDisplayText(def.WorkingDir)
	}
	rows := []string{name}
	if desc != "" {
		rows = append(rows, desc)
	}
	if dir != "" {
		rows = append(rows, dir)
	}
	preview := spaceDefinitionPreviewText(def)
	if needsRecovery {
		hints := spaceAssociationRecoveryHintLines(def.ID)
		rows = append(rows, hints...)
		preview = strings.Join(hints, "\n") + "\n" + preview
	}
	search := strings.Join(append(append([]string{}, rows...), sanitizePickerDisplayText(def.SourceFile), sanitizePickerDisplayText(filepath.Base(def.SourceFile))), " ")
	display := make([]string, len(rows))
	for i, row := range rows {
		if i == 0 {
			display[i] = "\x1b[1m" + row + "\x1b[0m"
		} else {
			display[i] = pickerMuted + row + "\x1b[0m"
		}
	}
	return PickerItem{
		Kind:         pickerKindDefinition,
		ID:           pickerSelectionID(pickerKindDefinition, def.ID),
		DefinitionID: def.ID,
		Label:        name,
		Rows:         rows,
		DisplayRows:  display,
		SearchText:   search,
		PreviewText:  preview,
	}
}

func appendUnopenedDefinitionItems(items []PickerItem, view string, snapshot HerdrSessionSnapshot, definitions []SpaceDefinition, records, unresolved []SpaceAssociationRecord) []PickerItem {
	if view == pickerViewAgents {
		return items
	}
	openKeys := associatedLiveDefinitionKeys(SpaceAssociationState{Records: records}, snapshot)
	unresolvedKeys := map[string]bool{}
	for _, record := range unresolved {
		unresolvedKeys[associationIdentityKey(record.DefinitionID, record.ResolvedDir)] = true
	}
	var extra []PickerItem
	for _, def := range definitions {
		if openKeys[associationIdentityKey(def.ID, def.ResolvedDir)] {
			continue
		}
		extra = append(extra, definitionPickerItem(def, unresolvedKeys[associationIdentityKey(def.ID, def.ResolvedDir)]))
	}
	sort.SliceStable(extra, func(i, j int) bool { return extra[i].Label < extra[j].Label })
	return append(items, extra...)
}
