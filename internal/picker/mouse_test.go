package picker

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/regutierrez/hseh/internal/config"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
)

func TestDefaultWidePreviewMinColumnsIs100(t *testing.T) {
	if config.DefaultWidePreviewMinColumns != 100 {
		t.Fatalf("default %d", config.DefaultWidePreviewMinColumns)
	}
	cols, errs := config.LoadWidePreviewMinColumns()
	if len(errs) != 0 {
		t.Fatalf("errs %v", errs)
	}
	if cols != 100 {
		t.Fatalf("unconfigured load %d", cols)
	}
}

func TestWidePreviewMinColumnsConfigOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "hseh.toml"), []byte("wide_preview_min_columns = 40\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cols, errs := config.LoadWidePreviewMinColumns()
	if len(errs) != 0 || cols != 40 {
		t.Fatalf("override cols=%d errs=%v", cols, errs)
	}
}

func TestPreviewStackedAt99SideBySideAt100(t *testing.T) {
	m := model{
		widePreviewMinCols: config.DefaultWidePreviewMinColumns,
		snapshotReady:      true,
		selectedID:         "w2",
		visible:            []Item{{ID: "w2", PreviewPane: "w2:p1"}},
	}
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 99, Height: 24})
	got := next.(model)
	if got.wideEnoughForPreview() {
		t.Fatal("99-column popup content must not split side by side")
	}
	if got.frame().mode != previewStacked {
		t.Fatalf("99-column popup must stack the preview, mode=%d", got.frame().mode)
	}
	if cmd == nil {
		t.Fatal("stacked preview must start a preview read")
	}
	f := got.frame()
	if f.previewY <= f.searchY || f.previewH < minStackedPreview || f.listH < minStackedListRows {
		t.Fatalf("stacked frame invalid: %+v", f)
	}
	got.previewInFlight = false
	next, cmd = got.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	got = next.(model)
	if !got.wideEnoughForPreview() {
		t.Fatal("100-column popup content must show preview side by side")
	}
	if cmd != nil {
		t.Fatal("switching stacked to side must not restart an already running preview chain")
	}
}

func TestClickSelectsWrappedMultilineUnicodeRow(t *testing.T) {
	m := model{
		width:              20,
		height:             24,
		widePreviewMinCols: 100,
		selectedID:         "a",
		visible: []Item{
			{ID: "a", Rows: []string{"alpha", "detail"}},
			{ID: "b", Rows: []string{"中文中文中文中文中文", "beta-tail"}},
		},
	}
	layout := m.buildListLayout(m.listPaneWidth(), m.bodyHeight())
	hitY := -1
	for i, id := range layout.ItemIDs {
		if id == "b" {
			hitY = i
			break
		}
	}
	if hitY < 0 {
		t.Fatalf("wrapped beta never laid out: %+v", layout)
	}
	got := m.itemIDAtMouse(1, tabRows+hitY)
	if got != "b" {
		t.Fatalf("click wrapped unicode row got %q layout=%+v", got, layout)
	}
}

func TestClickIgnoresHeaderPreviewSeparatorAndPadding(t *testing.T) {
	m := model{
		width:              120,
		height:             10,
		widePreviewMinCols: 100,
		selectedID:         "a",
		visible: []Item{
			{ID: "a", Rows: []string{"alpha"}},
			{ID: "b", Rows: []string{"beta"}},
		},
	}
	if id := m.itemIDAtMouse(2, 0); id != "" {
		t.Fatalf("header click selected %q", id)
	}
	for y := m.height - m.searchPaneHeight(); y < m.height; y++ {
		if id := m.itemIDAtMouse(2, y); id != "" {
			t.Fatalf("search click selected %q", id)
		}
	}
	listWidth := m.listPaneWidth()
	if id := m.itemIDAtMouse(listWidth+1, 2); id != "" {
		t.Fatalf("preview click selected %q", id)
	}
	layout := m.buildListLayout(listWidth, m.bodyHeight())
	sepY := -1
	for i, id := range layout.ItemIDs {
		if i > 0 && id == "" && layout.Lines[i] != "" || (id == "" && i > 0 && i < len(layout.ItemIDs)-1) {
			if layout.ItemIDs[i] == "" {
				sepY = i
				break
			}
		}
	}
	if sepY >= 0 {
		if id := m.itemIDAtMouse(1, tabRows+sepY); id != "" {
			t.Fatalf("separator click selected %q", id)
		}
	}
	if id := m.itemIDAtMouse(-1, 2); id != "" {
		t.Fatalf("padding click selected %q", id)
	}
}

