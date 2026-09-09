package picker

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/regutierrez/hseh/internal/config"
)

// colorTheme holds SGR prefixes for picker chrome, resolved from Herdr's
// [theme] configuration. Sidebar row content keeps following SidebarLayout.
type colorTheme struct {
	Name      string
	Accent    string // borders, rail, divider
	TabActive string // full SGR prefix for the active tab label
	Mauve     string // prompt, caret, match highlight
	Muted     string // secondary chrome text (subtext0)
	Overlay   string // counts, rules (overlay1)
	Yellow    string // loading / empty copy
	Red       string // errors
}

// herdrThemePalettes mirrors the tokens Herdr's built-in palettes expose
// (herdrdev/herdr src/app/state.rs). Only the tokens the picker consumes are listed.
var herdrThemePalettes = map[string]map[string]string{
	"catppuccin":       {"accent": "#89b4fa", "mauve": "#cba6f7", "subtext0": "#a6adc8", "yellow": "#f9e2af", "red": "#f38ba8", "overlay1": "#7f849c"},
	"catppuccin-latte": {"accent": "#1e66f5", "mauve": "#8839ef", "subtext0": "#6c6f85", "yellow": "#df8e1d", "red": "#d20f39", "overlay1": "#8c8fa1"},
	"tokyo-night":      {"accent": "#7aa2f7", "mauve": "#bb9af7", "subtext0": "#a9b1d6", "yellow": "#e0af68", "red": "#f7768e", "overlay1": "#697196"},
	"tokyo-night-day":  {"accent": "#2e7de9", "mauve": "#7847bd", "subtext0": "#6172b0", "yellow": "#8c6c3e", "red": "#f52a65", "overlay1": "#68709a"},
	"dracula":          {"accent": "#bd93f9", "mauve": "#ff79c6", "subtext0": "#d2d2dc", "yellow": "#f1fa8c", "red": "#ff5555", "overlay1": "#828cb4"},
	"nord":             {"accent": "#88c0d0", "mauve": "#b48ead", "subtext0": "#d8dee9", "yellow": "#ebcb8b", "red": "#bf616a", "overlay1": "#646e82"},
	"gruvbox":          {"accent": "#d79921", "mauve": "#d3869b", "subtext0": "#d5c4a1", "yellow": "#fabd2f", "red": "#fb4934", "overlay1": "#a89984"},
	"gruvbox-light":    {"accent": "#076678", "mauve": "#8f3f71", "subtext0": "#504945", "yellow": "#b57614", "red": "#9d0006", "overlay1": "#7c6f64"},
	"one-dark":         {"accent": "#61afef", "mauve": "#c678dd", "subtext0": "#969ca8", "yellow": "#e5c07b", "red": "#e06c75", "overlay1": "#737a87"},
	"one-light":        {"accent": "#4078f2", "mauve": "#a626a4", "subtext0": "#686b77", "yellow": "#c18401", "red": "#e45649", "overlay1": "#686b77"},
	"solarized":        {"accent": "#268bd2", "mauve": "#d33682", "subtext0": "#839496", "yellow": "#b58900", "red": "#dc322f", "overlay1": "#657b83"},
	"solarized-light":  {"accent": "#268bd2", "mauve": "#d33682", "subtext0": "#839496", "yellow": "#b58900", "red": "#dc322f", "overlay1": "#586e75"},
	"kanagawa":         {"accent": "#7e9cd8", "mauve": "#957fb8", "subtext0": "#c8c3aa", "yellow": "#c0a36e", "red": "#c34043", "overlay1": "#87867d"},
	"kanagawa-lotus":   {"accent": "#4d699b", "mauve": "#624c83", "subtext0": "#43436c", "yellow": "#77713f", "red": "#c84053", "overlay1": "#8a8980"},
	"rose-pine":        {"accent": "#c4a7e7", "mauve": "#c4a7e7", "subtext0": "#c8c5dc", "yellow": "#f6c177", "red": "#eb6f92", "overlay1": "#908caa"},
	"rose-pine-dawn":   {"accent": "#907aa9", "mauve": "#907aa9", "subtext0": "#797593", "yellow": "#ea9d34", "red": "#b4637a", "overlay1": "#797593"},
	"vesper":           {"accent": "#ffc799", "mauve": "#ffd1a8", "subtext0": "#a0a0a0", "yellow": "#ffc799", "red": "#ff8080", "overlay1": "#7e7e7e"},
}

