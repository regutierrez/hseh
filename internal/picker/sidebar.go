package picker

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/lipgloss"
	"github.com/regutierrez/hseh/internal/config"
)

// sidebarToken is one configured sidebar cell, including optional inline style.
type sidebarToken struct {
	Name   string
	Fg     string
	Bold   bool
	Dim    bool
	Styled bool
	Rules  []sidebarRule
}

// sidebarRule is one Herdr text-token rule (equals/contains/starts_with/gt/lt).
type sidebarRule struct {
	kind       string
	text       string
	number     float64
	ignoreCase bool
	fg         string
	bold       *bool
	dim        *bool
	hide       *bool
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

// loadSidebarLayout reads ui.sidebar agent rows, rows_by_agent, token styles, and status_indicators.
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
			switch value := item.(type) {
			case string:
				if value != "" {
					cells = append(cells, sidebarToken{Name: value})
				}
			case map[string]any:
				name, _ := value["token"].(string)
				if name == "" {
					continue
				}
				cell := sidebarToken{Name: name, Styled: true}
				if fg, ok := value["fg"].(string); ok {
					cell.Fg = fg
				}
				if bold, ok := value["bold"].(bool); ok {
					cell.Bold = bold
				}
				if dim, ok := value["dim"].(bool); ok {
					cell.Dim = dim
				}
				if rawRules, ok := value["rules"].([]any); ok {
					cell.Rules = parseSidebarRules(rawRules)
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

// Glyphs copy herdr status_icon: dots "● ● ● ○ ·", symbols "× ◐ ✓ ○ ·".
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

func stateIconSGR(status string, th colorTheme) string {
	th = themeOrDefault(th)
	switch status {
	case "blocked":
		return th.Red
	case "working":
		return th.Yellow
	case "done":
		return th.Blue
	case "idle":
		return th.Green
	default:
		return th.Overlay
	}
}

func parseSidebarRules(raw []any) []sidebarRule {
	var rules []sidebarRule
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if rule, ok := parseSidebarRule(entry); ok {
			rules = append(rules, rule)
		}
	}
	return rules
}

func parseSidebarRule(raw map[string]any) (sidebarRule, bool) {
	var rule sidebarRule
	conditions := 0
	if value, ok := raw["equals"].(string); ok {
		rule.kind, rule.text, conditions = "equals", value, conditions+1
	}
	if value, ok := raw["contains"].(string); ok {
		rule.kind, rule.text, conditions = "contains", value, conditions+1
	}
	if value, ok := raw["starts_with"].(string); ok {
		rule.kind, rule.text, conditions = "starts_with", value, conditions+1
	}
	if value, ok := floatFromTOML(raw["gt"]); ok {
		rule.kind, rule.number, conditions = "gt", value, conditions+1
	}
	if value, ok := floatFromTOML(raw["lt"]); ok {
		rule.kind, rule.number, conditions = "lt", value, conditions+1
	}
	if conditions != 1 {
		return sidebarRule{}, false
	}
	if ignore, ok := raw["ignore_case"].(bool); ok {
		if rule.kind == "gt" || rule.kind == "lt" {
			return sidebarRule{}, false
		}
		rule.ignoreCase = ignore
	}
	if fg, ok := raw["fg"].(string); ok {
		rule.fg = fg
	}
	if bold, ok := raw["bold"].(bool); ok {
		rule.bold = &bold
	}
	if dim, ok := raw["dim"].(bool); ok {
		rule.dim = &dim
	}
	if hide, ok := raw["hide"].(bool); ok {
		rule.hide = &hide
	}
	return rule, true
}

func floatFromTOML(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return n, true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}

func (r sidebarRule) matches(value string, numeric *optionalFloat) bool {
	switch r.kind {
	case "equals":
		if r.ignoreCase {
			return asciiEqualFold(value, r.text)
		}
		return value == r.text
	case "contains":
		if r.ignoreCase {
			return asciiContainsFold(value, r.text)
		}
		return strings.Contains(value, r.text)
	case "starts_with":
		if r.ignoreCase {
			return asciiHasPrefixFold(value, r.text)
		}
		return strings.HasPrefix(value, r.text)
	case "gt", "lt":
		n, ok := numeric.parse(value)
		if !ok {
			return false
		}
		if r.kind == "gt" {
			return n > r.number
		}
		return n < r.number
	default:
		return false
	}
}

type optionalFloat struct {
	parsed bool
	ok     bool
	value  float64
}

func (o *optionalFloat) parse(value string) (float64, bool) {
	if o.parsed {
		return o.value, o.ok
	}
	o.parsed = true
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	o.ok = true
	o.value = n
	return n, true
}

func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if asciiLower(a[i]) != asciiLower(b[i]) {
			return false
		}
	}
	return true
}

func asciiHasPrefixFold(value, prefix string) bool {
	if len(prefix) > len(value) {
		return false
	}
	return asciiEqualFold(value[:len(prefix)], prefix)
}

func asciiContainsFold(value, part string) bool {
	if part == "" {
		return true
	}
	if len(part) > len(value) {
		return false
	}
	for i := 0; i+len(part) <= len(value); i++ {
		if asciiEqualFold(value[i:i+len(part)], part) {
			return true
		}
	}
	return false
}

func asciiLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func applySidebarRules(token sidebarToken, value string) (sidebarToken, bool) {
	if token.Name == "state_icon" || len(token.Rules) == 0 {
		return token, false
	}
	var numeric optionalFloat
	for _, rule := range token.Rules {
		if !rule.matches(value, &numeric) {
			continue
		}
		if rule.hide != nil && *rule.hide {
			return token, true
		}
		if rule.fg != "" {
			token.Fg = rule.fg
			token.Styled = true
		}
		if rule.bold != nil {
			token.Bold = *rule.bold
			token.Styled = true
		}
		if rule.dim != nil {
			token.Dim = *rule.dim
			token.Styled = true
		}
		return token, false
	}
	return token, false
}

func stylePlainToken(token sidebarToken, plain, status string, th colorTheme) string {
	if plain == "" {
		return ""
	}
	style := lipgloss.NewStyle()
	colored := false
	if token.Name == "state_icon" && token.Fg == "" {
		plain = stateIconSGR(status, th) + plain + "\x1b[0m"
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
