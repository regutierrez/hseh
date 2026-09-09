package main

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLiveLayoutKeepsSelectionWhenPreviewFills(t *testing.T) {
	snapshot := HerdrSessionSnapshot{
		FocusedWorkspaceID: "w1",
		Workspaces: []HerdrWorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha", AgentStatus: "unknown", ActiveTabID: "w1:t1"},
			{WorkspaceID: "w2", Label: "beta", AgentStatus: "unknown", ActiveTabID: "w2:t1"},
		},
		Layouts: []HerdrPaneLayout{
			{TabID: "w1:t1", FocusedPaneID: "w1:p1"},
			{TabID: "w2:t1", FocusedPaneID: "w2:p1"},
		},
	}
	history := emptyFocusHistory(ServerContinuityWitness{})
	history.Spaces = []string{"w2", "w1"}
	m := newPickerModel("spaces", snapshot, history, defaultSidebarLayout(), 0, 40)
	m.width, m.height = 58, 22
	if m.selectedID != pickerSelectionID(pickerKindSpace, "w2") {
		t.Fatalf("preselect %s", m.selectedID)
	}
	before := m.View()
	m.previewText = strings.Repeat("BETA_TICK_9\n", 16)
	after := m.View()
	if !hasGraySelectedLabel(before, "beta") || !hasGraySelectedLabel(after, "beta") {
		t.Fatalf("lost selection after preview fill before=%q after=%q", before, after)
	}
}

func TestCRLFPreviewDoesNotEraseSelectedRow(t *testing.T) {
	var ticks strings.Builder
	for i := 0; i < 18; i++ {
		ticks.WriteString("\x1b[0mBETA_TICK_X\r\n")
	}
	m := pickerModel{width: 60, height: 22, widePreviewMinCols: 40, selectedID: "w2", visible: []PickerItem{
		{ID: "w1", Rows: []string{"alpha"}, DisplayRows: []string{"alpha"}},
		{ID: "w2", Rows: []string{"beta"}, DisplayRows: []string{"beta"}},
	}, previewText: AllowVisiblePreviewANSI(ticks.String())}
	got := m.View()
	if strings.ContainsRune(got, '\r') {
		t.Fatalf("CR in view: %q", got)
	}
	if !hasGraySelectedLabel(got, "beta") {
		t.Fatalf("CRLF preview hid selection: %q", StripTerminalControls(got))
	}
}

func TestPopupSizedViewKeepsBetaSelectionWithPreview(t *testing.T) {
	m := pickerModel{width: 58, height: 22, widePreviewMinCols: 40, selectedID: "w2", visible: []PickerItem{
		{ID: "w1", Rows: []string{"· · alpha"}, DisplayRows: []string{"· · alpha"}},
		{ID: "w2", Rows: []string{"· · beta"}, DisplayRows: []string{"· · beta"}},
	}}
	m.previewText = strings.Repeat("BETA_TICK_1\n", 12)
	got := m.View()
	if !hasGraySelectedLabel(got, "beta") {
		t.Fatalf("popup-sized view lost selection: %q", got)
	}
}

func TestStalePreviewDoesNotStartDuplicateRead(t *testing.T) {
	m := pickerModel{
		previewInFlight:    true,
		previewSeq:         2,
		widePreviewMinCols: 80,
		width:              100,
		selectedID:         "b",
		visible:            []PickerItem{{ID: "a", PreviewPane: "w1:p1"}, {ID: "b", PreviewPane: "w2:p1"}},
	}
	next, cmd := m.Update(previewLoadedMsg{seq: 1, targetID: "a", paneID: "w1:p1", text: "stale"})
	got := next.(pickerModel)
	if !got.previewInFlight {
		t.Fatal("stale reply cleared in-flight")
	}
	if cmd != nil {
		t.Fatal("stale reply started another read")
	}
	if got.previewText == "stale" {
		t.Fatal("stale text applied")
	}
}

func TestPreviewABAIgnoresFirstReply(t *testing.T) {
	m := pickerModel{widePreviewMinCols: 40, selectedID: "a", visible: []PickerItem{
		{ID: "a", PreviewPane: "pa", Rows: []string{"a"}},
		{ID: "b", PreviewPane: "pb", Rows: []string{"b"}},
	}}
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	got := next.(pickerModel)
	if cmd == nil || got.previewSeq != 1 {
		t.Fatalf("first preview seq=%d cmd=%v", got.previewSeq, cmd != nil)
	}
	got.selectedID = "b"
	cmd = got.afterSelectionChange()
	if cmd == nil {
		t.Fatal("expected second preview")
	}
	next, _ = got.Update(previewLoadedMsg{seq: 1, targetID: "a", paneID: "pa", text: "A-PANE"})
	got = next.(pickerModel)
	if got.previewText == "A-PANE" {
		t.Fatal("A reply applied after switching to B")
	}
	if !got.previewInFlight {
		t.Fatal("B read guard lost")
	}
	next, _ = got.Update(previewLoadedMsg{seq: got.previewSeq, targetID: "b", paneID: "pb", text: "B-PANE"})
	got = next.(pickerModel)
	if got.previewText != "B-PANE" {
		t.Fatalf("got %q", got.previewText)
	}
}