var herdrThemeAliases = map[string]string{
	"catppuccin": "catppuccin", "catppuccin-mocha": "catppuccin", "mocha": "catppuccin",
	"catppuccin-latte": "catppuccin-latte", "latte": "catppuccin-latte", "light": "catppuccin-latte",
	"terminal":    "terminal",
	"tokyo-night": "tokyo-night", "tokyonight": "tokyo-night",
	"tokyo-night-day": "tokyo-night-day", "tokyo-day": "tokyo-night-day", "tokyonight-day": "tokyo-night-day",
	"dracula": "dracula", "nord": "nord",
	"gruvbox": "gruvbox", "gruvbox-dark": "gruvbox", "gruvbox-light": "gruvbox-light",
	"one-dark": "one-dark", "onedark": "one-dark", "one-light": "one-light", "onelight": "one-light",
	"solarized": "solarized", "solarized-dark": "solarized", "solarized-light": "solarized-light",
	"kanagawa": "kanagawa", "kanagawa-lotus": "kanagawa-lotus", "lotus": "kanagawa-lotus",
	"rose-pine": "rose-pine", "rosepine": "rose-pine",
	"rose-pine-dawn": "rose-pine-dawn", "rosepine-dawn": "rose-pine-dawn", "dawn": "rose-pine-dawn",
	"vesper": "vesper",
}

const defaultHerdrThemeName = "catppuccin"

var hexColorPattern = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

type herdrThemeFile struct {
	Theme struct {
		Name   string            `toml:"name"`
		Custom map[string]string `toml:"custom"`
	} `toml:"theme"`
}

// loadTheme resolves the picker chrome palette from Herdr's config.
// Missing or unparsable config falls back to Herdr's default palette.
func loadTheme(configPath string) (colorTheme, []string) {
	if configPath == "" {
		configPath = config.HerdrConfigPath()
	}
	payload, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return resolveTheme("", nil), nil
		}
		return resolveTheme("", nil), []string{fmt.Sprintf("hseh theme: read %s: %v", configPath, err)}
	}
	var file herdrThemeFile
	if err := toml.Unmarshal(payload, &file); err != nil {
		return resolveTheme("", nil), []string{fmt.Sprintf("hseh theme: parse %s: %v", configPath, err)}
	}
	return resolveTheme(file.Theme.Name, file.Theme.Custom), nil
}

func canonicalHerdrThemeName(raw string) string {
	name := strings.ToLower(strings.TrimSpace(raw))
	name = strings.ReplaceAll(name, " ", "-")
	return strings.ReplaceAll(name, "_", "-")
}

func resolveTheme(name string, custom map[string]string) colorTheme {
	key := herdrThemeAliases[canonicalHerdrThemeName(name)]
	if key == "" {
		key = defaultHerdrThemeName
	}
	if key == "terminal" && len(custom) == 0 {
		return terminalTheme()
	}
	tokens := map[string]string{}
	if key != "terminal" {
		for token, value := range herdrThemePalettes[key] {
			tokens[token] = value
		}
	} else {
		for token, value := range herdrThemePalettes[defaultHerdrThemeName] {
			tokens[token] = value
		}
	}
	for token, value := range custom {
		if hexColorPattern.MatchString(value) {
			tokens[token] = value
		}
	}
	return colorTheme{
		Name:      key,
		Accent:    hexSGR(tokens["accent"], false),
		TabActive: hexSGR(tokens["accent"], true) + "\x1b[30m",
		Mauve:     hexSGR(tokens["mauve"], false),
		Muted:     hexSGR(tokens["subtext0"], false),
		Overlay:   hexSGR(tokens["overlay1"], false),
		Yellow:    hexSGR(tokens["yellow"], false),
		Red:       hexSGR(tokens["red"], false),
	}
}

// terminalTheme uses the terminal's own 16-color palette, matching Herdr's "terminal" theme.
func terminalTheme() colorTheme {
	return colorTheme{
		Name:      "terminal",
		Accent:    "\x1b[34m",
		TabActive: "\x1b[7;34m",
		Mauve:     "\x1b[35m",
		Muted:     "\x1b[90m",
		Overlay:   "\x1b[90m",
		Yellow:    "\x1b[33m",
		Red:       "\x1b[31m",
	}
}

func defaultTheme() colorTheme {
	return resolveTheme(defaultHerdrThemeName, nil)
}

func hexSGR(hex string, background bool) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return ""
	}
	value, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return ""
	}
	r, g, b := (value>>16)&0xff, (value>>8)&0xff, value&0xff
	plane := "38"
	if background {
		plane = "48"
	}
	return fmt.Sprintf("\x1b[%s;2;%d;%d;%dm", plane, r, g, b)
}

// th returns the model theme, defaulting to Herdr's default palette for zero-value models.
func (m model) th() colorTheme {
	if m.theme.Name == "" {
		return defaultTheme()
	}
	return m.theme
}
