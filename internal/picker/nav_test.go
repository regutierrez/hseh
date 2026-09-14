package picker

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
)

func TestApplyQuerySelectsFirstHit(t *testing.T) {
	m := model{
		allItems: []Item{
			{ID: "best", SearchText: "zzz"},
			{ID: "keep", SearchText: "zzz"},
		},
		selectedID: "keep",
		query:      "zzz",
	}
	m.applyQuery()
	if len(m.visible) != 2 || m.visible[0].ID != "best" {
		t.Fatalf("visible %+v", m.visible)
	}
	if m.selectedID != "best" {
		t.Fatalf("selected %q, want first hit", m.selectedID)
	}
	m.query = ""
	m.applyQuery()
	if m.selectedID != "best" {
		t.Fatalf("empty query dropped first hit: %q", m.selectedID)
	}
}

func TestQueryRetainedAcrossViewsAndClearedOnRebuild(t *testing.T) {
	snapshot := herdr.SessionSnapshot{
		Workspaces: []herdr.WorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha"},
			{WorkspaceID: "w2", Label: "beta"},
		},
		Agents: []herdr.AgentRow{{PaneRow: herdr.PaneRow{PaneID: "w1:p1", Agent: "pi", DisplayAgent: "beta-bot", AgentStatus: "idle"}}},
	}
	m := newModel("spaces", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), 80)
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

func TestTypingFiltersImmediately(t *testing.T) {
	snapshot := herdr.SessionSnapshot{
		Workspaces: []herdr.WorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha"},
			{WorkspaceID: "w2", Label: "beta"},
		},
		Agents: []herdr.AgentRow{{PaneRow: herdr.PaneRow{PaneID: "w1:p1", Agent: "pi", DisplayAgent: "beta-bot", AgentStatus: "idle"}}},
	}
	m := newModel("spaces", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), 80)
	m.width, m.height = 80, 24
	preselected := m.selectedID
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = next.(model)
	if m.query != "b" || len(m.visible) != 1 || m.visible[0].WorkspaceID != "w2" {
		t.Fatalf("letter did not search: query=%q visible=%+v", m.query, m.visible)
	}
	if !strings.Contains(m.View(), searchCaret) {
		t.Fatal("search box missing text pointer")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	if m.view != ViewAgents || m.query != "b" {
		t.Fatalf("tab while searching: view=%s query=%q", m.view, m.query)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(model)
	if m.query != "" {
		t.Fatalf("backspace query: %q", m.query)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	if m.selectedID == "" || m.selectedID == preselected {
		t.Fatalf("search did not select a non-preselected row: selected=%q preselect=%q", m.selectedID, preselected)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if !m.quitting || cmd == nil {
		t.Fatal("esc did not quit")
	}
	m = newModel("spaces", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), 80)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(model)
	if !m.quitting || cmd == nil {
		t.Fatal("ctrl+c did not quit")
	}
}

func TestNarrowStopsPreviewReadsAndWideResumes(t *testing.T) {
	snapshot := herdr.SessionSnapshot{Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w2", Label: "beta", ActiveTabID: "w2:t1"}}, Layouts: []herdr.PaneLayout{{TabID: "w2:t1", FocusedPaneID: "w2:p1"}}}
	srv := &hsehtest.Server{Snapshot: snapshot, PaneReadText: "tick"}
	socket, state := hsehtest.Start(t, srv)
	reads := &srv.Reads
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	t.Setenv("HERDR_SESSION", "hseh-test")
	m := model{widePreviewMinCols: 40, previewLoadingDelay: time.Millisecond, snapshotReady: true, selectedID: "w2", visible: []Item{{Kind: KindAgent, ID: "w2", PaneID: "w2:p1"}}}
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

func TestEnterFocusesSelectedAgentTab(t *testing.T) {
	snapshot := herdr.SessionSnapshot{
		FocusedWorkspaceID: "w1",
		Workspaces: []herdr.WorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha", ActiveTabID: "w1:t1"},
		},
		Agents: []herdr.AgentRow{{PaneRow: herdr.PaneRow{
			PaneID:       "w1:p1",
			TabID:        "w1:t1",
			WorkspaceID:  "w1",
			Agent:        "pi",
			DisplayAgent: "pi",
			AgentStatus:  "idle",
		}}},
	}
	srv := &hsehtest.Server{Snapshot: snapshot}
	socket, state := hsehtest.Start(t, srv)
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	m := newModel("agents", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{SocketPath: socket, PeerPID: 1, PeerStartTime: "1"}), 80)
	m.selectedID = selectionID(KindAgent, "w1:p1#1")
	cmd := m.startAccept()
	if cmd == nil {
		t.Fatal("expected accept")
	}
	msg := cmd()
	if _, ok := msg.(acceptedMsg); !ok {
		t.Fatalf("got %#v", msg)
	}
	if focusedTab, _ := srv.FocusedTab.Load().(string); focusedTab != "w1:t1" {
		t.Fatalf("focused tab %v", focusedTab)
	}
	if srv.Count("agent.focus") != 0 || srv.Count("tab.focus") != 1 {
		t.Fatalf("methods %v", srv.Methods())
	}
}

func TestEnterFocusesSelectedWorkspace(t *testing.T) {
	snapshot := herdr.SessionSnapshot{
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
	m := newModel("spaces", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{SocketPath: socket, PeerPID: 1, PeerStartTime: "1"}), 80)
	m.selectedID = selectionID(KindSpace, "w2")
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
