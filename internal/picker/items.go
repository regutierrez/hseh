package picker

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/gitinfo"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/termtext"
	"github.com/sahilm/fuzzy"
)

const (
	KindSpace      = "space"
	KindAgent      = "agent"
	KindDefinition = "definition"
	ViewSpaces     = "spaces"
	ViewAgents     = "agents"
)

// Item is one flat picker row with a stable target id.
type Item struct {
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

// ListDocument is the public JSON listing shape.
type ListDocument struct {
	Session ListSession `json:"session"`
	View    string      `json:"view"`
	Items   []Item      `json:"items"`
	Errors  []string    `json:"errors,omitempty"`
}

// ListSession names the Herdr session the listing came from.
type ListSession struct {
	Name       string `json:"name"`
	SocketPath string `json:"socket_path"`
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

// BuildItems builds Spaces or Agents rows from live snapshot plus history.
func BuildItems(view string, snapshot herdr.SessionSnapshot, history focus.History, spaceRows, agentRows [][]string) []Item {
	return BuildItemsWithLayout(view, snapshot, history, sidebarLayoutFromNameRows(spaceRows, agentRows))
}

// BuildItemsWithLayout uses a parsed sidebar layout for styles and per-agent rows.
func BuildItemsWithLayout(view string, snapshot herdr.SessionSnapshot, history focus.History, layout SidebarLayout) []Item {
	spaces := buildSpaceItems(snapshot, history, layout)
	agents := buildAgentItems(snapshot, history, layout)
	switch view {
	case ViewAgents:
		return agents
	default:
		return spaces
	}
}

func buildSpaceItems(snapshot herdr.SessionSnapshot, history focus.History, layout SidebarLayout) []Item {
	indexOf := map[string]int{}
	for i, id := range history.Spaces {
		indexOf[id] = i
	}
	workspaces := append([]herdr.WorkspaceRow{}, snapshot.Workspaces...)
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
	var items []Item
	for _, workspace := range workspaces {
		plain, display := renderSpaceSidebarRows(workspace, snapshot.GitByDirectory[herdr.ActiveWorkspaceDirectory(snapshot, workspace.WorkspaceID)], layout)
		items = append(items, Item{
			Kind:        KindSpace,
			ID:          SelectionID(KindSpace, workspace.WorkspaceID),
			WorkspaceID: workspace.WorkspaceID,
			Label:       termtext.StripControls(workspace.Label),
			Status:      workspace.AgentStatus,
			Rows:        plain,
			DisplayRows: display,
			SearchText:  strings.Join(plain, " "),
			PreviewPane: herdr.ActiveWorkspacePaneID(snapshot, workspace.WorkspaceID),
		})
	}
	return items
}

func buildAgentItems(snapshot herdr.SessionSnapshot, history focus.History, layout SidebarLayout) []Item {
	agents := append([]herdr.AgentRow{}, snapshot.Agents...)
	sort.SliceStable(agents, func(i, j int) bool {
		left := focus.AgentPriorityRank(agents[i].AgentStatus)
		right := focus.AgentPriorityRank(agents[j].AgentStatus)
		if left != right {
			return left < right
		}
		if agents[i].StateChangeSeq != agents[j].StateChangeSeq {
			return agents[i].StateChangeSeq > agents[j].StateChangeSeq
		}
		return i < j
	})
	var items []Item
	for _, agent := range agents {
		liveID, ok := focus.CurrentAgentLiveID(history, agent.PaneID)
		if !ok {
			liveID = focus.AgentLiveID{PaneID: agent.PaneID, Generation: 1}
		}
		plain, display := renderAgentSidebarRows(snapshot, agent, layout)
		items = append(items, Item{
			Kind:        KindAgent,
			ID:          SelectionID(KindAgent, liveID.String()),
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
	return strings.TrimSpace(termtext.StripControls(value))
}

func renderSpaceSidebarRows(workspace herdr.WorkspaceRow, git gitinfo.WorkspaceGit, layout SidebarLayout) (plain, display []string) {
	prefix, styledPrefix := statePrefix(workspace.AgentStatus, layout.StatusIndicators)
	name := sanitizeTokenValue(firstNonEmpty(workspace.Label, workspace.WorkspaceID))
	detail := strings.TrimSpace(git.Branch + " " + git.Status)
	if git.Branch != "" {
		detail = gitBranchIcon + " " + detail
	}
	row := prefix + name
	styled := styledPrefix + "\x1b[1m" + name + "\x1b[0m"
	if detail != "" {
		row += "  " + detail
		styled += "  " + mutedSGR + detail + "\x1b[0m"
	}
	return []string{row}, []string{styled}
}

func renderAgentSidebarRows(snapshot herdr.SessionSnapshot, agent herdr.AgentRow, layout SidebarLayout) (plain, display []string) {
	workspaceLabel := agent.WorkspaceID
	if workspace, ok := herdr.WorkspaceByID(snapshot, agent.WorkspaceID); ok && workspace.Label != "" {
		workspaceLabel = workspace.Label
	}
	tabLabel := ""
	if tab, ok := herdr.TabByID(snapshot, agent.TabID); ok {
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
	prefix, styledPrefix := statePrefix(agent.AgentStatus, layout.StatusIndicators)
	heading := agentHarnessIcon(agent.Agent) + " " + sanitizeTokenValue(firstNonEmpty(tabLabel, agent.Label, agent.PaneID))
	location := sanitizeTokenValue(workspaceLabel)
	if dir := abbreviatedDirectory(firstNonEmpty(agent.ForegroundCwd, agent.Cwd)); dir != "" {
		location += " (" + sanitizeTokenValue(dir) + ")"
	}
	plain = []string{prefix + heading, location}
	display = []string{styledPrefix + heading, mutedSGR + location + "\x1b[0m"}
	if description := strings.Join(details, " "); description != "" {
		plain = append(plain, description)
		display = append(display, mutedSGR+description+"\x1b[0m")
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

// FilterItems ranks matching items by fuzzy quality, then original order.
func FilterItems(items []Item, query string) []Item {
	filtered, _ := filterItemsInto(nil, nil, items, query)
	return filtered
}

// filterItemsInto appends matches to dst (reusing its backing array) and reuses
// the search-target scratch slice so per-keystroke filtering does not reallocate.
func filterItemsInto(dst []Item, scratch []string, items []Item, query string) ([]Item, []string) {
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

func EncodeListJSON(doc ListDocument) ([]byte, error) {
	if doc.Items == nil {
		doc.Items = []Item{}
	}
	return json.MarshalIndent(doc, "", "  ")
}

func ParseView(view string) (string, error) {
	switch view {
	case "", ViewSpaces:
		return ViewSpaces, nil
	case ViewAgents:
		return ViewAgents, nil
	default:
		return "", fmt.Errorf("hseh: invalid view %s", view)
	}
}
