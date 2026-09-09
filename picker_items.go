package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"
)

const (
	pickerKindSpace      = "space"
	pickerKindAgent      = "agent"
	pickerKindDefinition = "definition"
	pickerViewSpaces     = "spaces"
	pickerViewAgents     = "agents"
)

// PickerItem is one flat picker row with a stable target id.
type PickerItem struct {
	Kind         string   `json:"kind"`
	ID           string   `json:"id"`
	WorkspaceID  string   `json:"workspace_id,omitempty"`
	PaneID       string   `json:"pane_id,omitempty"`
	DefinitionID string   `json:"definition_id,omitempty"`
	Label        string   `json:"label,omitempty"`
	Status       string   `json:"status,omitempty"`
	Rows         []string `json:"rows"`
	DisplayRows  []string `json:"-"`
	SearchText   string   `json:"-"`
	PreviewPane  string   `json:"-"`
	PreviewText  string   `json:"-"`
	// Matches are byte offsets into SearchText matched by the current query, used for highlighting.
	Matches []int `json:"-"`
}

// PickerListDocument is the public JSON listing shape.
type PickerListDocument struct {
	Session PickerListSession `json:"session"`
	View    string            `json:"view"`
	Items   []PickerItem      `json:"items"`
	Errors  []string          `json:"errors,omitempty"`
}

// PickerListSession names the Herdr session the listing came from.
type PickerListSession struct {
	Name       string `json:"name"`
	SocketPath string `json:"socket_path"`
}

func herdrSessionName(socketPath string) string {
	if name := os.Getenv("HERDR_SESSION"); name != "" {
		return name
	}
	const marker = "/sessions/"
	if index := strings.Index(socketPath, marker); index >= 0 {
		rest := socketPath[index+len(marker):]
		if slash := strings.Index(rest, "/"); slash > 0 {
			return rest[:slash]
		}
	}
	return "default"
}

func sidebarLayoutFromNameRows(spaceRows, agentRows [][]string) SidebarLayout {
	layout := defaultSidebarLayout()
	if len(spaceRows) > 0 {
		layout.SpaceRows = tokensFromNames(spaceRows)
	}
	if len(agentRows) > 0 {
		layout.AgentRows = tokensFromNames(agentRows)
	}
	return layout
}

// BuildPickerItems builds Spaces or Agents rows from live snapshot plus history.
func BuildPickerItems(view string, snapshot HerdrSessionSnapshot, history FocusHistory, spaceRows, agentRows [][]string) []PickerItem {
	return BuildPickerItemsWithLayout(view, snapshot, history, sidebarLayoutFromNameRows(spaceRows, agentRows))
}

// BuildPickerItemsWithLayout uses a parsed sidebar layout for styles and per-agent rows.
func BuildPickerItemsWithLayout(view string, snapshot HerdrSessionSnapshot, history FocusHistory, layout SidebarLayout) []PickerItem {
	spaces := buildSpacePickerItems(snapshot, history, layout)
	agents := buildAgentPickerItems(snapshot, history, layout)
	switch view {
	case pickerViewAgents:
		return agents
	default:
		return spaces
	}
}

