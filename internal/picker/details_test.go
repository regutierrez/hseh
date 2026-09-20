package picker

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/termtext"
)

func TestHerdrDotGlyphsAndColors(t *testing.T) {
	for _, c := range []struct{ status, glyph, color string }{{"blocked", "●", "91"}, {"working", "●", "33"}, {"done", "●", "34"}, {"idle", "○", "32"}, {"unknown", "·", "97"}} {
		if got := stateIconGlyph(c.status, "dots"); got != c.glyph {
			t.Errorf("%s glyph %s want %s", c.status, got, c.glyph)
		}
		got := stylePlainToken(sidebarToken{Name: "state_icon"}, c.glyph, c.status, terminalTheme())
		if !strings.Contains(got, "\x1b["+c.color+"m") {
			t.Errorf("%s lost terminal palette color: %q", c.status, got)
		}
	}
}

func TestHerdrSymbolGlyphs(t *testing.T) {
	for _, c := range []struct{ status, glyph string }{{"blocked", "×"}, {"working", "◐"}, {"done", "✓"}, {"idle", "○"}, {"unknown", "·"}} {
		if got := stateIconGlyph(c.status, "symbols"); got != c.glyph {
			t.Errorf("%s glyph %s want %s", c.status, got, c.glyph)
		}
	}
}

func TestAgentDetailsUseHerdrDescriptionAndDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	snapshot := herdr.SessionSnapshot{Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w1", Label: "Project"}}, Tabs: []herdr.TabRow{{TabID: "w1:t1", Label: "Implement"}}}
	agent := herdr.AgentRow{PaneRow: herdr.PaneRow{WorkspaceID: "w1", TabID: "w1:t1", PaneID: "w1:p1", Agent: "pi", AgentStatus: "working", Cwd: filepath.Join(home, "code", "nested", "app"), DisplayAgent: "pi - Fix tests", Tokens: map[string]string{"name2": "keep details"}}}
	layout := defaultSidebarLayout()
	layout.AgentRows = tokensFromNames([][]string{{"state_icon", "workspace", "tab"}, {"agent"}, {"$name2"}})
	plain, display := renderAgentSidebarRows(snapshot, agent, layout, colorTheme{})
	if len(plain) != 3 || !strings.Contains(plain[0], "\U000f03ff Implement") || plain[1] != "Project (~/…/app)" || plain[2] != "pi - Fix tests keep details" {
		t.Fatalf("rows %+v", plain)
	}
	th := themeOrDefault(colorTheme{})
	if !strings.Contains(display[0], th.Text) {
		t.Fatalf("heading not themed: %q", display[0])
	}
	for _, i := range []int{1, 2} {
		if !strings.Contains(display[i], mutedSGR) {
			t.Fatalf("secondary row not muted: %q", display[i])
		}
	}
}

func TestSelectedRowRetainsStatusColorAndBold(t *testing.T) {
	m := model{selectedID: "a", visible: []Item{{ID: "a", DisplayRows: []string{"\x1b[33m●\x1b[0m \x1b[1mProject\x1b[0m"}}}}
	got := strings.Join(m.buildListLayout(30, 3).Lines, "\n")
	if !strings.Contains(got, "\x1b[33m●") || !strings.Contains(got, "\x1b[1mProject") {
		t.Fatalf("selected styling erased: %q", got)
	}
}

func TestWrappedDetailsKeepMutedColorAndRail(t *testing.T) {
	m := model{selectedID: "a", visible: []Item{{ID: "a", DisplayRows: []string{"\x1b[1mName\x1b[0m", mutedSGR + strings.Repeat("detail ", 5) + "\x1b[0m"}}}}
	layout := m.buildListLayout(12, 6)
	found := 0
	for i, id := range layout.ItemIDs {
		if id != "a" {
			continue
		}
		plain := termtext.StripControls(layout.Lines[i])
		if !strings.HasPrefix(plain, selectionRail+" ") {
			t.Fatalf("selected line lacks rail: %q", layout.Lines[i])
		}
		if !strings.Contains(plain, "detail") {
			continue
		}
		found++
		if !strings.Contains(layout.Lines[i], mutedSGR) {
			t.Fatalf("wrapped detail lost muted style: %q", layout.Lines[i])
		}
	}
	if found < 2 {
		t.Fatal("fixture did not produce wrapped detail rows")
	}
}
