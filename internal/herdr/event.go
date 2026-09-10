package herdr

import (
	"encoding/json"
	"fmt"
	"os"
)

// PluginEventData is the "data" member of a Herdr plugin event envelope.
type PluginEventData struct {
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

// PluginEventFromEnv reads the event name and payload Herdr hands an event hook process.
func PluginEventFromEnv() (name string, data PluginEventData, err error) {
	name = os.Getenv("HERDR_PLUGIN_EVENT")
	if name == "" {
		return "", PluginEventData{}, fmt.Errorf("hseh event: HERDR_PLUGIN_EVENT is not set")
	}
	raw := os.Getenv("HERDR_PLUGIN_EVENT_JSON")
	if raw == "" {
		return "", PluginEventData{}, fmt.Errorf("hseh event: HERDR_PLUGIN_EVENT_JSON is missing")
	}
	data, err = ParsePluginEventJSON(raw)
	return name, data, err
}

// ParsePluginEventJSON decodes the data member of a plugin event envelope.
func ParsePluginEventJSON(raw string) (PluginEventData, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return PluginEventData{}, fmt.Errorf("hseh event: malformed HERDR_PLUGIN_EVENT_JSON: %w", err)
	}
	var data PluginEventData
	if len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return PluginEventData{}, fmt.Errorf("hseh event: malformed event data: %w", err)
		}
	}
	return data, nil
}

// PluginEventPaneID prefers the nested pane object Herdr sends for pane events.
func PluginEventPaneID(data PluginEventData) string {
	if data.Pane != nil && data.Pane.PaneID != "" {
		return data.Pane.PaneID
	}
	return data.PaneID
}
