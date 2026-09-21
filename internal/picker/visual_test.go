package picker

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/termtext"
)

func hasRailSelectedLabel(view, label string) bool {
	for _, line := range strings.Split(view, "\n") {
		plain := termtext.StripControls(line)
		if strings.HasPrefix(plain, selectionRail) && strings.Contains(plain, label) {
			return true
		}
	}
	return false
}

func TestSelectedBlockHasRailBoldLabelAndSelectionFill(t *testing.T) {
	th := resolveTheme("catppuccin", nil)
	m := model{selectedID: "a", theme: th, visible: []Item{{ID: "a", DisplayRows: []string{"\x1b[31malpha\x1b[0m", "detail"}}, {ID: "b", Rows: []string{"beta"}}}}
	layout := m.buildListLayout(20, 6)
	selected, other := 0, 0
	sel := th.selectedFill()
	panel := th.listFill()
	for i, id := range layout.ItemIDs {
		line := layout.Lines[i]
		if id == "a" {
			selected++
			plain := termtext.StripControls(line)
			if !strings.HasPrefix(plain, selectionRail+" ") {
				t.Fatalf("selected row lacks rail: %q", line)
			}
			if strings.Contains(plain, "alpha") && !strings.Contains(line, "\x1b[1m") {
				t.Fatalf("selected primary label not bold: %q", line)
			}
			if sel != "" && !strings.Contains(line, sel) {
				t.Fatalf("selected row missing fill: %q", line)
			}
			if ansi.StringWidth(line) != 20 {
				t.Fatalf("selected row width: %q", line)
			}
		} else if strings.HasPrefix(termtext.StripControls(line), selectionRail) {
			t.Fatalf("rail leaked beyond selected block: %q", line)
		} else if id == "b" && panel != "" && !strings.Contains(line, panel) {
			t.Fatalf("unselected row missing panel fill: %q", line)
		}
		if id == "b" {
			other++
		}
	}
	if selected != 2 || other == 0 {
		t.Fatalf("expected both blocks, ids=%v", layout.ItemIDs)
	}
}

func TestLivePreviewCropsGridToBottomWithoutReflow(t *testing.T) {
	m := model{previewPane: "w1:p1", previewText: "TOP" + strings.Repeat(" ", 77) + "\nMIDDLE" + strings.Repeat(" ", 74) + "\nLAST" + strings.Repeat(" ", 76) + "\n"}
	got := termtext.StripControls(m.renderPreview(12, 2))
	lines := strings.Split(got, "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "MIDDLE") || !strings.HasPrefix(lines[1], "LAST") {
		t.Fatalf("not bottom two source rows: %q", got)
	}
}

func TestLivePreviewPreservesSGRAcrossClippedRows(t *testing.T) {
	m := model{previewPane: "w1:p1", previewText: "\x1b[31mTOP\n中文LAST\n"}
	got := m.renderPreview(6, 1)
	if !strings.Contains(got, "\x1b[31m") || ansi.StringWidth(got) != 6 || !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("style/cell boundary broken: %q", got)
	}
	if !strings.HasPrefix(termtext.StripControls(got), "中文LA") {
		t.Fatalf("wrong crop %q", got)
	}
}

func TestListingPreviewStartsAtTop(t *testing.T) {
	m := model{previewListing: true, previewText: "definition\nfirst tab\nlast tab"}
	if got := termtext.StripControls(m.renderPreview(12, 2)); !strings.Contains(got, "definition") || strings.Contains(got, "last tab") {
		t.Fatalf("definition was tail-cropped: %q", got)
	}
}

func TestViewTabsTrackKeyboardCycle(t *testing.T) {
	m := newModel("spaces", herdr.SessionSnapshot{}, focus.EmptyHistory(herdr.ContinuityWitness{}), 100)
	m.width, m.height = 100, 8
	for _, want := range []string{"Spaces", "Agents", "Spaces"} {
		got := m.View()
		plain := termtext.StripControls(got)
		for _, label := range []string{"Spaces", "Agents"} {
			if !strings.Contains(plain, label) {
				t.Fatalf("tab missing %s: %q", label, got)
			}
		}
		if !strings.Contains(got, m.th().TabActive+" "+want+" ") {
			t.Fatalf("active tab not %s: %q", want, got)
		}
		if m.itemIDAtMouse(0, 0) != "" {
			t.Fatal("tab row is clickable result")
		}
		for y := m.height - m.frame().searchH; y < m.height; y++ {
			if m.itemIDAtMouse(0, y) != "" {
				t.Fatal("search row is clickable result")
			}
		}
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = next.(model)
	}
}
