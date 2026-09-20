package picker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/regutierrez/hseh/internal/gitinfo"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/space"
)

func TestTerminalThemeUsesHerdrANSI(t *testing.T) {
	th := terminalTheme()
	if th.Name != "terminal" || th.Accent != "\x1b[34m" || th.Mauve != "\x1b[37m" || th.Muted != "\x1b[37m" || th.Overlay != "\x1b[97m" || th.Red != "\x1b[91m" || th.Green != "\x1b[32m" || th.Blue != "\x1b[34m" || th.Yellow != "\x1b[33m" {
		t.Fatalf("terminalTheme %+v", th)
	}
	resolved := resolveTheme("terminal", nil)
	if resolved != th {
		t.Fatalf("resolve terminal %+v", resolved)
	}
}

func TestNamedThemeRowMutedUsesSubtext0(t *testing.T) {
	th := resolveTheme("catppuccin", nil)
	want := hexSGR("#a6adc8", false)
	if th.Muted != want {
		t.Fatalf("catppuccin muted %q want %q", th.Muted, want)
	}
	_, display := renderSpaceRow(spaceRow{source: SourceHerdr, name: "alpha", path: "/tmp/a"}, "dots", th)
	if strings.Contains(display, "38;5;245") {
		t.Fatalf("hardcoded 245 survived: %q", display)
	}
	if !strings.Contains(display, want) {
		t.Fatalf("row missing subtext0: %q", display)
	}
}

func TestNamedThemeStatusGlyphsUsePalette(t *testing.T) {
	th := resolveTheme("catppuccin", nil)
	for _, c := range []struct{ status, token string }{
		{"blocked", th.Red},
		{"working", th.Yellow},
		{"done", th.Blue},
		{"idle", th.Green},
		{"unknown", th.Overlay},
	} {
		got := stylePlainToken(sidebarToken{Name: "state_icon"}, "●", c.status, th)
		if !strings.Contains(got, c.token) {
			t.Errorf("%s glyph %q missing %q", c.status, got, c.token)
		}
		if strings.Contains(got, "\x1b[91m") || strings.Contains(got, "\x1b[36m") {
			t.Errorf("%s used 16-color ANSI: %q", c.status, got)
		}
	}
	if !strings.Contains(stylePlainToken(sidebarToken{Name: "state_icon"}, "●", "blocked", th), hexSGR("#f38ba8", false)) {
		t.Fatal("blocked is not catppuccin red")
	}
	if !strings.Contains(stylePlainToken(sidebarToken{Name: "state_icon"}, "●", "working", th), hexSGR("#f9e2af", false)) {
		t.Fatal("working is not catppuccin yellow")
	}
	if !strings.Contains(stylePlainToken(sidebarToken{Name: "state_icon"}, "●", "done", th), hexSGR("#89b4fa", false)) {
		t.Fatal("done is not catppuccin blue")
	}
	if !strings.Contains(stylePlainToken(sidebarToken{Name: "state_icon"}, "○", "idle", th), hexSGR("#a6e3a1", false)) {
		t.Fatal("idle is not catppuccin green")
	}
}

func TestAutoSwitchPicksLightAndDarkNames(t *testing.T) {
	sel := themeSelection{Name: "tokyo-night", AutoSwitch: true, LightName: "catppuccin-latte", DarkName: "dracula"}
	light := resolveThemeConfig(sel, appearanceLight)
	dark := resolveThemeConfig(sel, appearanceDark)
	if light.Name != "catppuccin-latte" || light.Accent != hexSGR("#1e66f5", false) {
		t.Fatalf("light %+v", light)
	}
	if dark.Name != "dracula" || dark.Accent != hexSGR("#bd93f9", false) {
		t.Fatalf("dark %+v", dark)
	}
	sibling := resolveThemeConfig(themeSelection{Name: "gruvbox", AutoSwitch: true}, appearanceLight)
	if sibling.Name != "gruvbox-light" {
		t.Fatalf("sibling %q", sibling.Name)
	}
}

func TestFlatCustomHexAndRGBApply(t *testing.T) {
	th := resolveTheme("nord", map[string]string{
		"accent": "#010203",
		"red":    "rgb(255, 85, 85)",
		"green":  "rgb(1, 2, 3, 0.5)",
	})
	if th.Name != "nord" {
		t.Fatalf("name %q", th.Name)
	}
	if th.Accent != hexSGR("#010203", false) {
		t.Fatalf("hex accent %q", th.Accent)
	}
	if th.Red != "\x1b[38;2;255;85;85m" {
		t.Fatalf("rgb red %q", th.Red)
	}
	if th.Green != "\x1b[38;2;1;2;3m" {
		t.Fatalf("rgb alpha green %q", th.Green)
	}
	if th.Mauve != hexSGR("#b48ead", false) {
		t.Fatalf("unoverridden mauve %q", th.Mauve)
	}
}

