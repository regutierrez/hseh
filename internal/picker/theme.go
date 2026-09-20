package picker

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// colorTheme is the Herdr Palette tokens we paint, as foreground SGR (empty =
// Reset). Backgrounds are derived with toBgSGR. Values come from Herdr's
// [theme] on top of the built-in table copied from state.rs.
type colorTheme struct {
	Name        string
	Accent      string
	TabActive   string
	Mauve       string
	Muted       string // subtext0
	Overlay     string // overlay1
	Yellow      string
	Red         string
	Green       string
	Blue        string
	Text        string
	PanelBg     string
	SidebarBg   string
	ActiveRowBg string
	SelectionBg string
	Surface0    string
}

// herdrThemePalettes copies Herdr built-in hex values (state.rs) for tokens we
// paint. "reset" is Color::Reset.
var herdrThemePalettes = map[string]map[string]string{
	"catppuccin": {
		"accent": "#89b4fa", "panel_bg": "#181825", "sidebar_bg": "reset",
		"active_row_bg": "#1e1e2e", "selection_bg": "#313244", "surface0": "#313244",
		"overlay1": "#7f849c", "text": "#cdd6f4", "subtext0": "#a6adc8",
		"mauve": "#cba6f7", "green": "#a6e3a1", "yellow": "#f9e2af", "red": "#f38ba8", "blue": "#89b4fa",
	},
	"catppuccin-latte": {
		"accent": "#1e66f5", "panel_bg": "#eff1f5", "sidebar_bg": "reset",
		"active_row_bg": "#e6e9ef", "selection_bg": "#bdd0f5", "surface0": "#ccd0da",
		"overlay1": "#8c8fa1", "text": "#4c4f69", "subtext0": "#6c6f85",
		"mauve": "#8839ef", "green": "#40a02b", "yellow": "#df8e1d", "red": "#d20f39", "blue": "#1e66f5",
	},
	"tokyo-night": {
		"accent": "#7aa2f7", "panel_bg": "#1a1b26", "sidebar_bg": "reset",
		"active_row_bg": "#232636", "selection_bg": "#2d3650", "surface0": "#24283b",
		"overlay1": "#697196", "text": "#c0caf5", "subtext0": "#a9b1d6",
		"mauve": "#bb9af7", "green": "#9ece6a", "yellow": "#e0af68", "red": "#f7768e", "blue": "#7aa2f7",
	},
	"tokyo-night-day": {
		"accent": "#2e7de9", "panel_bg": "#e1e2e7", "sidebar_bg": "reset",
		"active_row_bg": "#d2d3da", "selection_bg": "#b6cae7", "surface0": "#c4c8da",
		"overlay1": "#68709a", "text": "#3760bf", "subtext0": "#6172b0",
		"mauve": "#7847bd", "green": "#587539", "yellow": "#8c6c3e", "red": "#f52a65", "blue": "#2e7de9",
	},
	"dracula": {
		"accent": "#bd93f9", "panel_bg": "#282a36", "sidebar_bg": "reset",
		"active_row_bg": "#373c52", "selection_bg": "#463f5d", "surface0": "#44475a",
		"overlay1": "#828cb4", "text": "#f8f8f2", "subtext0": "#d2d2dc",
		"mauve": "#ff79c6", "green": "#50fa7b", "yellow": "#f1fa8c", "red": "#ff5555", "blue": "#8be9fd",
	},
	"nord": {
		"accent": "#88c0d0", "panel_bg": "#2e3440", "sidebar_bg": "reset",
		"active_row_bg": "#434c5e", "selection_bg": "#40505d", "surface0": "#3b4252",
		"overlay1": "#646e82", "text": "#eceff4", "subtext0": "#d8dee9",
		"mauve": "#b48ead", "green": "#a3be8c", "yellow": "#ebcb8b", "red": "#bf616a", "blue": "#81a1c1",
	},
	"gruvbox": {
		"accent": "#d79921", "panel_bg": "#282828", "sidebar_bg": "reset",
		"active_row_bg": "#323130", "selection_bg": "#4b3f27", "surface0": "#3c3836",
		"overlay1": "#a89984", "text": "#ebdbb2", "subtext0": "#d5c4a1",
		"mauve": "#d3869b", "green": "#b8bb26", "yellow": "#fabd2f", "red": "#fb4934", "blue": "#83a598",
	},
	"gruvbox-light": {
		"accent": "#076678", "panel_bg": "#fbf1c7", "sidebar_bg": "reset",
		"active_row_bg": "#f2e5bc", "selection_bg": "#ebdbb2", "surface0": "#ebdbb2",
		"overlay1": "#7c6f64", "text": "#3c3836", "subtext0": "#504945",
		"mauve": "#8f3f71", "green": "#79740e", "yellow": "#b57614", "red": "#9d0006", "blue": "#076678",
	},
	"one-dark": {
		"accent": "#61afef", "panel_bg": "#282c34", "sidebar_bg": "reset",
		"active_row_bg": "#313640", "selection_bg": "#334659", "surface0": "#2c313a",
		"overlay1": "#737a87", "text": "#abb2bf", "subtext0": "#969ca8",
		"mauve": "#c678dd", "green": "#98c379", "yellow": "#e5c07b", "red": "#e06c75", "blue": "#61afef",
	},
	"one-light": {
		"accent": "#4078f2", "panel_bg": "#fafafa", "sidebar_bg": "reset",
		"active_row_bg": "#d8dbe2", "selection_bg": "#cddbf8", "surface0": "#f0f0f1",
		"overlay1": "#686b77", "text": "#383a42", "subtext0": "#686b77",
		"mauve": "#a626a4", "green": "#50a14f", "yellow": "#c18401", "red": "#e45649", "blue": "#4078f2",
	},
	"solarized": {
		"accent": "#268bd2", "panel_bg": "#002b36", "sidebar_bg": "reset",
		"active_row_bg": "#164b57", "selection_bg": "#083e55", "surface0": "#073642",
		"overlay1": "#657b83", "text": "#93a1a1", "subtext0": "#839496",
		"mauve": "#d33682", "green": "#859900", "yellow": "#b58900", "red": "#dc322f", "blue": "#268bd2",
	},
	"solarized-light": {
		"accent": "#268bd2", "panel_bg": "#fdf6e3", "sidebar_bg": "reset",
		"active_row_bg": "#eee8d5", "selection_bg": "#c9dcdf", "surface0": "#eee8d5",
		"overlay1": "#586e75", "text": "#657b83", "subtext0": "#839496",
		"mauve": "#d33682", "green": "#859900", "yellow": "#b58900", "red": "#dc322f", "blue": "#268bd2",
	},
	"kanagawa": {
		"accent": "#7e9cd8", "panel_bg": "#1f1f28", "sidebar_bg": "reset",
		"active_row_bg": "#363646", "selection_bg": "#32384b", "surface0": "#2a2a37",
		"overlay1": "#87867d", "text": "#dcd7ba", "subtext0": "#c8c3aa",
		"mauve": "#957fb8", "green": "#76946a", "yellow": "#c0a36e", "red": "#c34043", "blue": "#7e9cd8",
	},
	"kanagawa-lotus": {
		"accent": "#4d699b", "panel_bg": "#f2ecbc", "sidebar_bg": "reset",
		"active_row_bg": "#d5cea3", "selection_bg": "#dcd5ac", "surface0": "#dcd5ac",
		"overlay1": "#8a8980", "text": "#545464", "subtext0": "#43436c",
		"mauve": "#624c83", "green": "#6f894e", "yellow": "#77713f", "red": "#c84053", "blue": "#4d699b",
	},
	"rose-pine": {
		"accent": "#c4a7e7", "panel_bg": "#191724", "sidebar_bg": "reset",
		"active_row_bg": "#26233a", "selection_bg": "#3b344b", "surface0": "#1f1d2e",
		"overlay1": "#908caa", "text": "#e0def4", "subtext0": "#c8c5dc",
		"mauve": "#c4a7e7", "green": "#31748f", "yellow": "#f6c177", "red": "#eb6f92", "blue": "#31748f",
	},
	"rose-pine-dawn": {
		"accent": "#907aa9", "panel_bg": "#faf4ed", "sidebar_bg": "reset",
		"active_row_bg": "#e3d9cf", "selection_bg": "#f2e9e1", "surface0": "#f2e9e1",
		"overlay1": "#797593", "text": "#464261", "subtext0": "#797593",
		"mauve": "#907aa9", "green": "#286983", "yellow": "#ea9d34", "red": "#b4637a", "blue": "#286983",
	},
	"vesper": {
		"accent": "#ffc799", "panel_bg": "#1a1a1a", "sidebar_bg": "reset",
		"active_row_bg": "#101010", "selection_bg": "#232323", "surface0": "#232323",
		"overlay1": "#7e7e7e", "text": "#ffffff", "subtext0": "#a0a0a0",
		"mauve": "#ffd1a8", "green": "#99ffe4", "yellow": "#ffc799", "red": "#ff8080", "blue": "#b0b0b0",
	},
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

// appearanceKind is the host light/dark result used when [theme].auto_switch is on.
type appearanceKind int

const (
	appearanceDark appearanceKind = iota
	appearanceLight
)

// appearanceEnv forces light or dark for auto_switch (and tests). When unset,
// hostAppearance reads COLORFGBG's background index: 7 and 15 are treated as
// light (white / bright white), everything else as dark. Missing COLORFGBG
// defaults to dark, matching Herdr's unwrap_or(Dark).
const appearanceEnv = "HSEH_APPEARANCE"

// appearanceForTheme is the auto_switch probe. Tests replace it.
var appearanceForTheme = hostAppearance

type themeSelection struct {
	Name        string
	AutoSwitch  bool
	LightName   string
	DarkName    string
	Custom      map[string]string
	CustomLight map[string]string
	CustomDark  map[string]string
}

type herdrThemeFile struct {
	Theme herdrThemeSection `toml:"theme"`
}

type herdrThemeSection struct {
	Name       string         `toml:"name"`
	AutoSwitch bool           `toml:"auto_switch"`
	LightName  string         `toml:"light_name"`
	DarkName   string         `toml:"dark_name"`
	Custom     map[string]any `toml:"custom"`
}

func (s herdrThemeSection) selection() themeSelection {
	flat, light, dark := splitThemeCustom(s.Custom)
	return themeSelection{
		Name:        s.Name,
		AutoSwitch:  s.AutoSwitch,
		LightName:   s.LightName,
		DarkName:    s.DarkName,
		Custom:      flat,
		CustomLight: light,
		CustomDark:  dark,
	}
}

func splitThemeCustom(raw map[string]any) (flat, light, dark map[string]string) {
	flat, light, dark = map[string]string{}, map[string]string{}, map[string]string{}
	for key, value := range raw {
		switch key {
		case "light":
			if nested, ok := value.(map[string]any); ok {
				light = stringColorMap(nested)
				continue
			}
		case "dark":
			if nested, ok := value.(map[string]any); ok {
				dark = stringColorMap(nested)
				continue
			}
		}
		if text, ok := value.(string); ok {
			flat[key] = text
		}
	}
	return flat, light, dark
}

func stringColorMap(raw map[string]any) map[string]string {
	out := map[string]string{}
	for key, value := range raw {
		if text, ok := value.(string); ok {
			out[key] = text
		}
	}
	return out
}

// loadTheme resolves the picker palette from Herdr's config.
// Missing or unparsable config falls back to Herdr's default palette.
func loadTheme() (colorTheme, []string) {
	return loadThemeFromPath("")
}

func loadThemeFromPath(configPath string) (colorTheme, []string) {
	var file herdrThemeFile
	if errs := readHerdrConfig(configPath, "theme", &file); errs != nil {
		return resolveThemeConfig(themeSelection{}, appearanceForTheme()), errs
	}
	return resolveThemeConfig(file.Theme.selection(), appearanceForTheme()), nil
}

func canonicalHerdrThemeName(raw string) string {
	name := strings.ToLower(strings.TrimSpace(raw))
	name = strings.ReplaceAll(name, " ", "-")
	return strings.ReplaceAll(name, "_", "-")
}

func resolveTheme(name string, custom map[string]string) colorTheme {
	return resolveThemeConfig(themeSelection{Name: name, Custom: custom}, appearanceDark)
}

func resolveThemeConfig(sel themeSelection, appearance appearanceKind) colorTheme {
	name := sel.Name
	modeCustom := map[string]string(nil)
	fallback := defaultHerdrThemeName
	if sel.AutoSwitch {
		darkName, lightName := siblingThemeNames(sel.Name)
		if sel.DarkName != "" {
			darkName = sel.DarkName
		}
		if sel.LightName != "" {
			lightName = sel.LightName
		}
		if appearance == appearanceLight {
			name = lightName
			fallback = "catppuccin-latte"
			modeCustom = sel.CustomLight
		} else {
			name = darkName
			modeCustom = sel.CustomDark
		}
	}
	key := herdrThemeAliases[canonicalHerdrThemeName(name)]
	if key == "" {
		key = herdrThemeAliases[canonicalHerdrThemeName(fallback)]
	}
	if key == "" {
		key = defaultHerdrThemeName
	}
	tokens := baseThemeTokens(key)
	applyColorOverrides(tokens, sel.Custom)
	applyColorOverrides(tokens, modeCustom)
	return colorThemeFromTokens(key, tokens)
}

func siblingThemeNames(name string) (dark, light string) {
	switch canonicalHerdrThemeName(name) {
	case "catppuccin", "catppuccin-mocha", "catppuccin-latte", "latte", "light":
		return "catppuccin", "catppuccin-latte"
	case "tokyo-night", "tokyonight", "tokyo-night-day", "tokyo-day", "tokyonight-day":
		return "tokyo-night", "tokyo-night-day"
	case "gruvbox", "gruvbox-dark", "gruvbox-light":
		return "gruvbox", "gruvbox-light"
	case "one-dark", "onedark", "one-light", "onelight":
		return "one-dark", "one-light"
	case "solarized", "solarized-dark", "solarized-light":
		return "solarized", "solarized-light"
	case "kanagawa", "kanagawa-lotus", "lotus":
		return "kanagawa", "kanagawa-lotus"
	case "rose-pine", "rosepine", "rose-pine-dawn", "rosepine-dawn", "dawn":
		return "rose-pine", "rose-pine-dawn"
	default:
		return name, name
	}
}

func baseThemeTokens(key string) map[string]string {
	if key == "terminal" {
		return terminalTokenSGRs()
	}
	tokens := map[string]string{}
	for token, value := range herdrThemePalettes[key] {
		if sgr, ok := parseColorSGR(value); ok {
			tokens[token] = sgr
		}
	}
	return tokens
}

func applyColorOverrides(tokens map[string]string, custom map[string]string) {
	for token, value := range custom {
		if sgr, ok := parseColorSGR(value); ok {
			tokens[token] = sgr
		}
	}
}

func colorThemeFromTokens(name string, tokens map[string]string) colorTheme {
	accent := tokens["accent"]
	text := tokens["text"]
	return colorTheme{
		Name:        name,
		Accent:      accent,
		TabActive:   tabActiveSGR(accent, text),
		Mauve:       tokens["mauve"],
		Muted:       tokens["subtext0"],
		Overlay:     tokens["overlay1"],
		Yellow:      tokens["yellow"],
		Red:         tokens["red"],
		Green:       tokens["green"],
		Blue:        tokens["blue"],
		Text:        text,
		PanelBg:     tokens["panel_bg"],
		SidebarBg:   tokens["sidebar_bg"],
		ActiveRowBg: tokens["active_row_bg"],
		SelectionBg: tokens["selection_bg"],
		Surface0:    tokens["surface0"],
	}
}

func tabActiveSGR(accent, text string) string {
	bg := toBgSGR(accent)
	if bg == "" {
		if text == "" {
			return "\x1b[7m"
		}
		return "\x1b[7m" + text
	}
	return bg + text
}

func terminalTokenSGRs() map[string]string {
	return map[string]string{
		"accent":        "\x1b[34m",
		"panel_bg":      "",
		"sidebar_bg":    "",
		"active_row_bg": "\x1b[90m",
		"selection_bg":  "",
		"surface0":      "",
		"overlay1":      "\x1b[97m",
		"text":          "",
		"subtext0":      "\x1b[37m",
		"mauve":         "\x1b[37m",
		"green":         "\x1b[32m",
		"yellow":        "\x1b[33m",
		"red":           "\x1b[91m",
		"blue":          "\x1b[34m",
	}
}

// terminalTheme uses the terminal's own 16-color palette, matching Herdr's "terminal" theme.
func terminalTheme() colorTheme {
	return colorThemeFromTokens("terminal", terminalTokenSGRs())
}

func hexSGR(hex string) string {
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
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

var namedColorSGR = map[string]string{
	"black":        "\x1b[30m",
	"red":          "\x1b[31m",
	"green":        "\x1b[32m",
	"yellow":       "\x1b[33m",
	"blue":         "\x1b[34m",
	"magenta":      "\x1b[35m",
	"purple":       "\x1b[35m",
	"cyan":         "\x1b[36m",
	"gray":         "\x1b[37m",
	"grey":         "\x1b[37m",
	"darkgray":     "\x1b[90m",
	"darkgrey":     "\x1b[90m",
	"lightred":     "\x1b[91m",
	"lightgreen":   "\x1b[92m",
	"lightyellow":  "\x1b[93m",
	"lightblue":    "\x1b[94m",
	"lightmagenta": "\x1b[95m",
	"lightcyan":    "\x1b[96m",
	"white":        "\x1b[97m",
}

func parseColorSGR(value string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(value))
	switch s {
	case "reset", "default", "none", "transparent":
		return "", true
	}
	if strings.HasPrefix(s, "#") {
		if out := hexSGR(s); out != "" {
			return out, true
		}
		return "", false
	}
	if r, g, b, ok := parseRGBFunc(s); ok {
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b), true
	}
	if sgr, ok := namedColorSGR[s]; ok {
		return sgr, true
	}
	return "", false
}

