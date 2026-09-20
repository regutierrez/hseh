package picker

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/gitinfo"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/space"
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

// Spaces rows carry a source badge: a live Herdr workspace or an unopened template.
const (
	SourceHerdr    = "herdr"
	SourceTemplate = "template"
)

// Item is one flat picker row with a stable target id.
type Item struct {
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	WorkspaceID  string `json:"workspace_id,omitempty"`
	PaneID       string `json:"pane_id,omitempty"`
	DefinitionID string `json:"definition_id,omitempty"`
	Label        string `json:"label,omitempty"`
	Status       string `json:"status,omitempty"`
	// Source and Path are set on Spaces rows only. Path is absolute and is what the preview lists.
	Source string `json:"source,omitempty"`
	Path   string `json:"path,omitempty"`
	// Recovery holds the exact `hseh recover` commands for an unresolved template; the popup shows them in the preview.
	Recovery    []string `json:"recovery,omitempty"`
	Rows        []string `json:"rows"`
	DisplayRows []string `json:"-"`
	SearchText  string   `json:"-"`
	PreviewText string   `json:"-"`
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

// buildItemsWithLayout builds Spaces or Agents rows from the live snapshot plus history,
// using the parsed sidebar layout for status glyphs and per-agent description rows.
// git is keyed by active directory and only decorates Spaces rows.
func buildItemsWithLayout(view string, snapshot herdr.SessionSnapshot, history focus.History, layout sidebarLayout, git map[string]gitinfo.WorkspaceGit, th colorTheme) []Item {
	if view == ViewAgents {
		return buildAgentItems(snapshot, history, layout, th)
	}
	return buildSpaceItems(snapshot, history, layout, git, th)
}

func buildSpaceItems(snapshot herdr.SessionSnapshot, history focus.History, layout sidebarLayout, gitByDir map[string]gitinfo.WorkspaceGit, th colorTheme) []Item {
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
		dir := herdr.ActiveWorkspaceDirectory(snapshot, workspace.WorkspaceID)
		git := gitByDir[dir]
		// The checkout root is stable while the user moves around inside a repository.
		path := firstNonEmpty(git.Root, dir)
		name := space.SanitizeDisplayText(firstNonEmpty(workspace.Label, workspace.WorkspaceID))
		plain, display := renderSpaceRow(spaceRow{status: workspace.AgentStatus, source: SourceHerdr, name: name, git: git, path: path}, layout.StatusIndicators, th)
		items = append(items, Item{
			Kind:        KindSpace,
			ID:          selectionID(KindSpace, workspace.WorkspaceID),
			WorkspaceID: workspace.WorkspaceID,
			Label:       termtext.StripControls(workspace.Label),
			Status:      workspace.AgentStatus,
			Source:      SourceHerdr,
			Path:        path,
			Rows:        []string{plain},
			DisplayRows: []string{display},
			SearchText:  plain,
		})
	}
	return items
}

func buildAgentItems(snapshot herdr.SessionSnapshot, history focus.History, layout sidebarLayout, th colorTheme) []Item {
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
		plain, display := renderAgentSidebarRows(snapshot, agent, layout, th)
		items = append(items, Item{
			Kind:        KindAgent,
			ID:          selectionID(KindAgent, liveID.String()),
			WorkspaceID: agent.WorkspaceID,
			PaneID:      agent.PaneID,
			Label:       firstNonEmpty(agent.DisplayAgent, agent.Title, agent.Agent, agent.PaneID),
			Status:      agent.AgentStatus,
			Rows:        plain,
			DisplayRows: display,
			SearchText:  strings.Join(plain, " "),
		})
	}
	return items
}

type spaceRow struct {
	status string
	source string
	name   string
	tag    string // muted note after the name, e.g. "recovery needed"
	git    gitinfo.WorkspaceGit
	path   string
}

// Nerd Fonts 3 glyphs for the source badge: nf-md-sheep (as in sesh) and nf-md-content_copy.
const (
	herdrSourceIcon    = "\U000f0cc6"
	templateSourceIcon = "\U000f018f"
)

// statusSlotWidth keeps badges aligned whether or not a row has an agent status icon.
const statusSlotWidth = 2

// badgeColumnWidth fits the widest badge ("<icon> template") plus one separator cell.
var badgeColumnWidth = len([]rune(templateSourceIcon+" "+SourceTemplate)) + 1

// pathSeparator sits between the row body and the absolute path so the renderer can drop the path column.
const pathSeparator = "  "

func sourceBadge(source string) string {
	if source == SourceTemplate {
		return templateSourceIcon + " " + SourceTemplate
	}
	return herdrSourceIcon + " " + SourceHerdr
}

