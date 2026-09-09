package herdr

type pluginEventPayload struct {
	Type           string
	Event          string
	WorkspaceID    string
	PaneID         string
	PreviousPaneID string
	Agent          string
	Released       bool
}

func (p *pluginEventPayload) UnmarshalJSON(b []byte) error {
	_, data, err := ParsePluginEventJSON(string(b))
	if err != nil {
		return err
	}
	p.Type = data.Type
	p.WorkspaceID = data.WorkspaceID
	p.PaneID = PluginEventPaneID(data)
	p.PreviousPaneID = data.PreviousPaneID
	p.Agent = data.Agent
	p.Released = data.Released
	return nil
}