func TestClickScrolledVisibleRow(t *testing.T) {
	var items []Item
	for i := 0; i < 20; i++ {
		items = append(items, Item{ID: string(rune('a' + i)), Rows: []string{"row-" + string(rune('a'+i))}})
	}
	m := model{width: 24, height: 20, widePreviewMinCols: 100, selectedID: items[len(items)-1].ID, visible: items}
	layout := m.buildListLayout(m.listPaneWidth(), m.bodyHeight())
	var firstID string
	for _, id := range layout.ItemIDs {
		if id != "" && id != m.selectedID {
			firstID = id
			break
		}
	}
	if firstID == "" {
		t.Fatalf("expected scrolled non-selected row, layout=%+v", layout)
	}
	var y int
	for i, id := range layout.ItemIDs {
		if id == firstID {
			y = i
			break
		}
	}
	if got := m.itemIDAtMouse(1, tabRows+y); got != firstID {
		t.Fatalf("scrolled click %q want %s", got, firstID)
	}
}

func TestMouseReleaseAndRightClickDoNotSelect(t *testing.T) {
	m := model{
		width: 40, height: 8, widePreviewMinCols: 100, selectedID: "a",
		visible: []Item{{ID: "a", Rows: []string{"alpha"}}, {ID: "b", Rows: []string{"beta"}}},
	}
	layout := m.buildListLayout(m.listPaneWidth(), m.bodyHeight())
	y := 0
	for i, id := range layout.ItemIDs {
		if id == "b" {
			y = i
			break
		}
	}
	next, cmd := m.Update(tea.MouseMsg{X: 1, Y: tabRows + y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	got := next.(model)
	if got.selectedID != "a" || cmd != nil {
		t.Fatalf("release selected %s cmd=%v", got.selectedID, cmd != nil)
	}
	next, cmd = m.Update(tea.MouseMsg{X: 1, Y: tabRows + y, Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
	got = next.(model)
	if got.selectedID != "a" || cmd != nil {
		t.Fatalf("right click selected %s", got.selectedID)
	}
}

func TestClickSelectsWithoutFocusingHerdr(t *testing.T) {
	snapshot := herdr.SessionSnapshot{
		Version: "0.9.0", FocusedWorkspaceID: "w1",
		Workspaces: []herdr.WorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha", ActiveTabID: "w1:t1"},
			{WorkspaceID: "w2", Label: "beta", ActiveTabID: "w2:t1"},
		},
	}
	srv := &hsehtest.Server{Snapshot: snapshot}
	socket, state := hsehtest.Start(t, srv)
	focused := &srv.Focused
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	t.Setenv("HERDR_SESSION", "hseh-test")
	m := newModel("spaces", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout(), 0, 40)
	m.width, m.height = 80, 12
	layout := m.buildListLayout(m.listPaneWidth(), m.bodyHeight())
	y := -1
	for i, id := range layout.ItemIDs {
		if id == SelectionID(KindSpace, "w2") {
			y = i
			break
		}
	}
	if y < 0 {
		t.Fatalf("w2 not in layout %+v", layout.ItemIDs)
	}
	next, cmd := m.Update(tea.MouseMsg{X: 1, Y: tabRows + y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	got := next.(model)
	if got.selectedID != SelectionID(KindSpace, "w2") {
		t.Fatalf("selected %s", got.selectedID)
	}
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(acceptedMsg); ok {
			t.Fatal("click accepted")
		}
	}
	if focused.Load() != nil {
		t.Fatalf("click focused herdr %v", focused.Load())
	}
}