func TestNestedCustomLightDarkDecodeAndApply(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[theme]
name = "gruvbox"
auto_switch = true

[theme.custom]
accent = "#010203"
text = "#040506"

[theme.custom.light]
accent = "#070809"

[theme.custom.dark]
red = "#0a0b0c"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := appearanceForTheme
	t.Cleanup(func() { appearanceForTheme = orig })

	appearanceForTheme = func() appearanceKind { return appearanceLight }
	light, errs := loadThemeFromPath(path)
	if len(errs) != 0 {
		t.Fatalf("light decode %v", errs)
	}
	if light.Name != "gruvbox-light" || light.Accent != hexSGR("#070809", false) {
		t.Fatalf("light %+v", light)
	}

	appearanceForTheme = func() appearanceKind { return appearanceDark }
	dark, errs := loadThemeFromPath(path)
	if len(errs) != 0 {
		t.Fatalf("dark decode %v", errs)
	}
	if dark.Name != "gruvbox" || dark.Accent != hexSGR("#010203", false) || dark.Red != hexSGR("#0a0b0c", false) {
		t.Fatalf("dark %+v", dark)
	}
}

func TestTerminalCustomKeepsANSIBase(t *testing.T) {
	th := resolveTheme("terminal", map[string]string{"red": "#ff0000"})
	if th.Name != "terminal" {
		t.Fatalf("name %q", th.Name)
	}
	if th.Accent != "\x1b[34m" {
		t.Fatalf("accent swapped off ANSI: %q", th.Accent)
	}
	if th.Mauve != "\x1b[37m" || th.Muted != "\x1b[37m" || th.Overlay != "\x1b[97m" {
		t.Fatalf("base tokens lost: %+v", th)
	}
	if th.Red != hexSGR("#ff0000", false) {
		t.Fatalf("custom red %q", th.Red)
	}
	if strings.Contains(th.Accent, "38;2") {
		t.Fatal("terminal accent used catppuccin hex")
	}
}

func TestInvalidCustomColorIsIgnored(t *testing.T) {
	base := resolveTheme("catppuccin", nil)
	th := resolveTheme("catppuccin", map[string]string{"accent": "not-a-color", "mauve": "#zz", "red": "rgb(300,1,1)"})
	if th.Accent != base.Accent || th.Mauve != base.Mauve || th.Red != base.Red {
		t.Fatalf("invalid override applied: %+v", th)
	}
}

func TestHostAppearanceCOLORFGBGAndOverride(t *testing.T) {
	if appearanceFromCOLORFGBG("15;0") != appearanceDark || appearanceFromCOLORFGBG("0;15") != appearanceLight || appearanceFromCOLORFGBG("0;7") != appearanceLight {
		t.Fatal("COLORFGBG mapping")
	}
	t.Setenv(appearanceEnv, "light")
	t.Setenv("COLORFGBG", "15;0")
	if hostAppearance() != appearanceLight {
		t.Fatal("HSEH_APPEARANCE should win")
	}
	t.Setenv(appearanceEnv, "dark")
	if hostAppearance() != appearanceDark {
		t.Fatal("forced dark")
	}
}

func TestNamedAndResetCustomColors(t *testing.T) {
	th := resolveTheme("terminal", map[string]string{"mauve": "white", "overlay1": "reset"})
	if th.Mauve != "\x1b[97m" {
		t.Fatalf("named white %q", th.Mauve)
	}
	if th.Overlay != "" {
		t.Fatalf("reset overlay %q", th.Overlay)
	}
}

func TestCatppuccinMutedHelperMatchesTheme(t *testing.T) {
	if mutedSGR != resolveTheme("catppuccin", nil).Muted || mutedSGR == "\x1b[38;5;245m" {
		t.Fatalf("mutedSGR %q", mutedSGR)
	}
	_, display := renderAgentSidebarRows(herdr.SessionSnapshot{}, herdr.AgentRow{PaneRow: herdr.PaneRow{PaneID: "p", Agent: "pi", AgentStatus: "idle"}}, defaultSidebarLayout(), resolveTheme("catppuccin", nil))
	if len(display) < 2 || !strings.Contains(display[1], mutedSGR) {
		t.Fatalf("default muted missing: %q", display)
	}
}

func TestDefinitionRowUsesThemeMuted(t *testing.T) {
	th := resolveTheme("dracula", nil)
	item := definitionItem(space.Definition{ID: "d", Name: "app", ResolvedDir: "/srv/app"}, false, gitinfo.WorkspaceGit{}, th)
	if strings.Contains(item.DisplayRows[0], "38;5;245") || !strings.Contains(item.DisplayRows[0], th.Muted) {
		t.Fatalf("template muted: %q", item.DisplayRows[0])
	}
}