func buildSpacePickerItems(snapshot HerdrSessionSnapshot, history FocusHistory, layout SidebarLayout) []PickerItem {
	indexOf := map[string]int{}
	for i, id := range history.Spaces {
		indexOf[id] = i
	}
	workspaces := append([]HerdrWorkspaceRow{}, snapshot.Workspaces...)
	sort.SliceStable(workspaces, func(i, j int) bool {
		left, leftOK := indexOf[workspaces[i].WorkspaceID]
		right, rightOK := indexOf[workspaces[j].WorkspaceID]
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
	var items []PickerItem
	for _, workspace := range workspaces {
		plain, display := renderSpaceSidebarRows(workspace, snapshot.GitByDirectory[activeWorkspaceDirectory(snapshot, workspace.WorkspaceID)], layout)
		items = append(items, PickerItem{
			Kind:        pickerKindSpace,
			ID:          pickerSelectionID(pickerKindSpace, workspace.WorkspaceID),
			WorkspaceID: workspace.WorkspaceID,
			Label:       StripTerminalControls(workspace.Label),
			Status:      workspace.AgentStatus,
			Rows:        plain,
			DisplayRows: display,
			SearchText:  strings.Join(plain, " "),
			PreviewPane: ActiveWorkspacePaneID(snapshot, workspace.WorkspaceID),
		})
	}
	return items
}

func buildAgentPickerItems(snapshot HerdrSessionSnapshot, history FocusHistory, layout SidebarLayout) []PickerItem {
	agents := append([]HerdrAgentRow{}, snapshot.Agents...)
	sort.SliceStable(agents, func(i, j int) bool {
		left := agentPriorityRank(agents[i].AgentStatus)
		right := agentPriorityRank(agents[j].AgentStatus)
		if left != right {
			return left < right
		}
		if agents[i].StateChangeSeq != agents[j].StateChangeSeq {
			return agents[i].StateChangeSeq > agents[j].StateChangeSeq
		}
		return i < j
	})
	var items []PickerItem
	for _, agent := range agents {
		liveID, ok := currentAgentLiveID(history, agent.PaneID)
		if !ok {
			liveID = AgentLiveID{PaneID: agent.PaneID, Generation: 1}
		}
		plain, display := renderAgentSidebarRows(snapshot, agent, layout)
		items = append(items, PickerItem{
			Kind:        pickerKindAgent,
			ID:          pickerSelectionID(pickerKindAgent, liveID.String()),
			WorkspaceID: agent.WorkspaceID,
			PaneID:      agent.PaneID,
			Label:       firstNonEmpty(agent.DisplayAgent, agent.Title, agent.Agent, agent.PaneID),
			Status:      agent.AgentStatus,
			Rows:        plain,
			DisplayRows: display,
			SearchText:  strings.Join(plain, " "),
			PreviewPane: agent.PaneID,
		})
	}
	return items
}

func sanitizeTokenValue(value string) string {
	return strings.TrimSpace(StripTerminalControls(value))
}

func renderSpaceSidebarRows(workspace HerdrWorkspaceRow, git WorkspaceGit, layout SidebarLayout) (plain, display []string) {
	prefix, styledPrefix := pickerStatePrefix(workspace.AgentStatus, layout.StatusIndicators)
	name := sanitizeTokenValue(firstNonEmpty(workspace.Label, workspace.WorkspaceID))
	detail := strings.TrimSpace(git.Branch + " " + git.Status)
	if git.Branch != "" {
		detail = pickerGitBranchIcon + " " + detail
	}
	row := prefix + name
	styled := styledPrefix + "\x1b[1m" + name + "\x1b[0m"
	if detail != "" {
		row += "  " + detail
		styled += "  " + pickerMuted + detail + "\x1b[0m"
	}
	return []string{row}, []string{styled}
}

func renderAgentSidebarRows(snapshot HerdrSessionSnapshot, agent HerdrAgentRow, layout SidebarLayout) (plain, display []string) {
	workspaceLabel := agent.WorkspaceID
	if workspace, ok := workspaceByID(snapshot, agent.WorkspaceID); ok && workspace.Label != "" {
		workspaceLabel = workspace.Label
	}
	tabLabel := ""
	if tab, ok := tabByID(snapshot, agent.TabID); ok {
		tabLabel = tab.Label
	}
	values := map[string]string{
		"state_text":              sanitizeTokenValue(firstNonEmpty(agent.StateLabels[agent.AgentStatus], agent.AgentStatus)),
		"workspace":               sanitizeTokenValue(workspaceLabel),
		"tab":                     sanitizeTokenValue(tabLabel),
		"pane":                    sanitizeTokenValue(agent.Label),
		"agent":                   sanitizeTokenValue(firstNonEmpty(agent.DisplayAgent, agent.Agent)),
		"terminal_title":          sanitizeTokenValue(agent.TerminalTitle),
		"terminal_title_stripped": sanitizeTokenValue(agent.TitleStripped),
	}
	if agent.AgentStatus != "" {
		values["state_icon"] = stateIconGlyph(agent.AgentStatus, layout.StatusIndicators)
	}
	for name, token := range agent.Tokens {
		values["$"+name] = sanitizeTokenValue(token)
	}
	rows := layout.AgentRows
	if override, ok := layout.AgentRowsByAgent[agent.Agent]; ok {
		rows = override
	}
	// Keep Herdr's configured description rows, including per-harness overrides and reported tokens.
	var details []string
	if len(rows) > 1 {
		details, _ = renderSidebarRows(rows[1:], values, agent.AgentStatus)
	}
	prefix, styledPrefix := pickerStatePrefix(agent.AgentStatus, layout.StatusIndicators)
	heading := agentHarnessIcon(agent.Agent) + " " + sanitizeTokenValue(firstNonEmpty(tabLabel, agent.Label, agent.PaneID))
	location := sanitizeTokenValue(workspaceLabel)
	if dir := abbreviatedPickerDirectory(firstNonEmpty(agent.ForegroundCwd, agent.Cwd)); dir != "" {
		location += " (" + sanitizeTokenValue(dir) + ")"
	}
	plain = []string{prefix + heading, location}
	display = []string{styledPrefix + heading, pickerMuted + location + "\x1b[0m"}
	if description := strings.Join(details, " "); description != "" {
		plain = append(plain, description)
		display = append(display, pickerMuted+description+"\x1b[0m")
	}
	return plain, display
}

func renderTokenRows(layout [][]string, values map[string]string) []string {
	sanitized := map[string]string{}
	for key, value := range values {
		sanitized[key] = sanitizeTokenValue(value)
	}
	plain, _ := renderSidebarRows(tokensFromNames(layout), sanitized, sanitized["state_text"])
	return plain
}

func renderSidebarRows(layout [][]SidebarToken, values map[string]string, status string) (plain, display []string) {
	for _, cells := range layout {
		var plainParts []string
		var displayParts []string
		for _, cell := range cells {
			value := values[cell.Name]
			if value == "" {
				continue
			}
			plainParts = append(plainParts, value)
			displayParts = append(displayParts, stylePlainToken(cell, value, status))
		}
		if len(plainParts) == 0 {
			continue
		}
		plain = append(plain, strings.Join(plainParts, " · "))
		display = append(display, strings.Join(displayParts, " · "))
	}
	return plain, display
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// FilterPickerItems ranks matching items by fuzzy quality, then original order.
func FilterPickerItems(items []PickerItem, query string) []PickerItem {
	filtered, _ := filterPickerItemsInto(nil, nil, items, query)
	return filtered
}

// filterPickerItemsInto appends matches to dst (reusing its backing array) and reuses
// the search-target scratch slice so per-keystroke filtering does not reallocate.
func filterPickerItemsInto(dst []PickerItem, scratch []string, items []PickerItem, query string) ([]PickerItem, []string) {
	query = strings.TrimSpace(query)
	dst = dst[:0]
	if query == "" {
		dst = append(dst, items...)
		for i := range dst {
			dst[i].Matches = nil
		}
		return dst, scratch
	}
	scratch = scratch[:0]
	for _, item := range items {
		scratch = append(scratch, item.SearchText)
	}
	matches := fuzzy.Find(query, scratch)
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Index < matches[j].Index
	})
	for _, match := range matches {
		item := items[match.Index]
		item.Matches = match.MatchedIndexes
		dst = append(dst, item)
	}
	return dst, scratch
}

func encodePickerListJSON(doc PickerListDocument) ([]byte, error) {
	if doc.Items == nil {
		doc.Items = []PickerItem{}
	}
	return json.MarshalIndent(doc, "", "  ")
}
