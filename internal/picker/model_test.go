package picker

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderListKeepsSelectedLastRowVisible(t *testing.T) {
	var items []Item
	for i := 0; i < 12; i++ {
		items = append(items, Item{ID: string(rune('a' + i)), Rows: []string{"row-" + string(rune('a'+i)), "detail", "more"}})
	}
	items = append(items, Item{ID: "last", Rows: []string{"SELECTED_LAST", "tail"}})
	m := model{selectedID: "last", visible: items}
	got := m.renderList(20, 4)
	if !strings.Contains(got, "SELECTED_LAST") {
		t.Fatalf("got %q", got)
	}
}

func TestWrapDisplayLineUsesCellWidth(t *testing.T) {
	lines := wrapDisplayLine("中文emoji😀end", 4)
	for _, line := range lines {
		if w := lipgloss.Width(line); w > 4 {
			t.Fatalf("line %q width %d", line, w)
		}
	}
}

func TestClipBlockCapsPreviewHeight(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("line\n")
	}
	got := clipBlock(b.String(), 10, 3)
	if strings.Count(got, "\n") > 2 {
		t.Fatalf("preview overflow: %q", got)
	}
}

func TestRenderPreviewDoesNotOverflow(t *testing.T) {
	m := model{previewText: strings.Repeat("abcdefghij\n", 40)}
	got := m.renderPreview(8, 2)
	if strings.Count(got, "\n") > 1 {
		t.Fatalf("overflow %q", got)
	}
}
