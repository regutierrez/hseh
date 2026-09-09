package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func twoPaneModel() pickerModel {
	return pickerModel{
		width: 120, height: 30, widePreviewMinCols: 100, snapshotReady: true, catalogReady: true,
		previewLoadingDelay: 20 * time.Millisecond,
		selectedID:          "a",
		visible: []PickerItem{
			{ID: "a", PreviewPane: "pa", Rows: []string{"alpha"}},
			{ID: "b", PreviewPane: "pb", Rows: []string{"beta"}},
		},
	}
}

func TestSelectionChangeKeepsLastFrameUntilNewOneLands(t *testing.T) {
	m := twoPaneModel()
	m.previewText, m.previewTextLive, m.previewPane = "A-FRAME", true, "pa"
	m.selectedID = "b"
	cmd := m.afterSelectionChange()
	if cmd == nil {
		t.Fatal("expected a preview read for b")
	}
	if m.previewText != "A-FRAME" {
		t.Fatalf("frame cleared on selection change: %q", m.previewText)
	}
	view := StripTerminalControls(m.View())
	if !strings.Contains(view, "A-FRAME") || strings.Contains(view, pickerCopyLoadingPreview) {
		t.Fatalf("preview pane blanked or loading too early: %q", view)
	}
	next, _ := m.Update(previewLoadedMsg{seq: m.previewSeq, targetID: "b", paneID: "pb", text: "B-FRAME"})
	got := next.(pickerModel)
	if got.previewText != "B-FRAME" || got.previewLoading {
		t.Fatalf("new frame not applied: %q loading=%v", got.previewText, got.previewLoading)
	}
}

func TestLoadingCopyOnlyAfterDelay(t *testing.T) {
	m := twoPaneModel()
	m.previewText, m.previewTextLive, m.previewPane = "A-FRAME", true, "pa"
	m.selectedID = "b"
	cmd := m.afterSelectionChange()
	msgs := flattenCmds(func() tea.Msg {
		// Only run the loading timer; the read would need a socket.
		batch := cmd().(tea.BatchMsg)
		return tea.BatchMsg{batch[1]}
	})
	if len(msgs) != 1 {
		t.Fatalf("loading timer messages %d", len(msgs))
	}
	loading, ok := msgs[0].(previewLoadingMsg)
	if !ok || loading.seq != m.previewSeq {
		t.Fatalf("unexpected timer message %#v", msgs[0])
	}
	next, _ := m.Update(loading)
	got := next.(pickerModel)
	if !got.previewLoading {
		t.Fatal("slow preview did not switch to loading copy")
	}
	if !strings.Contains(StripTerminalControls(got.View()), pickerCopyLoadingPreview) {
		t.Fatal("loading copy not rendered")
	}
	// A stale timer (selection moved on) must not flip loading.
	got.previewLoading = false
	next, _ = got.Update(previewLoadingMsg{seq: got.previewSeq - 1})
	if next.(pickerModel).previewLoading {
		t.Fatal("stale loading timer applied")
	}
}

func TestSameItemRefreshKeepsFrameAndNeverShowsLoading(t *testing.T) {
	m := twoPaneModel()
	m.previewText, m.previewTextLive, m.previewPane = "A-FRAME", true, "pa"
	cmd := m.startPreview(true)
	if cmd == nil {
		t.Fatal("expected refresh read")
	}
	if _, isBatch := cmd().(tea.BatchMsg); isBatch {
		t.Fatal("refresh read must not arm a loading timer")
	}
	if m.previewText != "A-FRAME" || m.previewLoading {
		t.Fatal("refresh blanked frame")
	}
}

func TestPreviewClockChainsOnItsOwnAfterLoad(t *testing.T) {
	m := twoPaneModel()
	m.previewPane, m.previewInFlight, m.previewSeq = "pa", true, 3
	next, cmd := m.Update(previewLoadedMsg{seq: 3, targetID: "a", paneID: "pa", text: "tick"})
	got := next.(pickerModel)
	if cmd == nil {
		t.Fatal("loaded preview did not schedule the next preview tick")
	}
	next, cmd = got.Update(previewTickMsg{seq: 3})
	got = next.(pickerModel)
	if cmd == nil || !got.previewInFlight || got.previewSeq != 4 {
		t.Fatalf("tick did not start refresh: cmd=%v inflight=%v seq=%d", cmd != nil, got.previewInFlight, got.previewSeq)
	}
	if _, cmd := got.Update(previewTickMsg{seq: 3}); cmd != nil {
		t.Fatal("stale tick started a duplicate read")
	}
}

