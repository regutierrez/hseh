package picker

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
	"github.com/regutierrez/hseh/internal/space"
	"github.com/regutierrez/hseh/internal/termtext"
)

func spacesForClose() (herdr.SessionSnapshot, model) {
	snapshot := herdr.SessionSnapshot{
		FocusedWorkspaceID: "w1",
		Workspaces: []herdr.WorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha", ActiveTabID: "w1:t1"},
			{WorkspaceID: "w2", Label: "beta", ActiveTabID: "w2:t1"},
			{WorkspaceID: "w3", Label: "gamma", ActiveTabID: "w3:t1"},
		},
		Agents: []herdr.AgentRow{{PaneRow: herdr.PaneRow{
			PaneID: "w2:p1", WorkspaceID: "w2", TabID: "w2:t1", Agent: "pi", DisplayAgent: "beta-bot", AgentStatus: "idle",
		}}},
	}
	m := newModel(ViewSpaces, snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), 80)
	m.width, m.height = 80, 24
	return snapshot, m
}

func agentsForClose() (herdr.SessionSnapshot, model) {
	snapshot := herdr.SessionSnapshot{
		FocusedPaneID: "w1:p1",
		Agents: []herdr.AgentRow{
			{PaneRow: herdr.PaneRow{PaneID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Agent: "pi", DisplayAgent: "here", AgentStatus: "idle"}},
			{PaneRow: herdr.PaneRow{PaneID: "w1:p2", WorkspaceID: "w1", TabID: "w1:t2", Agent: "pi", DisplayAgent: "other", AgentStatus: "idle"}},
			{PaneRow: herdr.PaneRow{PaneID: "w1:p3", WorkspaceID: "w1", TabID: "w1:t3", Agent: "pi", DisplayAgent: "third", AgentStatus: "idle"}},
		},
	}
	m := newModel(ViewAgents, snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), 80)
	m.width, m.height = 80, 24
	return snapshot, m
}

func pointCloseAt(t *testing.T, socket, state string) {
	t.Helper()
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	t.Setenv("HERDR_SESSION", "hseh-test")
}

func TestCatalogOmitsCurrentSpace(t *testing.T) {
	snapshot, m := spacesForClose()
	if hasItemID(m.visible, selectionID(KindSpace, "w1")) || hasItemID(m.allItems, selectionID(KindSpace, "w1")) {
		t.Fatalf("current space still listed: %+v", idsOf(m.visible))
	}
	if len(m.visible) != 2 || m.visible[0].WorkspaceID != "w2" || m.visible[1].WorkspaceID != "w3" {
		t.Fatalf("visible spaces %+v", idsOf(m.visible))
	}
	live := liveState{snapshot: snapshot, history: m.history}
	listed := assembleItems(ViewSpaces, live, defaultSidebarLayout(), nil, nil)
	if !hasItemID(listed, selectionID(KindSpace, "w1")) {
		t.Fatal("list JSON path must still include the current space")
	}
}

func TestCatalogOmitsCurrentAgent(t *testing.T) {
	snapshot, m := agentsForClose()
	if hasItemID(m.visible, selectionID(KindAgent, "w1:p1#1")) || hasItemID(m.allItems, selectionID(KindAgent, "w1:p1#1")) {
		t.Fatalf("current agent still listed: %+v", idsOf(m.visible))
	}
	if len(m.visible) != 2 || m.visible[0].PaneID != "w1:p2" || m.visible[1].PaneID != "w1:p3" {
		t.Fatalf("visible agents %+v", idsOf(m.visible))
	}
	live := liveState{snapshot: snapshot, history: m.history}
	listed := assembleItems(ViewAgents, live, defaultSidebarLayout(), nil, nil)
	if !hasItemID(listed, selectionID(KindAgent, "w1:p1#1")) {
		t.Fatal("list JSON path must still include the current agent")
	}
}

func TestCtrlXArmsCloseOnLiveSpace(t *testing.T) {
	_, m := spacesForClose()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	got := next.(model)
	if cmd != nil || !got.pendingClose {
		t.Fatalf("ctrl+x should arm confirm: pending=%v cmd=%v", got.pendingClose, cmd != nil)
	}
	plain := termtext.StripControls(got.View())
	if !strings.Contains(plain, closeConfirmHint) || !strings.Contains(plain, closeConfirmMark) {
		t.Fatalf("confirm affordance missing: %q", plain)
	}
}

