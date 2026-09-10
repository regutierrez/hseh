package picker

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/space"
	"github.com/regutierrez/hseh/internal/termtext"
)

func TestLiveLayoutKeepsSelectionWhenPreviewFills(t *testing.T) {
	snapshot := herdr.SessionSnapshot{
		FocusedWorkspaceID: "w1",
		Workspaces: []herdr.WorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha", AgentStatus: "unknown", ActiveTabID: "w1:t1"},
			{WorkspaceID: "w2", Label: "beta", AgentStatus: "unknown", ActiveTabID: "w2:t1"},
		},
		Layouts: []herdr.PaneLayout{
			{TabID: "w1:t1", FocusedPaneID: "w1:p1"},
			{TabID: "w2:t1", FocusedPaneID: "w2:p1"},
		},
	}
	history := focus.EmptyHistory(herdr.ContinuityWitness{})
	history.Spaces = []string{"w2", "w1"}
	m := newModel("spaces", snapshot, history, defaultSidebarLayout(), 0, 40)
	m.width, m.height = 58, 22
	if m.selectedID != SelectionID(KindSpace, "w2") {
		t.Fatalf("preselect %s", m.selectedID)
	}
	before := m.View()
	m.previewText = strings.Repeat("BETA_TICK_9\n", 16)
	after := m.View()
	if !hasRailSelectedLabel(before, "beta") || !hasRailSelectedLabel(after, "beta") {
		t.Fatalf("lost selection after preview fill before=%q after=%q", before, after)
	}
}

func TestCRLFPreviewDoesNotEraseSelectedRow(t *testing.T) {
	var ticks strings.Builder
	for i := 0; i < 18; i++ {
		ticks.WriteString("\x1b[0mBETA_TICK_X\r\n")
	}
	m := model{width: 60, height: 22, widePreviewMinCols: 40, selectedID: "w2", visible: []Item{
		{ID: "w1", Rows: []string{"alpha"}, DisplayRows: []string{"alpha"}},
		{ID: "w2", Rows: []string{"beta"}, DisplayRows: []string{"beta"}},
	}, previewText: termtext.KeepSGR(ticks.String())}
	got := m.View()
	if strings.ContainsRune(got, '\r') {
		t.Fatalf("CR in view: %q", got)
	}
	if !hasRailSelectedLabel(got, "beta") {
		t.Fatalf("CRLF preview hid selection: %q", termtext.StripControls(got))
	}
}

func TestPopupSizedViewKeepsBetaSelectionWithPreview(t *testing.T) {
	m := model{width: 58, height: 22, widePreviewMinCols: 40, selectedID: "w2", visible: []Item{
		{ID: "w1", Rows: []string{"· · alpha"}, DisplayRows: []string{"· · alpha"}},
		{ID: "w2", Rows: []string{"· · beta"}, DisplayRows: []string{"· · beta"}},
	}}
	m.previewText = strings.Repeat("BETA_TICK_1\n", 12)
	got := m.View()
	if !hasRailSelectedLabel(got, "beta") {
		t.Fatalf("popup-sized view lost selection: %q", got)
	}
}

func TestStalePreviewDoesNotStartDuplicateRead(t *testing.T) {
	m := model{
		previewInFlight:    true,
		previewSeq:         2,
		widePreviewMinCols: 80,
		width:              100,
		selectedID:         "b",
		visible:            []Item{{ID: "a", PreviewPane: "w1:p1"}, {ID: "b", PreviewPane: "w2:p1"}},
	}
	next, cmd := m.Update(previewLoadedMsg{seq: 1, targetID: "a", paneID: "w1:p1", text: "stale"})
	got := next.(model)
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
	m := model{widePreviewMinCols: 40, selectedID: "a", visible: []Item{
		{ID: "a", PreviewPane: "pa", Rows: []string{"a"}},
		{ID: "b", PreviewPane: "pb", Rows: []string{"b"}},
	}}
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	got := next.(model)
	if cmd == nil || got.previewSeq != 1 {
		t.Fatalf("first preview seq=%d cmd=%v", got.previewSeq, cmd != nil)
	}
	got.selectedID = "b"
	cmd = got.afterSelectionChange()
	if cmd == nil {
		t.Fatal("expected second preview")
	}
	next, _ = got.Update(previewLoadedMsg{seq: 1, targetID: "a", paneID: "pa", text: "A-PANE"})
	got = next.(model)
	if got.previewText == "A-PANE" {
		t.Fatal("A reply applied after switching to B")
	}
	if !got.previewInFlight {
		t.Fatal("B read guard lost")
	}
	next, _ = got.Update(previewLoadedMsg{seq: got.previewSeq, targetID: "b", paneID: "pb", text: "B-PANE"})
	got = next.(model)
	if got.previewText != "B-PANE" {
		t.Fatalf("got %q", got.previewText)
	}
}