func parseRGBFunc(s string) (r, g, b int, ok bool) {
	if !strings.HasPrefix(s, "rgb(") || !strings.HasSuffix(s, ")") {
		return 0, 0, 0, false
	}
	parts := strings.Split(s[4:len(s)-1], ",")
	if len(parts) != 3 && len(parts) != 4 {
		return 0, 0, 0, false
	}
	parse := func(part string) (int, bool) {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 0 || n > 255 {
			return 0, false
		}
		return n, true
	}
	r, okR := parse(parts[0])
	g, okG := parse(parts[1])
	b, okB := parse(parts[2])
	return r, g, b, okR && okG && okB
}

func hostAppearance() appearanceKind {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(appearanceEnv))) {
	case "light":
		return appearanceLight
	case "dark":
		return appearanceDark
	}
	return appearanceFromCOLORFGBG(os.Getenv("COLORFGBG"))
}

func appearanceFromCOLORFGBG(value string) appearanceKind {
	value = strings.TrimSpace(value)
	if value == "" {
		return appearanceDark
	}
	parts := strings.Split(value, ";")
	bg := strings.TrimSpace(parts[len(parts)-1])
	n, err := strconv.Atoi(bg)
	if err != nil {
		return appearanceDark
	}
	if n == 7 || n == 15 {
		return appearanceLight
	}
	return appearanceDark
}