func TestSnapshotTickDoesNotTouchPreview(t *testing.T) {
	m := twoPaneModel()
	m.previewPane, m.previewInFlight, m.previewSeq = "pa", true, 7
	next, _ := m.Update(snapshotTickMsg{})
	got := next.(pickerModel)
	if got.previewSeq != 7 || !got.previewInFlight || !got.snapshotInFlight {
		t.Fatalf("snapshot tick coupled to preview: %+v", got)
	}
}

func TestFirstPaintBeforeCatalogShowsLoadingStates(t *testing.T) {
	m := newAsyncPickerModel("spaces", terminalPickerTheme(), 0, 100, nil)
	m.width, m.height = 120, 30
	plain := StripTerminalControls(m.View())
	if !strings.Contains(plain, "Spaces") || !strings.Contains(plain, pickerSearchTitle) {
		t.Fatalf("chrome missing before data: %q", plain)
	}
	if !strings.Contains(plain, pickerCopyLoading) {
		t.Fatalf("loading copy missing: %q", plain)
	}
	if strings.Count(strings.TrimSpace(plain), "\n") < 5 {
		t.Fatalf("first paint is nearly blank: %q", plain)
	}
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init did not schedule loading")
	}
	if _, ok := cmd().(pickerBootMsg); !ok {
		t.Fatal("Init must defer loading to the boot message")
	}
}

func TestFirstSnapshotPreselectsAndStartsPreview(t *testing.T) {
	m := newAsyncPickerModel("spaces", terminalPickerTheme(), 0, 100, nil)
	m.width, m.height = 120, 30
	m.snapshotSeq, m.snapshotInFlight = 1, true
	history := emptyFocusHistory(ServerContinuityWitness{})
	history.Spaces = []string{"w2", "w1"}
	next, cmd := m.Update(snapshotLoadedMsg{seq: 1, history: history, snapshot: HerdrSessionSnapshot{
		FocusedWorkspaceID: "w1",
		Workspaces: []HerdrWorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha", ActiveTabID: "w1:t1"},
			{WorkspaceID: "w2", Label: "beta", ActiveTabID: "w2:t1"},
		},
		Layouts: []HerdrPaneLayout{{TabID: "w1:t1", FocusedPaneID: "w1:p1"}, {TabID: "w2:t1", FocusedPaneID: "w2:p1"}},
	}})
	got := next.(pickerModel)
	if !got.snapshotReady || got.selectedID != pickerSelectionID(pickerKindSpace, "w2") {
		t.Fatalf("first snapshot did not preselect previous space: %q", got.selectedID)
	}
	if cmd == nil || !got.previewInFlight {
		t.Fatal("first snapshot did not start the preview")
	}
}

func TestLiveErrorBeforeFirstSnapshotShowsDedicatedCopy(t *testing.T) {
	m := newAsyncPickerModel("spaces", terminalPickerTheme(), 0, 100, nil)
	m.width, m.height = 120, 30
	m.snapshotSeq, m.snapshotInFlight = 1, true
	next, _ := m.Update(snapshotLoadedMsg{seq: 1, liveErr: errors.New("hseh herdr socket: down")})
	plain := StripTerminalControls(next.(pickerModel).View())
	if !strings.Contains(plain, pickerCopyLoadFailed) || !strings.Contains(plain, "down") {
		t.Fatalf("live error not surfaced: %q", plain)
	}
}

func TestErrorsStayOutOfSearchLineAndCountStays(t *testing.T) {
	m := twoPaneModel()
	m.query = "alp"
	m.applyQuery()
	m.acceptErr = "hseh focus: boom"
	m.recomputeStatus()
	lines := strings.Split(m.View(), "\n")
	f := m.frame()
	var searchLine string
	for i := f.searchY; i < f.searchY+f.searchH; i++ {
		if strings.Contains(lines[i], pickerSearchPrompt) {
			searchLine = StripTerminalControls(lines[i])
		}
	}
	if searchLine == "" || !strings.Contains(searchLine, "alp") || !strings.Contains(searchLine, " / ") {
		t.Fatalf("search line lost query or count: %q", searchLine)
	}
	if strings.Contains(searchLine, "boom") {
		t.Fatalf("error replaced search line: %q", searchLine)
	}
	footer := StripTerminalControls(lines[f.footerY])
	if !strings.Contains(footer, "boom") {
		t.Fatalf("error missing from footer: %q", footer)
	}
}