func TestEnterConfirmsWorkspaceCloseWithoutQuit(t *testing.T) {
	snapshot, m := spacesForClose()
	srv := &hsehtest.Server{Snapshot: snapshot}
	socket, state := hsehtest.Start(t, srv)
	pointCloseAt(t, socket, state)
	m.selectedID = selectionID(KindSpace, "w2")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = next.(model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if cmd == nil {
		t.Fatal("enter did not start close")
	}
	msg := cmd()
	closed, ok := msg.(closedMsg)
	if !ok {
		t.Fatalf("got %#v", msg)
	}
	if closed.item.WorkspaceID != "w2" {
		t.Fatalf("closed %+v", closed.item)
	}
	next, _ = m.Update(closed)
	m = next.(model)
	if m.quitting || m.pendingClose {
		t.Fatalf("close quit or left confirm armed: quitting=%v pending=%v", m.quitting, m.pendingClose)
	}
	if hasItemID(m.visible, selectionID(KindSpace, "w2")) {
		t.Fatalf("closed space still listed: %+v", idsOf(m.visible))
	}
	if m.selectedID != selectionID(KindSpace, "w3") {
		t.Fatalf("selection after close %q", m.selectedID)
	}
	if srv.Count("workspace.close") != 1 || srv.Count("tab.close") != 0 {
		t.Fatalf("methods %v", srv.Methods())
	}
	if got := srv.ClosedWorkspaces(); len(got) != 1 || got[0] != "w2" {
		t.Fatalf("closed workspaces %v", got)
	}
}

func TestEnterConfirmsPaneCloseWithoutQuit(t *testing.T) {
	snapshot, m := agentsForClose()
	srv := &hsehtest.Server{Snapshot: snapshot}
	socket, state := hsehtest.Start(t, srv)
	pointCloseAt(t, socket, state)
	m.selectedID = selectionID(KindAgent, "w1:p2#1")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = next.(model)
	if !m.pendingClose {
		t.Fatal("ctrl+x did not arm agent close")
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if cmd == nil {
		t.Fatal("enter did not start close")
	}
	msg := cmd()
	closed, ok := msg.(closedMsg)
	if !ok {
		t.Fatalf("got %#v", msg)
	}
	if closed.item.PaneID != "w1:p2" {
		t.Fatalf("closed %+v", closed.item)
	}
	next, _ = m.Update(closed)
	m = next.(model)
	if m.quitting {
		t.Fatal("pane close quit the picker")
	}
	if hasItemID(m.visible, selectionID(KindAgent, "w1:p2#1")) {
		t.Fatalf("closed agent still listed: %+v", idsOf(m.visible))
	}
	if srv.Count("pane.close") != 1 || srv.Count("tab.close") != 0 {
		t.Fatalf("methods %v", srv.Methods())
	}
	if got := srv.ClosedPanes(); len(got) != 1 || got[0] != "w1:p2" {
		t.Fatalf("closed panes %v", got)
	}
}

func TestDeleteCancelsPendingClose(t *testing.T) {
	_, m := spacesForClose()
	var calls int
	m.closeItem = func(context.Context, Item) error {
		calls++
		return nil
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = next.(model)
	selected := m.selectedID
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDelete})
	m = next.(model)
	if m.pendingClose || cmd != nil || m.selectedID != selected || m.view != ViewSpaces {
		t.Fatalf("delete should cancel in place: pending=%v selected=%q view=%s", m.pendingClose, m.selectedID, m.view)
	}
	if calls != 0 {
		t.Fatal("delete called herdr close")
	}
}

func TestNavigationAndQueryCancelPendingClose(t *testing.T) {
	keys := []tea.KeyMsg{
		{Type: tea.KeyUp},
		{Type: tea.KeyDown},
		{Type: tea.KeyTab},
		{Type: tea.KeyShiftTab},
		{Type: tea.KeyRunes, Runes: []rune{'b'}},
	}
	for _, key := range keys {
		_, m := spacesForClose()
		var calls int
		m.closeItem = func(context.Context, Item) error {
			calls++
			return nil
		}
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
		m = next.(model)
		if !m.pendingClose {
			t.Fatal("not armed")
		}
		next, _ = m.Update(key)
		got := next.(model)
		if got.pendingClose {
			t.Fatalf("%v kept confirm armed", key)
		}
		if calls != 0 {
			t.Fatalf("%v called close", key)
		}
	}
}

func TestCtrlXOnDefinitionIsNoop(t *testing.T) {
	work := t.TempDir()
	snapshot := herdr.SessionSnapshot{FocusedWorkspaceID: "w1", Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w1", Label: "live"}}}
	m := newModel(ViewSpaces, snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), 80)
	m.setSpaceCatalog([]space.Definition{{ID: "def-t", Name: "tmpl", ResolvedDir: work, WorkingDir: work}}, nil, nil, nil)
	m.width, m.height = 80, 24
	m.selectedID = selectionID(KindDefinition, "def-t")
	var calls int
	m.closeItem = func(context.Context, Item) error {
		calls++
		return nil
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	got := next.(model)
	if got.pendingClose || cmd != nil || calls != 0 {
		t.Fatalf("ctrl+x on template armed or closed: pending=%v cmd=%v calls=%d", got.pendingClose, cmd != nil, calls)
	}
}

func TestCloseErrorStaysInPickerFooter(t *testing.T) {
	snapshot, m := spacesForClose()
	srv := &hsehtest.Server{Snapshot: snapshot, FailMethod: "workspace.close"}
	socket, state := hsehtest.Start(t, srv)
	pointCloseAt(t, socket, state)
	m.selectedID = selectionID(KindSpace, "w2")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = next.(model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	msg := cmd()
	errMsg, ok := msg.(errorMsg)
	if !ok {
		t.Fatalf("got %#v", msg)
	}
	next, _ = m.Update(errMsg)
	got := next.(model)
	if got.quitting || got.pendingClose {
		t.Fatalf("error quit or left confirm: quitting=%v pending=%v", got.quitting, got.pendingClose)
	}
	if !hasItemID(got.visible, selectionID(KindSpace, "w2")) {
		t.Fatal("failed close dropped the row")
	}
	plain := termtext.StripControls(got.View())
	if !strings.Contains(got.statusErr, "workspace.close") || !strings.Contains(plain, "workspace.close") {
		t.Fatalf("close error missing from footer: status=%q view=%q", got.statusErr, plain)
	}
}

func TestEscapeWhilePendingCloseQuitsWithoutClosing(t *testing.T) {
	_, m := spacesForClose()
	var calls int
	m.closeItem = func(context.Context, Item) error {
		calls++
		return nil
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = next.(model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(model)
	if !got.quitting || cmd == nil || got.pendingClose || calls != 0 {
		t.Fatalf("escape while confirming: quitting=%v pending=%v calls=%d", got.quitting, got.pendingClose, calls)
	}
}

func TestMouseSelectionCancelsPendingClose(t *testing.T) {
	_, m := spacesForClose()
	var calls int
	m.closeItem = func(context.Context, Item) error {
		calls++
		return nil
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = next.(model)
	layout := m.buildListLayout(m.frame().listW, m.frame().listH)
	y := -1
	want := selectionID(KindSpace, "w3")
	for i, id := range layout.ItemIDs {
		if id == want {
			y = i
			break
		}
	}
	if y < 0 {
		t.Fatalf("w3 not in layout %+v", layout.ItemIDs)
	}
	next, _ = m.Update(tea.MouseMsg{X: 1, Y: tabRows + y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	got := next.(model)
	if got.pendingClose || got.selectedID != want || calls != 0 {
		t.Fatalf("mouse cancel: pending=%v selected=%q calls=%d", got.pendingClose, got.selectedID, calls)
	}
}

func TestStubCloseErrorUsesAcceptErr(t *testing.T) {
	_, m := spacesForClose()
	m.closeItem = func(context.Context, Item) error {
		return errors.New("injected workspace.close")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = next.(model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	next, _ = m.Update(cmd())
	got := next.(model)
	if got.quitting || !strings.Contains(got.statusErr, "injected workspace.close") {
		t.Fatalf("stub error: quitting=%v status=%q", got.quitting, got.statusErr)
	}
}

func idsOf(items []Item) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}
