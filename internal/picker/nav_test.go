package picker

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
)

func TestQueryRetainedAcrossViewsAndClearedOnRebuild(t *testing.T) {
	snapshot := herdr.SessionSnapshot{
		Workspaces: []herdr.WorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha"},
			{WorkspaceID: "w2", Label: "beta"},
		},
		Agents: []herdr.AgentRow{{PaneRow: herdr.PaneRow{PaneID: "w1:p1", Agent: "pi", DisplayAgent: "beta-bot", AgentStatus: "idle"}}},
	}
	m := newModel("spaces", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout(), 0, 80)
	m.query = "beta"
	m.applyQuery()
	if len(m.visible) != 1 || m.visible[0].WorkspaceID != "w2" {
		t.Fatalf("spaces query: %+v", m.visible)
	}
	m.cycleView(1)
	if m.view != ViewAgents {
		t.Fatalf("view %s", m.view)
	}
	if m.query != "beta" {
		t.Fatalf("query not kept: %q", m.query)
	}
	if len(m.visible) != 1 || m.visible[0].Kind != KindAgent {
		t.Fatalf("agents query: %+v", m.visible)
	}
	m.query = ""
	m.applyQuery()
	if len(m.visible) != 1 {
		t.Fatalf("cleared query should show all agents, got %d", len(m.visible))
	}
}

func TestPreviewDoesNotChangePreselectHistory(t *testing.T) {
	history := focus.EmptyHistory(herdr.ContinuityWitness{})
	history.Spaces = []string{"w2", "w1"}
	snapshot := herdr.SessionSnapshot{
		FocusedWorkspaceID: "w1",
		Workspaces: []herdr.WorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha"},
			{WorkspaceID: "w2", Label: "beta"},
		},
	}
	m := newModel("spaces", snapshot, history, defaultSidebarLayout(), 0, 40)
	if m.selectedID != SelectionID(KindSpace, "w2") {
		t.Fatalf("preselect %s", m.selectedID)
	}
	m.previewText = "ticks"
	if m.history.Spaces[0] != "w2" {
		t.Fatalf("preview mutated MRU: %v", m.history.Spaces)
	}
}

func TestNarrowStopsPreviewReadsAndWideResumes(t *testing.T) {
	snapshot := herdr.SessionSnapshot{Version: "0.9.0", Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w2", Label: "beta", ActiveTabID: "w2:t1"}}, Layouts: []herdr.PaneLayout{{TabID: "w2:t1", FocusedPaneID: "w2:p1"}}}
	srv := &hsehtest.Server{Snapshot: snapshot, PaneReadText: "tick"}
	socket, state := hsehtest.Start(t, srv)
	reads := &srv.Reads
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	t.Setenv("HERDR_SESSION", "hseh-test")
	m := model{widePreviewMinCols: 40, previewLoadingDelay: time.Millisecond, snapshotReady: true, selectedID: "w2", visible: []Item{{Kind: KindAgent, ID: "w2", PreviewPane: "w2:p1"}}}
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	got := next.(model)
	if cmd == nil {
		t.Fatal("expected wide preview read")
	}
	got = feedCmd(got, cmd)
	if reads.Load() != 1 {
		t.Fatalf("wide reads %d", reads.Load())
	}
	// Too short for a stacked preview: the preview is hidden and reads stop.
	next, cmd = got.Update(tea.WindowSizeMsg{Width: 30, Height: 6})
	got = next.(model)
	if got.showsPreview() {
		t.Fatal("6-row popup still shows a preview")
	}
	if cmd != nil || got.startPreview(false) != nil || got.startPreview(true) != nil {
		t.Fatal("hidden preview started a read")
	}
	if got.previewText != "tick" {
		t.Fatalf("hidden preview dropped the last frame: %q", got.previewText)
	}
	if reads.Load() != 1 {
		t.Fatalf("narrow extra reads %d", reads.Load())
	}
	next, cmd = got.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	got = next.(model)
	if cmd == nil {
		t.Fatal("expected resume preview read")
	}
	got = feedCmd(got, cmd)
	if reads.Load() != 2 {
		t.Fatalf("resume reads %d", reads.Load())
	}
}

func TestEnterFocusesSelectedWorkspace(t *testing.T) {
	snapshot := herdr.SessionSnapshot{
		Version:            "0.9.0",
		FocusedWorkspaceID: "w1",
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
	m := newModel("spaces", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{SocketPath: socket, PeerPID: 1, PeerStartTime: "1"}), defaultSidebarLayout(), 0, 80)
	m.selectedID = SelectionID(KindSpace, "w2")
	cmd := m.startAccept()
	if cmd == nil {
		t.Fatal("expected accept")
	}
	msg := cmd()
	if _, ok := msg.(acceptedMsg); !ok {
		t.Fatalf("got %#v", msg)
	}
	if focused.Load() != "w2" {
		t.Fatalf("focused %v", focused.Load())
	}
}