func TestPreviousAgentOffscreenStaysVisible(t *testing.T) {
	var items []Item
	for i := 0; i < 12; i++ {
		items = append(items, Item{ID: "a" + string(rune('a'+i)), Rows: []string{"row", "more", "lines"}})
	}
	items = append(items, Item{ID: "prev", Rows: []string{"PREVIOUS_AGENT", "line2", "line3"}})
	m := model{width: 40, height: 8, selectedID: "prev", visible: items}
	got := termtext.StripControls(m.renderList(20, 6))
	if !strings.Contains(got, "PREVIOUS_AGENT") {
		t.Fatalf("previous agent offscreen: %q", got)
	}
}

func TestSelectionDisappearsWhenTargetGone(t *testing.T) {
	m := model{
		layout:     defaultSidebarLayout(),
		selectedID: "w-gone",
		allItems:   []Item{{ID: "w-gone"}, {ID: "w1"}},
		visible:    []Item{{ID: "w-gone"}, {ID: "w1"}},
		snapshot:   herdr.SessionSnapshot{Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w1", Label: "keep"}}},
		history:    focus.EmptyHistory(herdr.ContinuityWitness{}),
	}
	m.refreshMembership()
	if m.selectedID == "w-gone" {
		t.Fatal("disappeared target still selected")
	}
}

func TestAgentPriorityOrderBlockedFirst(t *testing.T) {
	snapshot := herdr.SessionSnapshot{Agents: []herdr.AgentRow{
		{PaneRow: herdr.PaneRow{PaneID: "p-idle", Agent: "pi", AgentStatus: "idle"}, StateChangeSeq: 9},
		{PaneRow: herdr.PaneRow{PaneID: "p-done", Agent: "pi", AgentStatus: "done"}, StateChangeSeq: 3},
		{PaneRow: herdr.PaneRow{PaneID: "p-block", Agent: "pi", AgentStatus: "blocked"}, StateChangeSeq: 1},
		{PaneRow: herdr.PaneRow{PaneID: "p-work", Agent: "pi", AgentStatus: "working"}, StateChangeSeq: 4},
	}}
	items := BuildItemsWithLayout("agents", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout())
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
	snapshot := herdr.SessionSnapshot{
		Workspaces: []herdr.WorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha"},
			{WorkspaceID: "w2", Label: "beta"},
		},
	}
	m := newModel("spaces", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout(), 0, 40)
	m.snapshotSeq = 1
	m.snapshotInFlight = true
	m.associationRecords = []space.AssociationRecord{{DefinitionID: "def", ResolvedDir: "/tmp", WorkspaceID: "w2"}}
	fresh := herdr.SessionSnapshot{Workspaces: []herdr.WorkspaceRow{
		{WorkspaceID: "w1", Label: "alpha"},
		{WorkspaceID: "w2", Label: "beta"},
		{WorkspaceID: "w3", Label: "gamma"},
	}}
	next, _ := m.Update(snapshotLoadedMsg{
		seq:        1,
		snapshot:   fresh,
		history:    focus.EmptyHistory(herdr.ContinuityWitness{}),
		catalogErr: fmt.Errorf("hseh association: corrupt state"),
	})
	got := next.(model)
	if len(got.snapshot.Workspaces) != 3 {
		t.Fatalf("live snapshot discarded: %+v", got.snapshot.Workspaces)
	}
	if got.associationRecords != nil {
		t.Fatalf("corrupt associations used: %+v", got.associationRecords)
	}
	if !strings.Contains(got.statusErr, "corrupt") {
		t.Fatalf("association error not visible: %q", got.statusErr)
	}
	if !HasItemID(got.visible, SelectionID(KindSpace, "w3")) {
		t.Fatalf("live membership frozen: %+v", got.visible)
	}
}

