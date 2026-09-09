package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// HerdrAgentSession is a native conversation reference, not a live occupant id.
type HerdrAgentSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

// HerdrWorkspaceRow is one live workspace from session.snapshot.
type HerdrWorkspaceRow struct {
	WorkspaceID string            `json:"workspace_id"`
	Number      int               `json:"number"`
	Label       string            `json:"label"`
	Focused     bool              `json:"focused"`
	PaneCount   int               `json:"pane_count"`
	TabCount    int               `json:"tab_count"`
	ActiveTabID string            `json:"active_tab_id"`
	AgentStatus string            `json:"agent_status"`
	Tokens      map[string]string `json:"tokens"`
	Worktree    *HerdrWorktree    `json:"worktree"`
}

// HerdrWorktree is optional Git checkout provenance on a workspace.
type HerdrWorktree struct {
	CheckoutPath string `json:"checkout_path"`
	RepoName     string `json:"repo_name"`
	RepoRoot     string `json:"repo_root"`
}

// HerdrTabRow is one live tab from session.snapshot.
type HerdrTabRow struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Number      int    `json:"number"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
	PaneCount   int    `json:"pane_count"`
	AgentStatus string `json:"agent_status"`
}

// HerdrPaneRow is one live pane from session.snapshot.
type HerdrPaneRow struct {
	PaneID        string             `json:"pane_id"`
	TerminalID    string             `json:"terminal_id"`
	WorkspaceID   string             `json:"workspace_id"`
	TabID         string             `json:"tab_id"`
	Focused       bool               `json:"focused"`
	Cwd           string             `json:"cwd"`
	ForegroundCwd string             `json:"foreground_cwd"`
	Label         string             `json:"label"`
	Agent         string             `json:"agent"`
	Title         string             `json:"title"`
	TerminalTitle string             `json:"terminal_title"`
	TitleStripped string             `json:"terminal_title_stripped"`
	DisplayAgent  string             `json:"display_agent"`
	AgentStatus   string             `json:"agent_status"`
	StateLabels   map[string]string  `json:"state_labels"`
	Tokens        map[string]string  `json:"tokens"`
	AgentSession  *HerdrAgentSession `json:"agent_session"`
	Revision      uint64             `json:"revision"`
}

// HerdrAgentRow is one live agent from session.snapshot.
type HerdrAgentRow struct {
	HerdrPaneRow
	Name           string `json:"name"`
	StateChangeSeq uint64 `json:"state_change_seq"`
}

// HerdrPaneLayout is the active-pane map for one tab.
type HerdrPaneLayout struct {
	WorkspaceID   string `json:"workspace_id"`
	TabID         string `json:"tab_id"`
	FocusedPaneID string `json:"focused_pane_id"`
}

// HerdrSessionSnapshot is the live session.snapshot body.
type HerdrSessionSnapshot struct {
	GitByDirectory     map[string]WorkspaceGit `json:"-"`
	Version            string                  `json:"version"`
	Protocol           uint32                  `json:"protocol"`
	FocusedWorkspaceID string                  `json:"focused_workspace_id"`
	FocusedTabID       string                  `json:"focused_tab_id"`
	FocusedPaneID      string                  `json:"focused_pane_id"`
	Workspaces         []HerdrWorkspaceRow     `json:"workspaces"`
	Tabs               []HerdrTabRow           `json:"tabs"`
	Panes              []HerdrPaneRow          `json:"panes"`
	Layouts            []HerdrPaneLayout       `json:"layouts"`
	Agents             []HerdrAgentRow         `json:"agents"`
}

type herdrSnapshotEnvelope struct {
	Type     string                `json:"type"`
	Snapshot *HerdrSessionSnapshot `json:"snapshot"`
}

// LoadHerdrSessionSnapshot loads session.snapshot (Herdr 0.9.0 shape only).
func LoadHerdrSessionSnapshot() (HerdrSessionSnapshot, ServerContinuityWitness, error) {
	return LoadHerdrSessionSnapshotContext(context.Background())
}

func LoadHerdrSessionSnapshotContext(ctx context.Context) (HerdrSessionSnapshot, ServerContinuityWitness, error) {
	var envelope herdrSnapshotEnvelope
	witness, err := CallHerdrMethodContext(ctx, "session.snapshot", map[string]any{}, &envelope)
	if err != nil {
		return HerdrSessionSnapshot{}, witness, err
	}
	if envelope.Type != "session_snapshot" || envelope.Snapshot == nil {
		return HerdrSessionSnapshot{}, witness, fmt.Errorf("hseh herdr socket: session.snapshot missing snapshot")
	}
	return *envelope.Snapshot, witness, nil
}

// FocusHerdrWorkspace focuses a live workspace.
func FocusHerdrWorkspace(workspaceID string) error {
	return FocusHerdrWorkspaceContext(context.Background(), workspaceID)
}

func FocusHerdrWorkspaceContext(ctx context.Context, workspaceID string) error {
	var result json.RawMessage
	_, err := CallHerdrMethodContext(ctx, "workspace.focus", map[string]any{"workspace_id": workspaceID}, &result)
	return err
}

type herdrAgentFocusEnvelope struct {
	Type  string         `json:"type"`
	Agent *HerdrAgentRow `json:"agent"`
}

// FocusHerdrAgent focuses a live agent pane, then syncs the hosted tab from the returned agent_info.
func FocusHerdrAgent(paneID string) error {
	return FocusHerdrAgentContext(context.Background(), paneID)
}

func FocusHerdrAgentContext(ctx context.Context, paneID string) error {
	var envelope herdrAgentFocusEnvelope
	_, err := CallHerdrMethodContext(ctx, "agent.focus", map[string]any{"target": paneID}, &envelope)
	if err != nil {
		return err
	}
	if envelope.Agent == nil || envelope.Agent.TabID == "" {
		return fmt.Errorf("hseh focus: agent.focus missing tab_id")
	}
	var result json.RawMessage
	_, err = CallHerdrMethodContext(ctx, "tab.focus", map[string]any{"tab_id": envelope.Agent.TabID}, &result)
	return err
}

// HerdrPaneReadResult is socket pane.read text plus revision.
type HerdrPaneReadResult struct {
	PaneID    string `json:"pane_id"`
	Text      string `json:"text"`
	Revision  uint64 `json:"revision"`
	Truncated bool   `json:"truncated"`
	Format    string `json:"format"`
	Source    string `json:"source"`
}

type herdrPaneReadEnvelope struct {
	Type string               `json:"type"`
	Read *HerdrPaneReadResult `json:"read"`
}

// ReadHerdrPaneVisibleANSI reads the visible viewport without focusing the pane.
func ReadHerdrPaneVisibleANSI(paneID string) (HerdrPaneReadResult, ServerContinuityWitness, error) {
	return ReadHerdrPaneVisibleANSIContext(context.Background(), paneID)
}

func ReadHerdrPaneVisibleANSIContext(ctx context.Context, paneID string) (HerdrPaneReadResult, ServerContinuityWitness, error) {
	var envelope herdrPaneReadEnvelope
	params := map[string]any{
		"pane_id":    paneID,
		"source":     "visible",
		"format":     "ansi",
		"strip_ansi": false,
	}
	witness, err := CallHerdrMethodContext(ctx, "pane.read", params, &envelope)
	if err != nil {
		return HerdrPaneReadResult{}, witness, err
	}
	if envelope.Type != "pane_read" || envelope.Read == nil {
		return HerdrPaneReadResult{}, witness, fmt.Errorf("hseh herdr socket: pane.read missing text for %s", paneID)
	}
	return *envelope.Read, witness, nil
}

type herdrPluginPaneOpenEnvelope struct {
	Type string `json:"type"`
}

// OpenHsehPluginPopup asks Herdr to open the hseh popup pane entrypoint.
func OpenHsehPluginPopup(view string) error {
	pluginID := osGetenvPluginID()
	var envelope herdrPluginPaneOpenEnvelope
	_, err := CallHerdrMethodContext(withHerdrHedge(context.Background()), "plugin.pane.open", map[string]any{
		"plugin_id":  pluginID,
		"entrypoint": view,
		"placement":  "popup",
		"width":      "85%",
		"height":     "80%",
		"focus":      true,
	}, &envelope)
	if err != nil {
		// The hedged duplicate of this request, or a repeated keypress, finds the
		// popup already open. Either way the popup the user asked for is on screen.
		if strings.Contains(err.Error(), "ui_busy") {
			return nil
		}
		return err
	}
	if envelope.Type != "ok" && envelope.Type != "plugin_pane_opened" {
		return fmt.Errorf("hseh launch: unexpected plugin.pane.open type %s", envelope.Type)
	}
	return nil
}

func osGetenvPluginID() string {
	if id := os.Getenv("HERDR_PLUGIN_ID"); id != "" {
		return id
	}
	return "hseh"
}

// ActiveWorkspacePaneID returns the active pane in a workspace's active tab.
func ActiveWorkspacePaneID(snapshot HerdrSessionSnapshot, workspaceID string) string {
	activeTabID := ""
	for _, workspace := range snapshot.Workspaces {
		if workspace.WorkspaceID == workspaceID {
			activeTabID = workspace.ActiveTabID
			break
		}
	}
	if activeTabID == "" {
		return ""
	}
	for _, layout := range snapshot.Layouts {
		if layout.TabID == activeTabID {
			return layout.FocusedPaneID
		}
	}
	return ""
}

func workspaceByID(snapshot HerdrSessionSnapshot, workspaceID string) (HerdrWorkspaceRow, bool) {
	for _, workspace := range snapshot.Workspaces {
		if workspace.WorkspaceID == workspaceID {
			return workspace, true
		}
	}
	return HerdrWorkspaceRow{}, false
}

func tabByID(snapshot HerdrSessionSnapshot, tabID string) (HerdrTabRow, bool) {
	for _, tab := range snapshot.Tabs {
		if tab.TabID == tabID {
			return tab, true
		}
	}
	return HerdrTabRow{}, false
}
