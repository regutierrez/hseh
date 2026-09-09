package picker

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/lipgloss"
	"github.com/regutierrez/hseh/internal/config"
)

// SidebarToken is one configured sidebar cell, including optional inline style.
type SidebarToken struct {
	Name   string
	Fg     string
	Bold   bool
	Dim    bool
	Styled bool
}

// SidebarLayout is the parsed Herdr sidebar configuration used by the picker.
type SidebarLayout struct {
	SpaceRows        [][]SidebarToken
	AgentRows        [][]SidebarToken
	AgentRowsByAgent map[string][][]SidebarToken
	StatusIndicators string
}

type herdrSidebarFile struct {
	UI struct {
		StatusIndicators string `toml:"status_indicators"`
		Sidebar          struct {
			Agents struct {
				Rows        [][]any            `toml:"rows"`
				RowsByAgent map[string][][]any `toml:"rows_by_agent"`
			} `toml:"agents"`
			Spaces struct {
				Rows [][]any `toml:"rows"`
			} `toml:"spaces"`
		} `toml:"sidebar"`
	} `toml:"ui"`
}

func defaultAgentSidebarRows() [][]string {
	return [][]string{{"state_icon", "machine", "workspace", "tab"}, {"agent"}}
}

func defaultSpaceSidebarRows() [][]string {
	return [][]string{{"state_icon", "workspace"}, {"branch", "git_status"}}
}

func defaultSidebarLayout() SidebarLayout {
	return SidebarLayout{
		SpaceRows:        tokensFromNames(defaultSpaceSidebarRows()),
		AgentRows:        tokensFromNames(defaultAgentSidebarRows()),
		AgentRowsByAgent: map[string][][]SidebarToken{},
		StatusIndicators: "dots",
	}
}

// LoadSidebarLayout reads ui.sidebar rows, rows_by_agent, token styles, and status_indicators.
func LoadSidebarLayout(configPath string) (SidebarLayout, []string) {
	layout := defaultSidebarLayout()
	if configPath == "" {
		configPath = config.HerdrConfigPath()
	}
	payload, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return layout, nil
		}
		return layout, []string{fmt.Sprintf("hseh sidebar: read %s: %v", configPath, err)}
	}
	var file herdrSidebarFile
	if err := toml.Unmarshal(payload, &file); err != nil {
		return layout, []string{fmt.Sprintf("hseh sidebar: parse %s: %v", configPath, err)}
	}
	if file.UI.StatusIndicators == "symbols" || file.UI.StatusIndicators == "dots" {
		layout.StatusIndicators = file.UI.StatusIndicators
	}
	if parsed := parseSidebarTokenRows(file.UI.Sidebar.Spaces.Rows); len(parsed) > 0 {
		layout.SpaceRows = parsed
	}
	if parsed := parseSidebarTokenRows(file.UI.Sidebar.Agents.Rows); len(parsed) > 0 {
		layout.AgentRows = parsed
	}
	for agentID, raw := range file.UI.Sidebar.Agents.RowsByAgent {
		if parsed := parseSidebarTokenRows(raw); len(parsed) > 0 {
			layout.AgentRowsByAgent[agentID] = parsed
		}
	}
	return layout, nil
}

func tokensFromNames(rows [][]string) [][]SidebarToken {
	var out [][]SidebarToken
	for _, row := range rows {
		var cells []SidebarToken
		for _, name := range row {
			cells = append(cells, SidebarToken{Name: name})
		}
		out = append(out, cells)
	}
	return out
}

func parseSidebarTokenRows(raw [][]any) [][]SidebarToken {
	var rows [][]SidebarToken
	for _, row := range raw {
		var cells []SidebarToken
		for _, item := range row {
			switch value := item.(type) {
			case string:
				if value != "" {
					cells = append(cells, SidebarToken{Name: value})
				}
			case map[string]any:
				name, _ := value["token"].(string)
				if name == "" {
					continue
				}
				cell := SidebarToken{Name: name, Styled: true}
				if fg, ok := value["fg"].(string); ok {
					cell.Fg = fg
				}
				if bold, ok := value["bold"].(bool); ok {
					cell.Bold = bold
				}
				if dim, ok := value["dim"].(bool); ok {
					cell.Dim = dim
				}
				cells = append(cells, cell)
			}
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	}
	return rows
}

// Herdr 0.9.0 settings preview in the installed binary:
// color dots  ● ● ● ○ ·
// distinct symbols  × ◐ ✓ ○ ·
func stateIconGlyph(status, mode string) string {
	if mode == "symbols" {
		switch status {
		case "blocked":
			return "×"
		case "working":
			return "◐"
		case "done":
			return "✓"
		case "idle":
			return "○"
		default:
			return "·"
		}
	}
	switch status {
	case "blocked", "working", "done":
		return "●"
	case "idle":
		return "○"
	default:
		return "·"
	}
}

func stateIconColor(status string) string {
	switch status {
	case "blocked":
		return "91"
	case "working":
		return "33"
	case "done":
		return "36"
	case "idle":
		return "32"
	default:
		return "90"
	}
}

func stylePlainToken(token SidebarToken, plain, status string) string {
	if plain == "" {
		return ""
	}
	style := lipgloss.NewStyle()
	colored := false
	if token.Name == "state_icon" && token.Fg == "" {
		// Herdr's terminal theme uses the terminal palette, not CSS color names.
		plain = "\x1b[" + stateIconColor(status) + "m" + plain + "\x1b[0m"
	}
	if token.Styled && token.Fg != "" {
		style = style.Foreground(lipgloss.Color(token.Fg))
		colored = true
	}
	if token.Styled && token.Bold {
		style = style.Bold(true)
		colored = true
	}
	if token.Styled && token.Dim {
		style = style.Faint(true)
		colored = true
	}
	if colored {
		return style.Render(plain)
	}
	return plain
}