func TestFooterDropsSecondaryStatusBeforeHelp(t *testing.T) {
	m := pickerModel{snapshotReady: false}
	wide := StripTerminalControls(m.renderFooter(120))
	if !strings.Contains(wide, "Loading Herdr session") || !strings.Contains(wide, "esc close") {
		t.Fatalf("wide footer: %q", wide)
	}
	tight := StripTerminalControls(m.renderFooter(50))
	if strings.Contains(tight, "Loading Herdr session") || !strings.Contains(tight, "esc close") {
		t.Fatalf("tight footer kept status over help: %q", tight)
	}
	if ansi.StringWidth(m.renderFooter(50)) != 50 {
		t.Fatal("footer width")
	}
}

func TestEmptyStatesHaveDedicatedCopy(t *testing.T) {
	m := pickerModel{width: 60, height: 20, widePreviewMinCols: 100, snapshotReady: true, catalogReady: true}
	if got := StripTerminalControls(m.View()); !strings.Contains(got, pickerCopyNoSpaces) || !strings.Contains(got, pickerCopyNoPreview) {
		t.Fatalf("empty spaces copy: %q", got)
	}
	m.view = pickerViewAgents
	if got := StripTerminalControls(m.View()); !strings.Contains(got, pickerCopyNoAgents) {
		t.Fatalf("empty agents copy: %q", got)
	}
	m.query = "zzz"
	if got := StripTerminalControls(m.View()); !strings.Contains(got, pickerCopyNoResults) || !strings.Contains(got, "zzz") {
		t.Fatalf("no results copy: %q", got)
	}
	m.query = ""
	m.previewErr = "pane gone"
	if got := StripTerminalControls(m.View()); !strings.Contains(got, pickerCopyPreviewFailed) || !strings.Contains(got, "pane gone") {
		t.Fatalf("preview error copy: %q", got)
	}
}

func TestOverflowRowsWhenClipped(t *testing.T) {
	var items []PickerItem
	for i := 0; i < 30; i++ {
		items = append(items, PickerItem{ID: fmt.Sprintf("i%02d", i), Rows: []string{fmt.Sprintf("row-%02d", i), "detail"}})
	}
	m := pickerModel{selectedID: "i15", visible: items}
	layout := m.buildPickerListLayout(30, 12)
	plain := StripTerminalControls(strings.Join(layout.Lines, "\n"))
	if !strings.Contains(plain, "↑ ") || !strings.Contains(plain, " more") || !strings.Contains(plain, "↓ ") {
		t.Fatalf("overflow rows missing: %q", plain)
	}
	if !strings.Contains(plain, "row-15") {
		t.Fatalf("selected row missing: %q", plain)
	}
	if layout.ItemIDs[0] != "" || layout.ItemIDs[len(layout.ItemIDs)-1] != "" {
		t.Fatalf("overflow rows must not be clickable: %v", layout.ItemIDs)
	}
	m.selectedID = "i00"
	plain = StripTerminalControls(m.renderList(30, 12))
	if strings.Contains(plain, "↓ ") || !strings.Contains(plain, "↑ ") {
		t.Fatalf("bottom-anchored list wrong indicators: %q", plain)
	}
}

func TestListWrapsOnlyVisibleWindow(t *testing.T) {
	var items []PickerItem
	for i := 0; i < 5000; i++ {
		items = append(items, PickerItem{ID: fmt.Sprintf("i%04d", i), Rows: []string{strings.Repeat("x", 200), "detail"}})
	}
	m := pickerModel{selectedID: "i2500", visible: items}
	start := time.Now()
	layout := m.buildPickerListLayout(40, 20)
	elapsed := time.Since(start)
	if len(layout.Lines) != 20 || !strings.Contains(strings.Join(layout.ItemIDs, ","), "i2500") {
		t.Fatalf("window lost selection: %v", layout.ItemIDs)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("layout wrapped far more than the window: %v", elapsed)
	}
}

func TestInvertedBlockOrderPreserved(t *testing.T) {
	m := pickerModel{selectedID: "a", visible: []PickerItem{
		{ID: "a", Rows: []string{"first-rank"}}, {ID: "b", Rows: []string{"second-rank"}}, {ID: "c", Rows: []string{"third-rank"}},
	}}
	plain := StripTerminalControls(m.renderList(30, 8))
	if strings.Index(plain, "third-rank") > strings.Index(plain, "second-rank") || strings.Index(plain, "second-rank") > strings.Index(plain, "first-rank") {
		t.Fatalf("blocks not inverted: %q", plain)
	}
}

