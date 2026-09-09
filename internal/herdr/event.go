package herdr

import (
	"encoding/json"
	"fmt"
)

type PluginEventEnvelope struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type PluginEventData struct {
	Type           string           `json:"type"`
	WorkspaceID    string           `json:"workspace_id"`
	PaneID         string           `json:"pane_id"`
	PreviousPaneID string           `json:"previous_pane_id"`
	Agent          string           `json:"agent"`
	Released       bool             `json:"released"`
	Pane           *PluginEventPane `json:"pane"`
}

type PluginEventPane struct {
	PaneID string `json:"pane_id"`
}

func ParsePluginEventJSON(raw string) (PluginEventEnvelope, PluginEventData, error) {
	var envelope PluginEventEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return PluginEventEnvelope{}, PluginEventData{}, fmt.Errorf("hseh event: malformed HERDR_PLUGIN_EVENT_JSON: %w", err)
	}
	var data PluginEventData
	if len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return envelope, PluginEventData{}, fmt.Errorf("hseh event: malformed event data: %w", err)
		}
	}
	return envelope, data, nil
}

func PluginEventPaneID(data PluginEventData) string {
	if data.Pane != nil && data.Pane.PaneID != "" {
		return data.Pane.PaneID
	}
	return data.PaneID
}