func renderSpaceRow(row spaceRow, statusIndicators string, th colorTheme) (plain, display string) {
	th = themeOrDefault(th)
	muted := th.Muted
	prefix, styledPrefix := statePrefix(row.status, statusIndicators, th)
	if prefix == "" {
		prefix = strings.Repeat(" ", statusSlotWidth)
		styledPrefix = prefix
	}
	badge := sourceBadge(row.source)
	badge += strings.Repeat(" ", max(0, badgeColumnWidth-len([]rune(badge))))
	plain = prefix + badge + row.name
	display = styledPrefix + muted + badge + "\x1b[0m" + th.Text + "\x1b[1m" + row.name + "\x1b[0m"
	if row.tag != "" {
		plain += " (" + row.tag + ")"
		display += " " + muted + "(" + row.tag + ")" + "\x1b[0m"
	}
	if detail := strings.TrimSpace(row.git.Branch + " " + row.git.Status); detail != "" {
		if row.git.Branch != "" {
			detail = gitBranchIcon + " " + detail
		}
		plain += "  " + detail
		display += "  " + muted + detail + "\x1b[0m"
	}
	if row.path != "" {
		plain += pathSeparator + row.path
		display += pathSeparator + muted + row.path + "\x1b[0m"
	}
	return plain, display
}

// withoutPathColumn drops the trailing path from a Spaces row for narrow lists.
// Search text keeps the path so fuzzy matches on it still rank the row.
func (item Item) withoutPathColumn() Item {
	if item.Path == "" || len(item.Rows) == 0 {
		return item
	}
	rows := append([]string{}, item.Rows...)
	rows[0] = strings.TrimSuffix(rows[0], pathSeparator+item.Path)
	item.Rows = rows
	if len(item.DisplayRows) > 0 {
		display := append([]string{}, item.DisplayRows...)
		display[0] = stripDisplayPath(display[0], item.Path)
		item.DisplayRows = display
	}
	return item
}

func stripDisplayPath(display, path string) string {
	if path == "" {
		return display
	}
	i := strings.LastIndex(display, path)
	if i < 0 {
		return display
	}
	if j := strings.LastIndex(display[:i], pathSeparator); j >= 0 {
		return display[:j]
	}
	return display[:i]
}

func renderAgentSidebarRows(snapshot herdr.SessionSnapshot, agent herdr.AgentRow, layout sidebarLayout, th colorTheme) (plain, display []string) {
	workspaceLabel := agent.WorkspaceID
	if workspace, ok := herdr.WorkspaceByID(snapshot, agent.WorkspaceID); ok && workspace.Label != "" {
		workspaceLabel = workspace.Label
	}
	tabLabel := ""
	if tab, ok := herdr.TabByID(snapshot, agent.TabID); ok {
		tabLabel = tab.Label
	}
	values := map[string]string{
		"state_text":              space.SanitizeDisplayText(firstNonEmpty(agent.StateLabels[agent.AgentStatus], agent.AgentStatus)),
		"workspace":               space.SanitizeDisplayText(workspaceLabel),
		"tab":                     space.SanitizeDisplayText(tabLabel),
		"pane":                    space.SanitizeDisplayText(agent.Label),
		"agent":                   space.SanitizeDisplayText(firstNonEmpty(agent.DisplayAgent, agent.Agent)),
		"terminal_title":          space.SanitizeDisplayText(agent.TerminalTitle),
		"terminal_title_stripped": space.SanitizeDisplayText(agent.TitleStripped),
	}
	if agent.AgentStatus != "" {
		values["state_icon"] = stateIconGlyph(agent.AgentStatus, layout.StatusIndicators)
	}
	for name, token := range agent.Tokens {
		values["$"+name] = space.SanitizeDisplayText(token)
	}
	rows := layout.AgentRows
	if override, ok := layout.AgentRowsByAgent[agent.Agent]; ok {
		rows = override
	}
	// Keep Herdr's configured description rows, including per-harness overrides and reported tokens.
	var details []string
	if len(rows) > 1 {
		details, _ = renderSidebarRows(rows[1:], values, agent.AgentStatus, th)
	}
	th = themeOrDefault(th)
	muted := th.Muted
	prefix, styledPrefix := statePrefix(agent.AgentStatus, layout.StatusIndicators, th)
	heading := agentHarnessIcon(agent.Agent) + " " + space.SanitizeDisplayText(firstNonEmpty(tabLabel, agent.Label, agent.PaneID))
	location := space.SanitizeDisplayText(workspaceLabel)
	if dir := abbreviatedDirectory(firstNonEmpty(agent.ForegroundCwd, agent.Cwd)); dir != "" {
		location += " (" + space.SanitizeDisplayText(dir) + ")"
	}
	plain = []string{prefix + heading, location}
	display = []string{styledPrefix + th.Text + heading + "\x1b[0m", muted + location + "\x1b[0m"}
	if description := strings.Join(details, " "); description != "" {
		plain = append(plain, description)
		display = append(display, muted+description+"\x1b[0m")
	}
	return plain, display
}

func renderSidebarRows(layout [][]sidebarToken, values map[string]string, status string, th colorTheme) (plain, display []string) {
	for _, cells := range layout {
		var plainParts []string
		var displayParts []string
		for _, cell := range cells {
			value := values[cell.Name]
			if value == "" {
				continue
			}
			styled, hidden := applySidebarRules(cell, value)
			if hidden {
				continue
			}
			plainParts = append(plainParts, value)
			displayParts = append(displayParts, stylePlainToken(styled, value, status, th))
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

// filterItemsInto ranks matching items by fuzzy quality, then original order. It appends
// to dst (reusing its backing array) and reuses the search-target scratch slice so
// per-keystroke filtering does not reallocate.
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
