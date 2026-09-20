package picker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamedThemePaintsRowsAndStatus(t *testing.T) {
	th := resolveTheme("catppuccin", nil)
	if th.Muted != hexSGR("#a6adc8", false) {
		t.Fatalf("muted %q", th.Muted)
	}
	_, display := renderSpaceRow(spaceRow{source: SourceHerdr, name: "alpha", path: "/tmp/a"}, "dots", th)
	if strings.Contains(display, "38;5;245") || !strings.Contains(display, th.Muted) || !strings.Contains(display, th.Text) {
		t.Fatalf("row %q", display)
	}
	if th.PanelBg != hexSGR("#181825", false) || th.Text != hexSGR("#cdd6f4", false) {
		t.Fatalf("palette %+v", th)
	}
	for status, token := range map[string]string{"blocked": th.Red, "working": th.Yellow, "done": th.Blue, "idle": th.Green, "unknown": th.Overlay} {
		got := stylePlainToken(sidebarToken{Name: "state_icon"}, "●", status, th)
		if !strings.Contains(got, token) || strings.Contains(got, "\x1b[91m") {
			t.Fatalf("%s %q", status, got)
		}
	}
}

func TestAutoSwitchAndCustom(t *testing.T) {
	sel := themeSelection{Name: "tokyo-night", AutoSwitch: true, LightName: "catppuccin-latte", DarkName: "dracula"}
	if light := resolveThemeConfig(sel, appearanceLight); light.Name != "catppuccin-latte" {
		t.Fatalf("light %q", light.Name)
	}
	if dark := resolveThemeConfig(sel, appearanceDark); dark.Name != "dracula" {
		t.Fatalf("dark %q", dark.Name)
	}
	if sibling := resolveThemeConfig(themeSelection{Name: "gruvbox", AutoSwitch: true}, appearanceLight); sibling.Name != "gruvbox-light" {
		t.Fatalf("sibling %q", sibling.Name)
	}

	th := resolveTheme("nord", map[string]string{
		"accent": "#010203",
		"red":    "rgb(255, 85, 85)",
		"green":  "rgb(1, 2, 3, 0.5)",
		"mauve":  "white",
		"blue":   "reset",
	})
	if th.Accent != hexSGR("#010203", false) || th.Red != "\x1b[38;2;255;85;85m" || th.Green != "\x1b[38;2;1;2;3m" || th.Mauve != "\x1b[97m" || th.Blue != "" {
		t.Fatalf("custom %+v", th)
	}

	term := resolveTheme("terminal", map[string]string{"red": "#ff0000"})
	if term.Accent != "\x1b[34m" || term.Mauve != "\x1b[37m" || term.Red != hexSGR("#ff0000", false) || term.PanelBg != "" || term.Text != "" || strings.Contains(term.Accent, "38;2") {
		t.Fatalf("terminal custom %+v", term)
	}
	if term.selectedFill() != "\x1b[100m" {
		t.Fatalf("terminal selected fill %q", term.selectedFill())
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`
[theme]
name = "gruvbox"
auto_switch = true
[theme.custom]
accent = "#010203"
[theme.custom.light]
accent = "#070809"
[theme.custom.dark]
red = "#0a0b0c"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := appearanceForTheme
	t.Cleanup(func() { appearanceForTheme = orig })
	appearanceForTheme = func() appearanceKind { return appearanceLight }
	light, errs := loadThemeFromPath(path)
	if len(errs) != 0 || light.Name != "gruvbox-light" || light.Accent != hexSGR("#070809", false) {
		t.Fatalf("toml light %+v %v", light, errs)
	}
	appearanceForTheme = func() appearanceKind { return appearanceDark }
	dark, errs := loadThemeFromPath(path)
	if len(errs) != 0 || dark.Name != "gruvbox" || dark.Accent != hexSGR("#010203", false) || dark.Red != hexSGR("#0a0b0c", false) {
		t.Fatalf("toml dark %+v %v", dark, errs)
	}

	if appearanceFromCOLORFGBG("0;15") != appearanceLight || appearanceFromCOLORFGBG("15;0") != appearanceDark {
		t.Fatal("COLORFGBG")
	}

	for name, tokens := range herdrThemePalettes {
		for _, key := range herdrPaletteKeys {
			if _, ok := tokens[key]; !ok {
				t.Fatalf("%s missing %s", name, key)
			}
		}
	}
	if toBgSGR(hexSGR("#112233", false)) != hexSGR("#112233", true) || toBgSGR("\x1b[34m") != "\x1b[44m" || toBgSGR("") != "" {
		t.Fatal("toBgSGR")
	}
	t.Setenv(appearanceEnv, "light")
	t.Setenv("COLORFGBG", "15;0")
	if hostAppearance() != appearanceLight {
		t.Fatal("HSEH_APPEARANCE")
	}
}

func TestListingUsesHerdrTokensAndLivePreviewKeepsPaneSGR(t *testing.T) {
	th := resolveTheme("catppuccin", nil)
	listing := colorListing("\x1b[34msub/\x1b[0m\nfile.go", th)
	if !strings.Contains(listing, th.Blue+"sub/\x1b[0m") || !strings.Contains(listing, th.Text+"file.go\x1b[0m") || strings.Contains(listing, "\x1b[34msub") {
		t.Fatalf("listing %q", listing)
	}
	m := model{theme: th, previewText: "\x1b[31magent\x1b[0m", previewPane: "w1:p1"}
	got := m.renderPreview(12, 1)
	if !strings.Contains(got, "\x1b[31magent") || !strings.Contains(got, th.panelFill()) {
		t.Fatalf("live preview remapped or unfilled: %q", got)
	}
}
