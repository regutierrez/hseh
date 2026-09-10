package herdr

import (
	"context"
	"fmt"
	"strings"

	"github.com/regutierrez/hseh/internal/config"
)

// AgentSession is a native conversation reference, not a live occupant id.
type AgentSession struct {
	Value string `json:"value"`
}

// WorkspaceRow is one live workspace from session.snapshot.
type WorkspaceRow struct {
	WorkspaceID string    `json:"workspace_id"`
	Label       string    `json:"label"`
	Focused     bool      `json:"focused"`
	ActiveTabID string    `json:"active_tab_id"`
	AgentStatus string    `json:"agent_status"`
	Worktree    *Worktree `json:"worktree"`
}

// Worktree is optional Git checkout provenance on a workspace.
type Worktree struct {
	CheckoutPath string `json:"checkout_path"`
}

// TabRow is one live tab from session.snapshot.
type TabRow struct {
	TabID string `json:"tab_id"`
	Label string `json:"label"`
}

// PaneRow is one live pane from session.snapshot.
type PaneRow struct {
	PaneID        string            `json:"pane_id"`
	WorkspaceID   string            `json:"workspace_id"`
	TabID         string            `json:"tab_id"`
	Cwd           string            `json:"cwd"`
	ForegroundCwd string            `json:"foreground_cwd"`
	Label         string            `json:"label"`
	Agent         string            `json:"agent"`
	Title         string            `json:"title"`
	TerminalTitle string            `json:"terminal_title"`
	TitleStripped string            `json:"terminal_title_stripped"`
	DisplayAgent  string            `json:"display_agent"`
	AgentStatus   string            `json:"agent_status"`
	StateLabels   map[string]string `json:"state_labels"`
	Tokens        map[string]string `json:"tokens"`
	AgentSession  *AgentSession     `json:"agent_session"`
}

// AgentRow is one live agent from session.snapshot.
type AgentRow struct {
	PaneRow
	StateChangeSeq uint64 `json:"state_change_seq"`
}

// PaneLayout is the active-pane map for one tab.
type PaneLayout struct {
	TabID         string `json:"tab_id"`
	FocusedPaneID string `json:"focused_pane_id"`
}

// SessionSnapshot is the live session.snapshot body.
type SessionSnapshot struct {
	FocusedWorkspaceID string         `json:"focused_workspace_id"`
	FocusedPaneID      string         `json:"focused_pane_id"`
	Workspaces         []WorkspaceRow `json:"workspaces"`
	Tabs               []TabRow       `json:"tabs"`
	Panes              []PaneRow      `json:"panes"`
	Layouts            []PaneLayout   `json:"layouts"`
	Agents             []AgentRow     `json:"agents"`
}

type SnapshotEnvelope struct {
	Type     string           `json:"type"`
	Snapshot *SessionSnapshot `json:"snapshot"`
}

// LoadSessionSnapshotContext loads session.snapshot.
func LoadSessionSnapshotContext(ctx context.Context) (SessionSnapshot, ContinuityWitness, error) {
	var envelope SnapshotEnvelope
	witness, err := callContext(ctx, "session.snapshot", map[string]any{}, &envelope)
	if err != nil {
		return SessionSnapshot{}, witness, err
	}
	if envelope.Type != "session_snapshot" || envelope.Snapshot == nil {
		return SessionSnapshot{}, witness, fmt.Errorf("hseh herdr socket: session.snapshot missing snapshot")
	}
	return *envelope.Snapshot, witness, nil
}

// FocusWorkspaceContext focuses a live workspace.
func FocusWorkspaceContext(ctx context.Context, workspaceID string) error {
	_, err := callContext(ctx, "workspace.focus", map[string]any{"workspace_id": workspaceID}, nil)
	return err
}

type AgentFocusEnvelope struct {
	Type  string    `json:"type"`
	Agent *AgentRow `json:"agent"`
}

// FocusAgentContext focuses a live agent pane, then syncs the hosted tab from the returned agent_info.
func FocusAgentContext(ctx context.Context, paneID string) error {
	var envelope AgentFocusEnvelope
	_, err := callContext(ctx, "agent.focus", map[string]any{"target": paneID}, &envelope)
	if err != nil {
		return err
	}
	if envelope.Agent == nil || envelope.Agent.TabID == "" {
		return fmt.Errorf("hseh focus: agent.focus missing tab_id")
	}
	_, err = callContext(ctx, "tab.focus", map[string]any{"tab_id": envelope.Agent.TabID}, nil)
	return err
}

// PaneReadResult is socket pane.read text plus revision.
type PaneReadResult struct {
	Text     string `json:"text"`
	Revision uint64 `json:"revision"`
}

type PaneReadEnvelope struct {
	Type string          `json:"type"`
	Read *PaneReadResult `json:"read"`
}

// ReadPaneVisibleANSIContext reads the visible viewport without focusing the pane.
func ReadPaneVisibleANSIContext(ctx context.Context, paneID string) (PaneReadResult, ContinuityWitness, error) {
	var envelope PaneReadEnvelope
	params := map[string]any{
		"pane_id":    paneID,
		"source":     "visible",
		"format":     "ansi",
		"strip_ansi": false,
	}
	witness, err := callContext(ctx, "pane.read", params, &envelope)
	if err != nil {
		return PaneReadResult{}, witness, err
	}
	if envelope.Type != "pane_read" || envelope.Read == nil {
		return PaneReadResult{}, witness, fmt.Errorf("hseh herdr socket: pane.read missing text for %s", paneID)
	}
	return *envelope.Read, witness, nil
}

type pluginPaneOpenEnvelope struct {
	Type string `json:"type"`
}

// OpenPluginPopup asks Herdr to open the hseh popup pane entrypoint, sized
// from popup_width/popup_height in hseh.toml. Config errors are not fatal
// here: the popup itself reports them in its footer.
func OpenPluginPopup(view string) error {
	pluginID := config.PluginID()
	settings, _ := config.Load()
	var envelope pluginPaneOpenEnvelope
	_, err := callContext(WithHedge(context.Background()), "plugin.pane.open", map[string]any{
		"plugin_id":  pluginID,
		"entrypoint": view,
		"placement":  "popup",
		"width":      settings.PopupWidth.Param(),
		"height":     settings.PopupHeight.Param(),
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

// activeWorkspacePaneID returns the active pane in a workspace's active tab.
func activeWorkspacePaneID(snapshot SessionSnapshot, workspaceID string) string {
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

func WorkspaceByID(snapshot SessionSnapshot, workspaceID string) (WorkspaceRow, bool) {
	for _, workspace := range snapshot.Workspaces {
		if workspace.WorkspaceID == workspaceID {
			return workspace, true
		}
	}
	return WorkspaceRow{}, false
}

func TabByID(snapshot SessionSnapshot, tabID string) (TabRow, bool) {
	for _, tab := range snapshot.Tabs {
		if tab.TabID == tabID {
			return tab, true
		}
	}
	return TabRow{}, false
}

func ActiveWorkspaceDirectory(snapshot SessionSnapshot, workspaceID string) string {
	paneID := activeWorkspacePaneID(snapshot, workspaceID)
	for _, pane := range snapshot.Panes {
		if pane.PaneID == paneID {
			if strings.TrimSpace(pane.ForegroundCwd) != "" {
				return pane.ForegroundCwd
			}
			return pane.Cwd
		}
	}
	return ""
}