func TestSnapshotLiveFetchErrorDoesNotReplaceSnapshot(t *testing.T) {
	snapshot := herdr.SessionSnapshot{Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w1", Label: "alpha"}}}
	m := newModel("spaces", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout(), 0, 40)
	m.snapshotSeq = 2
	m.snapshotInFlight = true
	next, _ := m.Update(snapshotLoadedMsg{seq: 2, liveErr: fmt.Errorf("hseh herdr socket: down")})
	got := next.(model)
	if len(got.snapshot.Workspaces) != 1 || got.snapshot.Workspaces[0].WorkspaceID != "w1" {
		t.Fatalf("live error replaced snapshot: %+v", got.snapshot.Workspaces)
	}
	if !strings.Contains(got.statusErr, "down") {
		t.Fatalf("live error hidden: %q", got.statusErr)
	}
}

// The first preview read is what the user waits for on open, so it must be a
// hedged selection-change read. Pre-seeding previewPane during rebuildVisible
// once made afterSelectionChange treat it as an unhedged refresh that could sit
// on Herdr's 100ms poll tick.
func TestFirstPreviewReadIsHedged(t *testing.T) {
	snapshot := herdr.SessionSnapshot{
		FocusedWorkspaceID: "w1",
		Workspaces: []herdr.WorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha", AgentStatus: "unknown", ActiveTabID: "w1:t1"},
			{WorkspaceID: "w2", Label: "beta", AgentStatus: "unknown", ActiveTabID: "w2:t1"},
		},
		Layouts: []herdr.PaneLayout{
			{TabID: "w1:t1", FocusedPaneID: "w1:p1"},
			{TabID: "w2:t1", FocusedPaneID: "w2:p1"},
		},
	}
	history := focus.EmptyHistory(herdr.ContinuityWitness{})
	history.Spaces = []string{"w2", "w1"}

	type read struct {
		pane   string
		hedged bool
	}
	record := func(reads *[]read) func(ctx context.Context, paneID string) (string, error) {
		return func(ctx context.Context, paneID string) (string, error) {
			*reads = append(*reads, read{pane: paneID, hedged: herdr.Hedged(ctx)})
			return "frame:" + paneID, nil
		}
	}

	// Popup path: the async model learns its items from the first snapshotLoadedMsg.
	var asyncReads []read
	m := newAsyncModel("spaces", colorTheme{}, 0, 40, nil)
	m.width, m.height = 120, 30
	m.readPane = record(&asyncReads)
	m.snapshotSeq, m.snapshotInFlight = 1, true
	next, cmd := m.Update(snapshotLoadedMsg{seq: 1, snapshot: snapshot, history: history})
	got := feedCmd(next.(model), cmd)
	if len(asyncReads) != 1 || asyncReads[0].pane != "w2:p1" {
		t.Fatalf("first snapshot reads = %+v, want one read of w2:p1", asyncReads)
	}
	if !asyncReads[0].hedged {
		t.Fatal("first preview read after the snapshot was not hedged")
	}
	if !got.previewTextLive || got.previewText != "frame:w2:p1" {
		t.Fatalf("first preview not painted: live=%v text=%q", got.previewTextLive, got.previewText)
	}

	// Sync path: a model built with the snapshot in hand boots straight into its first read.
	var syncReads []read
	m = newModel("spaces", snapshot, history, defaultSidebarLayout(), 0, 40)
	m.width, m.height = 120, 30
	m.readPane = record(&syncReads)
	next, cmd = m.Update(bootMsg{})
	_ = feedCmd(next.(model), cmd)
	if len(syncReads) != 1 || !syncReads[0].hedged {
		t.Fatalf("boot reads = %+v, want one hedged read", syncReads)
	}
}
