package picker

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/regutierrez/hseh/internal/config"
)

type sidebarToken struct {
	Name string
}

// sidebarLayout is the parsed Herdr sidebar configuration used by the picker. Spaces rows
// have a fixed sesh-style shape, so only the agent rows and status glyph style are read.
type sidebarLayout struct {
	AgentRows        [][]sidebarToken
	AgentRowsByAgent map[string][][]sidebarToken
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
		} `toml:"sidebar"`
	} `toml:"ui"`
}

func defaultSidebarLayout() sidebarLayout {
	return sidebarLayout{
		AgentRows:        tokensFromNames([][]string{{"agent"}}),
		AgentRowsByAgent: map[string][][]sidebarToken{},
		StatusIndicators: "dots",
	}
}

// readHerdrConfig decodes Herdr's config.toml into v. A missing file leaves v untouched
// and reports nothing; other read or parse failures are returned as footer messages.
func readHerdrConfig(configPath, scope string, v any) []string {
	if configPath == "" {
		configPath = config.HerdrConfigPath()
	}
	payload, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []string{fmt.Sprintf("hseh %s: read %s: %v", scope, configPath, err)}
	}
	if err := toml.Unmarshal(payload, v); err != nil {
		return []string{fmt.Sprintf("hseh %s: parse %s: %v", scope, configPath, err)}
	}
	return nil
}

func loadSidebarLayout(configPath string) (sidebarLayout, []string) {
	layout := defaultSidebarLayout()
	var file herdrSidebarFile
	if errs := readHerdrConfig(configPath, "sidebar", &file); errs != nil {
		return layout, errs
	}
	if file.UI.StatusIndicators == "symbols" || file.UI.StatusIndicators == "dots" {
		layout.StatusIndicators = file.UI.StatusIndicators
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

func tokensFromNames(rows [][]string) [][]sidebarToken {
	var out [][]sidebarToken
	for _, row := range rows {
		var cells []sidebarToken
		for _, name := range row {
			cells = append(cells, sidebarToken{Name: name})
		}
		out = append(out, cells)
	}
	return out
}

func parseSidebarTokenRows(raw [][]any) [][]sidebarToken {
	var rows [][]sidebarToken
	for _, row := range raw {
		var cells []sidebarToken
		for _, item := range row {
			if value, ok := item.(string); ok && value != "" {
				cells = append(cells, sidebarToken{Name: value})
			}
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	}
	return rows
}

// Glyphs mirror the settings preview in Herdr 0.9.0.
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
