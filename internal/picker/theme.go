package picker

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// colorTheme holds SGR prefixes for picker chrome and row accents, resolved
// from Herdr's [theme] configuration.
type colorTheme struct {
	Name      string
	Accent    string // borders, rail, divider
	TabActive string // full SGR prefix for the active tab label
	Mauve     string // prompt, caret, match highlight
	Muted     string // secondary chrome and row text (subtext0)
	Overlay   string // counts, rules, unknown status (overlay1)
	Yellow    string // loading / empty copy, working status
	Red       string // errors, blocked status
	Green     string // idle status
	Blue      string // done status
}

// herdrThemePalettes mirrors the tokens Herdr 0.9.0's built-in palettes expose
// (herdrdev/herdr src/app/state.rs). A later Herdr palette change will drift
// until these hex values are copied again (or Herdr exposes the live palette).
var herdrThemePalettes = map[string]map[string]string{
	"catppuccin":       {"accent": "#89b4fa", "mauve": "#cba6f7", "subtext0": "#a6adc8", "yellow": "#f9e2af", "red": "#f38ba8", "overlay1": "#7f849c", "green": "#a6e3a1", "blue": "#89b4fa"},
	"catppuccin-latte": {"accent": "#1e66f5", "mauve": "#8839ef", "subtext0": "#6c6f85", "yellow": "#df8e1d", "red": "#d20f39", "overlay1": "#8c8fa1", "green": "#40a02b", "blue": "#1e66f5"},
	"tokyo-night":      {"accent": "#7aa2f7", "mauve": "#bb9af7", "subtext0": "#a9b1d6", "yellow": "#e0af68", "red": "#f7768e", "overlay1": "#697196", "green": "#9ece6a", "blue": "#7aa2f7"},
	"tokyo-night-day":  {"accent": "#2e7de9", "mauve": "#7847bd", "subtext0": "#6172b0", "yellow": "#8c6c3e", "red": "#f52a65", "overlay1": "#68709a", "green": "#587539", "blue": "#2e7de9"},
	"dracula":          {"accent": "#bd93f9", "mauve": "#ff79c6", "subtext0": "#d2d2dc", "yellow": "#f1fa8c", "red": "#ff5555", "overlay1": "#828cb4", "green": "#50fa7b", "blue": "#8be9fd"},
	"nord":             {"accent": "#88c0d0", "mauve": "#b48ead", "subtext0": "#d8dee9", "yellow": "#ebcb8b", "red": "#bf616a", "overlay1": "#646e82", "green": "#a3be8c", "blue": "#81a1c1"},
	"gruvbox":          {"accent": "#d79921", "mauve": "#d3869b", "subtext0": "#d5c4a1", "yellow": "#fabd2f", "red": "#fb4934", "overlay1": "#a89984", "green": "#b8bb26", "blue": "#83a598"},
	"gruvbox-light":    {"accent": "#076678", "mauve": "#8f3f71", "subtext0": "#504945", "yellow": "#b57614", "red": "#9d0006", "overlay1": "#7c6f64", "green": "#79740e", "blue": "#076678"},
	"one-dark":         {"accent": "#61afef", "mauve": "#c678dd", "subtext0": "#969ca8", "yellow": "#e5c07b", "red": "#e06c75", "overlay1": "#737a87", "green": "#98c379", "blue": "#61afef"},
	"one-light":        {"accent": "#4078f2", "mauve": "#a626a4", "subtext0": "#686b77", "yellow": "#c18401", "red": "#e45649", "overlay1": "#686b77", "green": "#50a14f", "blue": "#4078f2"},
	"solarized":        {"accent": "#268bd2", "mauve": "#d33682", "subtext0": "#839496", "yellow": "#b58900", "red": "#dc322f", "overlay1": "#657b83", "green": "#859900", "blue": "#268bd2"},
	"solarized-light":  {"accent": "#268bd2", "mauve": "#d33682", "subtext0": "#839496", "yellow": "#b58900", "red": "#dc322f", "overlay1": "#586e75", "green": "#859900", "blue": "#268bd2"},
	"kanagawa":         {"accent": "#7e9cd8", "mauve": "#957fb8", "subtext0": "#c8c3aa", "yellow": "#c0a36e", "red": "#c34043", "overlay1": "#87867d", "green": "#76946a", "blue": "#7e9cd8"},
	"kanagawa-lotus":   {"accent": "#4d699b", "mauve": "#624c83", "subtext0": "#43436c", "yellow": "#77713f", "red": "#c84053", "overlay1": "#8a8980", "green": "#6f894e", "blue": "#4d699b"},
	"rose-pine":        {"accent": "#c4a7e7", "mauve": "#c4a7e7", "subtext0": "#c8c5dc", "yellow": "#f6c177", "red": "#eb6f92", "overlay1": "#908caa", "green": "#31748f", "blue": "#31748f"},
	"rose-pine-dawn":   {"accent": "#907aa9", "mauve": "#907aa9", "subtext0": "#797593", "yellow": "#ea9d34", "red": "#b4637a", "overlay1": "#797593", "green": "#286983", "blue": "#286983"},
	"vesper":           {"accent": "#ffc799", "mauve": "#ffd1a8", "subtext0": "#a0a0a0", "yellow": "#ffc799", "red": "#ff8080", "overlay1": "#7e7e7e", "green": "#99ffe4", "blue": "#b0b0b0"},
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
		tokens[token] = hexSGR(value, false)
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
	return colorTheme{
		Name:      name,
		Accent:    accent,
		TabActive: tabActiveSGR(accent),
		Mauve:     tokens["mauve"],
		Muted:     tokens["subtext0"],
		Overlay:   tokens["overlay1"],
		Yellow:    tokens["yellow"],
		Red:       tokens["red"],
		Green:     tokens["green"],
		Blue:      tokens["blue"],
	}
}

func tabActiveSGR(accent string) string {
	if accent == "" {
		return "\x1b[7m"
	}
	if rest, ok := strings.CutPrefix(accent, "\x1b[38;2;"); ok && strings.HasSuffix(rest, "m") {
		return "\x1b[48;2;" + rest + "\x1b[30m"
	}
	var code int
	if _, err := fmt.Sscanf(accent, "\x1b[%dm", &code); err == nil && code > 0 {
		return fmt.Sprintf("\x1b[7;%dm", code)
	}
	return "\x1b[7m" + accent
}

func terminalTokenSGRs() map[string]string {
	return map[string]string{
		"accent":   "\x1b[34m",
		"mauve":    "\x1b[37m",
		"subtext0": "\x1b[37m",
		"overlay1": "\x1b[97m",
		"yellow":   "\x1b[33m",
		"red":      "\x1b[91m",
		"green":    "\x1b[32m",
		"blue":     "\x1b[34m",
	}
}

// terminalTheme uses the terminal's own 16-color palette, matching Herdr's "terminal" theme.
func terminalTheme() colorTheme {
	return colorThemeFromTokens("terminal", terminalTokenSGRs())
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
		if out := hexSGR(s, false); out != "" {
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