func TestQueryMatchesHighlightedANSISafe(t *testing.T) {
	items := []PickerItem{{ID: "a", Rows: []string{"● beta-project", "~/code"}, DisplayRows: []string{"\x1b[33m●\x1b[0m \x1b[1mbeta-project\x1b[0m", pickerMuted + "~/code\x1b[0m"}, SearchText: "● beta-project ~/code"}}
	m := pickerModel{selectedID: "a", query: "bp", theme: terminalPickerTheme()}
	m.visible, m.searchScratch = filterPickerItemsInto(nil, nil, items, "bp")
	if len(m.visible) != 1 || len(m.visible[0].Matches) == 0 {
		t.Fatalf("fuzzy match lost: %+v", m.visible)
	}
	line := m.renderList(40, 3)
	style := m.th().Mauve + "\x1b[1m"
	if strings.Count(line, style) < 2 {
		t.Fatalf("matched runes not highlighted: %q", line)
	}
	if !strings.Contains(line, style+"b") || !strings.Contains(line, style+"p") {
		t.Fatalf("wrong runes highlighted: %q", line)
	}
	plain := StripTerminalControls(line)
	if !strings.Contains(plain, "● beta-project") || ansi.StringWidth(strings.Split(line, "\n")[len(strings.Split(line, "\n"))-1]) != 40 {
		t.Fatalf("highlight changed visible text or width: %q", plain)
	}
}

func TestHighlightFallsBackToSubstringWhenRowsDiverge(t *testing.T) {
	item := PickerItem{ID: "a", Rows: []string{"Alpha"}, DisplayRows: []string{"ALPHA-extra"}, SearchText: "Alpha", Matches: []int{0, 1}}
	rows := highlightItemRows(item, "alp", "\x1b[7m")
	if !strings.Contains(rows[0], "\x1b[7mA\x1b[0m") || !strings.Contains(rows[0], "\x1b[7mP") {
		t.Fatalf("substring fallback missing: %q", rows[0])
	}
}

