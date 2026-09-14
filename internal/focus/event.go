package focus

import (
	"context"
	"fmt"

	"github.com/regutierrez/hseh/internal/config"
	"github.com/regutierrez/hseh/internal/herdr"
)

func ApplyPluginEvent(name string, data herdr.PluginEventData) error {
	workspaceID := data.WorkspaceID
	paneID := herdr.PluginEventPaneID(data)
	stateDir := config.StateDir()
	return withLock(stateDir, func() error {
		snapshot, witness, err := herdr.LoadSessionSnapshotContext(context.Background())
		if err != nil {
			return err
		}
		history, err := loadValidated(stateDir, witness)
		if err != nil {
			return err
		}
		switch name {
		case "workspace.focused":
			if workspaceID == "" {
				return fmt.Errorf("hseh event: workspace.focused missing workspace_id")
			}
			history = recordWorkspaceFocus(history, workspaceID)
		case "workspace.closed":
			if workspaceID == "" {
				return fmt.Errorf("hseh event: workspace.closed missing workspace_id")
			}
			history = removeWorkspace(history, workspaceID)
		case "pane.focused":
			if paneID == "" {
				return fmt.Errorf("hseh event: pane.focused missing pane_id")
			}
			history = ensurePaneOccupantFromLive(history, snapshot, paneID)
			history = RecordAgentPaneFocus(history, paneID)
		case "pane.closed", "pane.exited":
			if paneID == "" {
				return fmt.Errorf("hseh event: %s missing pane_id", name)
			}
			history = clearPaneOccupant(history, paneID)
		case "pane.moved":
			previous := data.PreviousPaneID
			if previous == "" || paneID == "" {
				return fmt.Errorf("hseh event: pane.moved missing previous_pane_id or pane")
			}
			history = rekeyMovedPaneOccupant(history, previous, paneID)
		case "pane.agent_detected":
			if paneID == "" {
				return fmt.Errorf("hseh event: pane.agent_detected missing pane_id")
			}
			sessionVal := ""
			for _, agent := range snapshot.Agents {
				if agent.PaneID == paneID {
					sessionVal = sessionValue(agent.AgentSession)
					break
				}
			}
			history = ApplyVerifiedOccupantTransition(history, paneID, data.Agent, sessionVal, data.Released)
		default:
			return nil
		}
		history = Prune(history, snapshot)
		if name == "pane.focused" {
			history = acknowledgeAgentPresentation(history, snapshot, paneID)
		}
		return WriteFile(stateDir, history)
	})
}

func ensurePaneOccupantFromLive(history History, snapshot herdr.SessionSnapshot, paneID string) History {
	for _, agent := range snapshot.Agents {
		if agent.PaneID == paneID {
			return ApplyVerifiedOccupantTransition(history, paneID, agent.Agent, sessionValue(agent.AgentSession), false)
		}
	}
	if _, ok := history.Occupants[paneID]; ok {
		return clearPaneOccupant(history, paneID)
	}
	return history
}
