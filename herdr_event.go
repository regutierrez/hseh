package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type pluginEventEnvelope struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type pluginEventData struct {
	Type           string           `json:"type"`
	WorkspaceID    string           `json:"workspace_id"`
	PaneID         string           `json:"pane_id"`
	PreviousPaneID string           `json:"previous_pane_id"`
	Agent          string           `json:"agent"`
	Released       bool             `json:"released"`
	Pane           *pluginEventPane `json:"pane"`
}

type pluginEventPane struct {
	PaneID string `json:"pane_id"`
}

func parsePluginEventJSON(raw string) (pluginEventEnvelope, pluginEventData, error) {
	var envelope pluginEventEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return pluginEventEnvelope{}, pluginEventData{}, fmt.Errorf("hseh event: malformed HERDR_PLUGIN_EVENT_JSON: %w", err)
	}
	var data pluginEventData
	if len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return envelope, pluginEventData{}, fmt.Errorf("hseh event: malformed event data: %w", err)
		}
	}
	return envelope, data, nil
}

func pluginEventPaneID(data pluginEventData) string {
	if data.Pane != nil && data.Pane.PaneID != "" {
		return data.Pane.PaneID
	}
	return data.PaneID
}

func runPluginEvent() error {
	name := os.Getenv("HERDR_PLUGIN_EVENT")
	if name == "" {
		return fmt.Errorf("hseh event: HERDR_PLUGIN_EVENT is not set")
	}
	raw := os.Getenv("HERDR_PLUGIN_EVENT_JSON")
	if raw == "" {
		return fmt.Errorf("hseh event: HERDR_PLUGIN_EVENT_JSON is missing")
	}
	_, data, err := parsePluginEventJSON(raw)
	if err != nil {
		return err
	}
	return applyPluginEvent(name, data)
}

func applyPluginEvent(name string, data pluginEventData) error {
	workspaceID := data.WorkspaceID
	paneID := pluginEventPaneID(data)
	stateDir := pluginStateDir()
	return withFocusHistoryLock(stateDir, func() error {
		snapshot, witness, err := LoadHerdrSessionSnapshot()
		if err != nil {
			return err
		}
		history, err := LoadValidatedFocusHistory(stateDir, witness)
		if err != nil {
			return err
		}
		switch name {
		case "workspace.focused":
			if workspaceID == "" {
				return fmt.Errorf("hseh event: workspace.focused missing workspace_id")
			}
			history = RecordWorkspaceFocus(history, workspaceID)
		case "workspace.closed":
			if workspaceID == "" {
				return fmt.Errorf("hseh event: workspace.closed missing workspace_id")
			}
			history = RemoveWorkspace(history, workspaceID)
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
			history = ClearPaneOccupant(history, paneID)
		case "pane.moved":
			previous := data.PreviousPaneID
			if previous == "" || paneID == "" {
				return fmt.Errorf("hseh event: pane.moved missing previous_pane_id or pane")
			}
			history = RekeyMovedPaneOccupant(history, previous, paneID)
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
		history = pruneFocusHistory(history, snapshot)
		return writeFocusHistoryFile(stateDir, history)
	})
}

func ensurePaneOccupantFromLive(history FocusHistory, snapshot HerdrSessionSnapshot, paneID string) FocusHistory {
	for _, agent := range snapshot.Agents {
		if agent.PaneID == paneID {
			return ApplyVerifiedOccupantTransition(history, paneID, agent.Agent, sessionValue(agent.AgentSession), false)
		}
	}
	if _, ok := history.Occupants[paneID]; ok {
		return ClearPaneOccupant(history, paneID)
	}
	return history
}

func runWorkspaceSwitch() error {
	snapshot, witness, err := LoadHerdrSessionSnapshot()
	if err != nil {
		return err
	}
	stateDir := pluginStateDir()
	var target string
	if err := withFocusHistoryLock(stateDir, func() error {
		history, loadErr := LoadValidatedFocusHistory(stateDir, witness)
		if loadErr != nil {
			return loadErr
		}
		history, target = SelectNextWorkspace(history, snapshot, time.Now().UnixMilli())
		return writeFocusHistoryFile(stateDir, history)
	}); err != nil {
		return err
	}
	if target == "" {
		return nil
	}
	return FocusHerdrWorkspace(target)
}

func parsePickerView(view string) (string, error) {
	switch view {
	case "", pickerViewSpaces:
		return pickerViewSpaces, nil
	case pickerViewAgents:
		return pickerViewAgents, nil
	default:
		return "", fmt.Errorf("hseh: invalid view %s", view)
	}
}

func runPickerList(view string) error {
	view, err := parsePickerView(view)
	if err != nil {
		return err
	}
	snapshot, witness, err := LoadHerdrSessionSnapshot()
	if err != nil {
		return err
	}
	history, err := LoadPrunedFocusHistory(snapshot, witness)
	if err != nil {
		return err
	}
	layout, configErrs := LoadSidebarLayout("")
	_, moreErrs := LoadPreviewPollInterval()
	configErrs = append(configErrs, moreErrs...)
	definitions, defErrs := LoadSpaceDefinitions(spaceDefinitionsDir())
	configErrs = append(configErrs, defErrs...)
	state, assocErr := loadReconciledSpaceAssociationState(pluginStateDir(), witness)
	var records, unresolved []SpaceAssociationRecord
	if assocErr != nil {
		configErrs = append(configErrs, assocErr.Error())
	} else {
		records = state.Records
		unresolved = state.Unresolved
	}
	if view != pickerViewAgents {
		snapshot.GitByDirectory = loadWorkspaceGit(context.Background(), snapshot)
	}
	items := appendUnopenedDefinitionItems(BuildPickerItemsWithLayout(view, snapshot, history, layout), view, snapshot, definitions, records, unresolved)
	doc := PickerListDocument{
		Session: PickerListSession{Name: herdrSessionName(HerdrSocketPath()), SocketPath: HerdrSocketPath()},
		View:    view,
		Items:   items,
		Errors:  configErrs,
	}
	payload, err := encodePickerListJSON(doc)
	if err != nil {
		return err
	}
	fmt.Println(string(payload))
	return nil
}

func runLaunch(view string) error {
	view, err := parsePickerView(view)
	if err != nil {
		return err
	}
	return OpenHsehPluginPopup(view)
}