func themeOrDefault(th colorTheme) colorTheme {
	if th.Name == "" {
		return resolveTheme(defaultHerdrThemeName, nil)
	}
	return th
}

func (m model) th() colorTheme {
	return themeOrDefault(m.theme)
}

// toBgSGR turns a stored foreground SGR into the matching background SGR.
func toBgSGR(fg string) string {
	if fg == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(fg, "\x1b[38;"); ok {
		return "\x1b[48;" + rest
	}
	var code int
	if _, err := fmt.Sscanf(fg, "\x1b[%dm", &code); err == nil {
		switch {
		case code >= 30 && code <= 37:
			return fmt.Sprintf("\x1b[%dm", code+10)
		case code >= 90 && code <= 97:
			return fmt.Sprintf("\x1b[%dm", code+10)
		}
	}
	return ""
}

func (th colorTheme) panelFill() string { return toBgSGR(th.PanelBg) }

func (th colorTheme) listFill() string {
	if bg := toBgSGR(th.SidebarBg); bg != "" {
		return bg
	}
	return toBgSGR(th.PanelBg)
}

func (th colorTheme) selectedFill() string {
	if bg := toBgSGR(th.SelectionBg); bg != "" {
		return bg
	}
	return toBgSGR(th.ActiveRowBg)
}

func (th colorTheme) searchFill() string {
	if bg := toBgSGR(th.Surface0); bg != "" {
		return bg
	}
	return toBgSGR(th.PanelBg)
}

func replayAfterReset(line, sgr string) string {
	if sgr == "" {
		return line
	}
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+sgr)
	return strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+sgr)
}

// applySurface pads a row and paints a Herdr background token through resets.
func applySurface(line, bg string, width int) string {
	line = padDisplayWidth(line, width)
	if bg == "" {
		return line
	}
	return bg + replayAfterReset(line, bg) + "\x1b[0m"
}
