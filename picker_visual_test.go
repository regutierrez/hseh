package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func hasGraySelectedLabel(view, label string) bool {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, pickerSelectedBackground) && strings.Contains(StripTerminalControls(line), label) {
			return true
		}
	}
	return false
}

func TestSelectedBlockHasGrayBackground(t *testing.T) {
	m := pickerModel{selectedID: "a", visible: []PickerItem{{ID: "a", DisplayRows: []string{"\x1b[31malpha\x1b[0m", "detail"}}, {ID: "b", Rows: []string{"beta"}}}}
	layout := m.buildPickerListLayout(20, 6)
	selected, other := 0, 0
	for i, id := range layout.ItemIDs {
		if id == "a" {
			selected++
			if !strings.Contains(layout.Lines[i], "\x1b[48;5;243m") {
				t.Fatalf("selected row lacks gray background: %q", layout.Lines[i])
			}
			if ansi.StringWidth(layout.Lines[i]) != 20 {
				t.Fatalf("selected background width: %q", layout.Lines[i])
			}
		} else if strings.Contains(layout.Lines[i], "\x1b[48;5;243m") {
			t.Fatalf("highlight leaked beyond selected block: %q", layout.Lines[i])
		}
		if id == "b" {
			other++
		}
	}
	if selected == 0 || other == 0 {
		t.Fatalf("expected both blocks, ids=%v", layout.ItemIDs)
	}
	if strings.Contains(StripTerminalControls(strings.Join(layout.Lines, "\n")), "> ") {
		t.Fatal("old pointer remains")
	}
}

func TestLivePreviewCropsGridToBottomWithoutReflow(t *testing.T) {
	m := pickerModel{previewPane: "w1:p1", previewText: "TOP" + strings.Repeat(" ", 77) + "\nMIDDLE" + strings.Repeat(" ", 74) + "\nLAST" + strings.Repeat(" ", 76) + "\n"}
	got := StripTerminalControls(m.renderPreview(12, 2))
	lines := strings.Split(got, "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "MIDDLE") || !strings.HasPrefix(lines[1], "LAST") {
		t.Fatalf("not bottom two source rows: %q", got)
	}
}

func TestLivePreviewPreservesSGRAcrossClippedRows(t *testing.T) {
	m := pickerModel{previewPane: "w1:p1", previewText: "\x1b[31mTOP\n中文LAST\n"}
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
		if !strings.Contains(got, "\x1b[48;5;74m\x1b[30m "+want+" ") {
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
