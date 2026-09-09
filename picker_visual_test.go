package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// hasRailSelectedLabel reports whether a rendered line carrying the label starts with the selection rail.
func hasRailSelectedLabel(view, label string) bool {
	for _, line := range strings.Split(view, "\n") {
		plain := StripTerminalControls(line)
		if strings.HasPrefix(plain, pickerSelectionRail) && strings.Contains(plain, label) {
			return true
		}
	}
	return false
}

func TestSelectedBlockHasRailAndBoldLabelNotBackground(t *testing.T) {
	m := pickerModel{selectedID: "a", visible: []PickerItem{{ID: "a", DisplayRows: []string{"\x1b[31malpha\x1b[0m", "detail"}}, {ID: "b", Rows: []string{"beta"}}}}
	layout := m.buildPickerListLayout(20, 6)
	selected, other := 0, 0
	for i, id := range layout.ItemIDs {
		line := layout.Lines[i]
		if strings.Contains(line, "\x1b[48;") {
			t.Fatalf("row background used: %q", line)
		}
		if id == "a" {
			selected++
			plain := StripTerminalControls(line)
			if !strings.HasPrefix(plain, pickerSelectionRail+" ") {
				t.Fatalf("selected row lacks rail: %q", line)
			}
			if strings.Contains(plain, "alpha") && !strings.Contains(line, "\x1b[1m") {
				t.Fatalf("selected primary label not bold: %q", line)
			}
			if ansi.StringWidth(line) != 20 {
				t.Fatalf("selected row width: %q", line)
			}
		} else if strings.HasPrefix(StripTerminalControls(line), pickerSelectionRail) {
			t.Fatalf("rail leaked beyond selected block: %q", line)
		}
		if id == "b" {
			other++
		}
	}
	if selected != 2 || other == 0 {
		t.Fatalf("expected both blocks, ids=%v", layout.ItemIDs)
	}
	if strings.Contains(StripTerminalControls(strings.Join(layout.Lines, "\n")), "> ") {
		t.Fatal("old pointer remains")
	}
}

func TestLivePreviewCropsGridToBottomWithoutReflow(t *testing.T) {
	m := pickerModel{previewPane: "w1:p1", previewTextLive: true, previewText: "TOP" + strings.Repeat(" ", 77) + "\nMIDDLE" + strings.Repeat(" ", 74) + "\nLAST" + strings.Repeat(" ", 76) + "\n"}
	got := StripTerminalControls(m.renderPreview(12, 2))
	lines := strings.Split(got, "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "MIDDLE") || !strings.HasPrefix(lines[1], "LAST") {
		t.Fatalf("not bottom two source rows: %q", got)
	}
}

func TestLivePreviewPreservesSGRAcrossClippedRows(t *testing.T) {
	m := pickerModel{previewPane: "w1:p1", previewTextLive: true, previewText: "\x1b[31mTOP\n中文LAST\n"}
	got := m.renderPreview(6, 1)
	if !strings.Contains(got, "\x1b[31m") || ansi.StringWidth(got) != 6 || !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("style/cell boundary broken: %q", got)
	}
	if !strings.HasPrefix(StripTerminalControls(got), "中文LA") {
		t.Fatalf("wrong crop %q", got)
	}
}

func TestDefinitionPreviewStillStartsAtTop(t *testing.T) {
	m := pickerModel{previewText: "definition\nfirst tab\nlast tab"}
	if got := StripTerminalControls(m.renderPreview(12, 2)); !strings.Contains(got, "definition") || strings.Contains(got, "last tab") {
		t.Fatalf("definition was tail-cropped: %q", got)
	}
}

func TestViewTabsTrackKeyboardCycle(t *testing.T) {
	m := newPickerModel("spaces", HerdrSessionSnapshot{}, emptyFocusHistory(ServerContinuityWitness{}), defaultSidebarLayout(), 0, 100)
	m.width, m.height = 100, 8
	for _, want := range []string{"Spaces", "Agents", "Spaces"} {
		got := m.View()
		plain := StripTerminalControls(got)
		for _, label := range []string{"Spaces", "Agents"} {
			if !strings.Contains(plain, label) {
				t.Fatalf("tab missing %s: %q", label, got)
			}
		}
		if strings.Contains(plain, " All") {
			t.Fatalf("All tab still present: %q", got)
		}
		if !strings.Contains(got, m.th().TabActive+" "+want+" ") {
			t.Fatalf("active tab not %s: %q", want, got)
		}
		if m.itemIDAtMouse(0, 0) != "" {
			t.Fatal("tab row is clickable result")
		}
		for y := m.height - m.searchPaneHeight(); y < m.height; y++ {
			if m.itemIDAtMouse(0, y) != "" {
				t.Fatal("search row is clickable result")
			}
		}
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = next.(pickerModel)
	}
}