func TestDividerDragOnlyWhileDragging(t *testing.T) {
	var out bytes.Buffer
	m := twoPaneModel()
	m.mouseOut = &out
	f := m.frame()
	// Clicking a list row selects it and does not touch mouse mode.
	next, _ := m.Update(tea.MouseMsg{X: 2, Y: f.listY + f.listH - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	got := next.(pickerModel)
	if got.dividerDrag || out.Len() != 0 {
		t.Fatal("row click armed a drag or changed mouse mode")
	}
	next, cmd := got.Update(tea.MouseMsg{X: f.dividerX, Y: f.previewY + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	got = next.(pickerModel)
	if !got.dividerDrag || cmd == nil {
		t.Fatal("divider press did not arm drag")
	}
	flattenCmds(cmd)
	if !strings.Contains(out.String(), "\x1b[?1002h") {
		t.Fatalf("cell motion not enabled for drag: %q", out.String())
	}
	next, _ = got.Update(tea.MouseMsg{X: 30, Y: f.previewY + 2, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	got = next.(pickerModel)
	if got.frame().listW != 30 {
		t.Fatalf("drag did not move divider: %d", got.frame().listW)
	}
	next, _ = got.Update(tea.MouseMsg{X: 2, Y: f.previewY + 2, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	got = next.(pickerModel)
	if got.frame().listW != pickerMinListWidth {
		t.Fatalf("drag not clamped: %d", got.frame().listW)
	}
	next, cmd = got.Update(tea.MouseMsg{X: 25, Y: f.previewY + 2, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	got = next.(pickerModel)
	flattenCmds(cmd)
	if got.dividerDrag || !strings.Contains(out.String(), "\x1b[?1002l") {
		t.Fatalf("release did not end drag / disable motion: %q", out.String())
	}
	if !strings.Contains(mouseClickOnlyEnable, "?1000h") || strings.Contains(mouseClickOnlyEnable, "?1002h") {
		t.Fatal("ordinary mouse mode must be click-only")
	}
}

func TestStackedPreviewRendersBelowList(t *testing.T) {
	m := pickerModel{width: 60, height: 24, widePreviewMinCols: 100, snapshotReady: true, catalogReady: true, selectedID: "a",
		visible: []PickerItem{{ID: "a", PreviewPane: "pa", Rows: []string{"alpha"}}}, previewText: "PANE-BODY", previewTextLive: true}
	f := m.frame()
	if f.mode != previewStacked {
		t.Fatalf("mode %d", f.mode)
	}
	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		t.Fatalf("view rows %d want %d", len(lines), m.height)
	}
	found := -1
	for i, line := range lines {
		if strings.Contains(StripTerminalControls(line), "PANE-BODY") {
			found = i
		}
	}
	if found < f.previewY || found >= f.previewY+f.previewH || found <= f.searchY {
		t.Fatalf("preview at row %d frame %+v", found, f)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > m.width {
			t.Fatalf("row overflow: %q", line)
		}
	}
}

func TestViewLinesFillWidth(t *testing.T) {
	items := []PickerItem{{ID: "a", Rows: []string{"alpha"}, PreviewPane: "pa"}}
	models := []pickerModel{
		{width: 110, height: 24, widePreviewMinCols: 100, snapshotReady: true, catalogReady: true, selectedID: "a", visible: items, previewText: "BODY", previewTextLive: true},
		{width: 70, height: 24, widePreviewMinCols: 100, snapshotReady: true, catalogReady: true, selectedID: "a", visible: items, previewText: "BODY", previewTextLive: true},
		newAsyncPickerModel("spaces", terminalPickerTheme(), 0, 100, nil),
	}
	models[2].width, models[2].height = 110, 24
	for _, m := range models {
		lines := strings.Split(m.View(), "\n")
		if len(lines) != m.height {
			t.Fatalf("%dx%d got %d lines mode=%d", m.width, m.height, len(lines), m.frame().mode)
		}
		for i, line := range lines {
			if w := ansi.StringWidth(line); w != m.width {
				t.Fatalf("%dx%d mode=%d row %d width %d: %q", m.width, m.height, m.frame().mode, i, w, StripTerminalControls(line))
			}
		}
	}
}

func TestThemeResolution(t *testing.T) {
	th := resolvePickerTheme("Tokyo Night", nil)
	if th.Name != "tokyo-night" || th.Accent != hexSGR("#7aa2f7", false) || !strings.HasPrefix(th.TabActive, "\x1b[48;2;") {
		t.Fatalf("tokyo-night %+v", th)
	}
	th = resolvePickerTheme("terminal", nil)
	if th.Accent != "\x1b[34m" || th.Name != "terminal" {
		t.Fatalf("terminal %+v", th)
	}
	th = resolvePickerTheme("nope", map[string]string{"accent": "#123456", "mauve": "bad"})
	if th.Accent != hexSGR("#123456", false) || th.Mauve != hexSGR("#cba6f7", false) {
		t.Fatalf("custom override %+v", th)
	}
	if hexSGR("#abc", true) != "\x1b[48;2;170;187;204m" {
		t.Fatalf("short hex %q", hexSGR("#abc", true))
	}
	if strings.Contains(strings.Join([]string{pickerModel{}.renderTabs(), pickerModel{width: 40, height: 10}.renderSearch(40)}, ""), "\x1b[48;5;") {
		t.Fatal("raw 256-color slab still used in chrome")
	}
}

// burstPreviewReader blocks until cancelled or a short delay, counting outcomes.
type burstPreviewReader struct {
	cancelled atomic.Int32
	completed atomic.Int32
	delay     time.Duration
}

func (r *burstPreviewReader) read(ctx context.Context, paneID string) (string, error) {
	timer := time.NewTimer(r.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		r.cancelled.Add(1)
		return "", ctx.Err()
	case <-timer.C:
		r.completed.Add(1)
		return "frame:" + paneID, nil
	}
}

func burstModel(reader *burstPreviewReader) pickerModel {
	m := pickerModel{width: 120, height: 30, widePreviewMinCols: 100, snapshotReady: true, catalogReady: true, previewLoadingDelay: time.Millisecond}
	m.readPane = reader.read
	for i := 0; i < 9; i++ {
		m.visible = append(m.visible, PickerItem{ID: fmt.Sprintf("i%d", i), PreviewPane: fmt.Sprintf("p%d", i), Rows: []string{fmt.Sprintf("row %d", i)}})
	}
	m.selectedID = "i0"
	return m
}

// runPreviewBurst navigates 8 times quickly, then drains every command. Exactly one read survives.
func runPreviewBurst(m pickerModel) (pickerModel, *burstPreviewReader) {
	reader := &burstPreviewReader{delay: 5 * time.Millisecond}
	m.readPane = reader.read
	var cmds []tea.Cmd
	for i := 1; i <= 8; i++ {
		m.selectedID = fmt.Sprintf("i%d", i)
		if cmd := m.afterSelectionChange(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	for _, cmd := range cmds {
		m = feedCmd(m, cmd)
	}
	return m, reader
}

func TestPreviewBurstCancelsSevenCompletesOne(t *testing.T) {
	m, reader := runPreviewBurst(burstModel(&burstPreviewReader{}))
	if reader.cancelled.Load() != 7 || reader.completed.Load() != 1 {
		t.Fatalf("cancelled=%d completed=%d", reader.cancelled.Load(), reader.completed.Load())
	}
	if m.previewText != "frame:p8" || m.previewInFlight {
		t.Fatalf("final frame %q inflight=%v", m.previewText, m.previewInFlight)
	}
}

func benchmarkItems(n int) []PickerItem {
	items := make([]PickerItem, 0, n)
	for i := 0; i < n; i++ {
		rows := []string{fmt.Sprintf("● project-%04d  main +%d", i, i%7), fmt.Sprintf("~/code/area-%d/project-%04d", i%13, i), "pi - working on feature branch"}
		items = append(items, PickerItem{ID: fmt.Sprintf("space:w%d", i), Rows: rows, DisplayRows: []string{"\x1b[33m●\x1b[0m \x1b[1m" + rows[0][4:] + "\x1b[0m", pickerMuted + rows[1] + "\x1b[0m", pickerMuted + rows[2] + "\x1b[0m"}, SearchText: strings.Join(rows, " "), PreviewPane: fmt.Sprintf("w%d:p1", i)})
	}
	return items
}

func BenchmarkFilterPickerItems(b *testing.B) {
	items := benchmarkItems(2000)
	var dst []PickerItem
	var scratch []string
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst, scratch = filterPickerItemsInto(dst, scratch, items, "proj 42")
	}
	if len(dst) == 0 {
		b.Fatal("no matches")
	}
}

func BenchmarkRenderVisibleWindow(b *testing.B) {
	m := pickerModel{width: 120, height: 40, widePreviewMinCols: 100, snapshotReady: true, catalogReady: true, query: "proj"}
	m.visible = benchmarkItems(2000)
	m.selectedID = m.visible[1000].ID
	f := m.frame()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		layout := m.buildPickerListLayout(f.listW, f.listH)
		if len(layout.Lines) != f.listH {
			b.Fatal("bad height")
		}
	}
}

func BenchmarkPreviewNavigationBurst(b *testing.B) {
	base := burstModel(&burstPreviewReader{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := base
		m.visible = append([]PickerItem{}, base.visible...)
		m.cancels = &pickerCancels{}
		_, reader := runPreviewBurst(m)
		if reader.cancelled.Load() != 7 || reader.completed.Load() != 1 {
			b.Fatalf("cancelled=%d completed=%d", reader.cancelled.Load(), reader.completed.Load())
		}
	}
}

// realisticModel mirrors a live session: a handful of rows, a live preview frame, side-by-side layout.
func realisticModel() pickerModel {
	m := pickerModel{width: 170, height: 40, widePreviewMinCols: 100, snapshotReady: true, catalogReady: true, theme: terminalPickerTheme()}
	m.allItems = benchmarkItems(12)
	m.visible = append([]PickerItem{}, m.allItems...)
	m.selectedID = m.visible[3].ID
	var frame strings.Builder
	for i := 0; i < 38; i++ {
		frame.WriteString(fmt.Sprintf("\x1b[32m$\x1b[0m line %02d \x1b[1msome output\x1b[0m with text that fills the row nicely\n", i))
	}
	m.previewText, m.previewTextLive, m.previewPane = frame.String(), true, m.visible[3].PreviewPane
	return m
}

func BenchmarkView(b *testing.B) {
	m := realisticModel()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(m.View()) == 0 {
			b.Fatal("empty view")
		}
	}
}

func BenchmarkUpdateKeyRune(b *testing.B) {
	base := realisticModel()
	base.readPane = func(context.Context, string) (string, error) { return "", nil }
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := base
		m.cancels = &pickerCancels{}
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
		next, _ = next.(pickerModel).Update(tea.KeyMsg{Type: tea.KeyBackspace})
		if len(next.(pickerModel).visible) != len(base.visible) {
			b.Fatal("filter did not restore")
		}
	}
}