func TestPreviousAgentOffscreenStaysVisible(t *testing.T) {
	var items []PickerItem
	for i := 0; i < 12; i++ {
		items = append(items, PickerItem{ID: "a" + string(rune('a'+i)), Rows: []string{"row", "more", "lines"}})
	}
	items = append(items, PickerItem{ID: "prev", Rows: []string{"PREVIOUS_AGENT", "line2", "line3"}})
	m := pickerModel{width: 40, height: 8, selectedID: "prev", visible: items}
	got := StripTerminalControls(m.renderList(20, 6))
	if !strings.Contains(got, "PREVIOUS_AGENT") {
		t.Fatalf("previous agent offscreen: %q", got)
	}
}

func TestSelectionDisappearsWhenTargetGone(t *testing.T) {
	m := pickerModel{
		layout:     defaultSidebarLayout(),
		selectedID: "w-gone",
		allItems:   []PickerItem{{ID: "w-gone"}, {ID: "w1"}},
		visible:    []PickerItem{{ID: "w-gone"}, {ID: "w1"}},
		snapshot:   HerdrSessionSnapshot{Workspaces: []HerdrWorkspaceRow{{WorkspaceID: "w1", Label: "keep"}}},
		history:    emptyFocusHistory(ServerContinuityWitness{}),
	}
	m.refreshMembership()
	if m.selectedID == "w-gone" {
		t.Fatal("disappeared target still selected")
	}
}

func TestAgentPriorityOrderBlockedFirst(t *testing.T) {
	snapshot := HerdrSessionSnapshot{Agents: []HerdrAgentRow{
		{HerdrPaneRow: HerdrPaneRow{PaneID: "p-idle", Agent: "pi", AgentStatus: "idle"}, StateChangeSeq: 9},
		{HerdrPaneRow: HerdrPaneRow{PaneID: "p-done", Agent: "pi", AgentStatus: "done"}, StateChangeSeq: 3},
		{HerdrPaneRow: HerdrPaneRow{PaneID: "p-block", Agent: "pi", AgentStatus: "blocked"}, StateChangeSeq: 1},
		{HerdrPaneRow: HerdrPaneRow{PaneID: "p-work", Agent: "pi", AgentStatus: "working"}, StateChangeSeq: 4},
	}}
	items := BuildPickerItemsWithLayout("agents", snapshot, emptyFocusHistory(ServerContinuityWitness{}), defaultSidebarLayout())
	want := []string{"p-block", "p-done", "p-work", "p-idle"}
	if len(items) != 4 {
		t.Fatalf("%d items", len(items))
	}
	for i, pane := range want {
		if items[i].PaneID != pane {
			t.Fatalf("index %d got %s want %s", i, items[i].PaneID, pane)
		}
	}
}

func TestSnapshotCorruptAssociationKeepsLiveMembership(t *testing.T) {
	snapshot := HerdrSessionSnapshot{
		Workspaces: []HerdrWorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha"},
			{WorkspaceID: "w2", Label: "beta"},
		},
	}
	m := newPickerModel("spaces", snapshot, emptyFocusHistory(ServerContinuityWitness{}), defaultSidebarLayout(), 0, 40)
	m.snapshotSeq = 1
	m.snapshotInFlight = true
	m.associationRecords = []SpaceAssociationRecord{{DefinitionID: "def", ResolvedDir: "/tmp", WorkspaceID: "w2"}}
	fresh := HerdrSessionSnapshot{Workspaces: []HerdrWorkspaceRow{
		{WorkspaceID: "w1", Label: "alpha"},
		{WorkspaceID: "w2", Label: "beta"},
		{WorkspaceID: "w3", Label: "gamma"},
	}}
	next, _ := m.Update(snapshotLoadedMsg{
		seq:        1,
		snapshot:   fresh,
		history:    emptyFocusHistory(ServerContinuityWitness{}),
		catalogErr: fmt.Errorf("hseh association: corrupt state"),
	})
	got := next.(pickerModel)
	if len(got.snapshot.Workspaces) != 3 {
		t.Fatalf("live snapshot discarded: %+v", got.snapshot.Workspaces)
	}
	if got.associationRecords != nil {
		t.Fatalf("corrupt associations used: %+v", got.associationRecords)
	}
	if !strings.Contains(got.statusErr, "corrupt") {
		t.Fatalf("association error not visible: %q", got.statusErr)
	}
	if !pickerItemByID(got.visible, pickerSelectionID(pickerKindSpace, "w3")) {
		t.Fatalf("live membership frozen: %+v", got.visible)
	}
}

func TestSnapshotLiveFetchErrorDoesNotReplaceSnapshot(t *testing.T) {
	snapshot := HerdrSessionSnapshot{Workspaces: []HerdrWorkspaceRow{{WorkspaceID: "w1", Label: "alpha"}}}
	m := newPickerModel("spaces", snapshot, emptyFocusHistory(ServerContinuityWitness{}), defaultSidebarLayout(), 0, 40)
	m.snapshotSeq = 2
	m.snapshotInFlight = true
	next, _ := m.Update(snapshotLoadedMsg{seq: 2, liveErr: fmt.Errorf("hseh herdr socket: down")})
	got := next.(pickerModel)
	if len(got.snapshot.Workspaces) != 1 || got.snapshot.Workspaces[0].WorkspaceID != "w1" {
		t.Fatalf("live error replaced snapshot: %+v", got.snapshot.Workspaces)
	}
	if !strings.Contains(got.statusErr, "down") {
		t.Fatalf("live error hidden: %q", got.statusErr)
	}
}
