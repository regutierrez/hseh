package picker

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/space"
)

func closeSpaces() model {
	m := newModel(ViewSpaces, herdr.SessionSnapshot{
		FocusedWorkspaceID: "w1",
		Workspaces: []herdr.WorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha"},
			{WorkspaceID: "w2", Label: "beta"},
			{WorkspaceID: "w3", Label: "gamma"},
		},
	}, focus.EmptyHistory(herdr.ContinuityWitness{}), 80)
	m.width, m.height = 80, 24
	return m
}

func TestCatalogOmitsCurrentTargets(t *testing.T) {
	spaces := closeSpaces()
	if hasItemID(spaces.visible, selectionID(KindSpace, "w1")) {
		t.Fatal("current space listed")
	}
	listed := assembleItems(ViewSpaces, liveState{snapshot: spaces.snapshot, history: spaces.history}, defaultSidebarLayout(), nil, nil)
	if !hasItemID(listed, selectionID(KindSpace, "w1")) {
		t.Fatal("list JSON dropped current space")
	}

	agents := newModel(ViewAgents, herdr.SessionSnapshot{
		FocusedPaneID: "w1:p1",
		Agents: []herdr.AgentRow{
			{PaneRow: herdr.PaneRow{PaneID: "w1:p1", WorkspaceID: "w1", Agent: "pi", AgentStatus: "idle"}},
			{PaneRow: herdr.PaneRow{PaneID: "w1:p2", WorkspaceID: "w1", Agent: "pi", AgentStatus: "idle"}},
		},
	}, focus.EmptyHistory(herdr.ContinuityWitness{}), 80)
	if hasItemID(agents.visible, selectionID(KindAgent, "w1:p1#1")) {
		t.Fatal("current agent listed")
	}
}

func TestCloseKeys(t *testing.T) {
	var closed []string
	track := func(_ context.Context, item Item) error {
		closed = append(closed, item.Kind)
		return nil
	}

	m := closeSpaces()
	m.closeItem = track
	m.selectedID = selectionID(KindSpace, "w2")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = next.(model)
	if cmd != nil || !m.pendingClose {
		t.Fatal("ctrl+x should arm")
	}

	for _, key := range []tea.KeyMsg{{Type: tea.KeyDelete}, {Type: tea.KeyUp}, {Type: tea.KeyRunes, Runes: []rune{'x'}}} {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
		m = next.(model)
		next, _ = m.Update(key)
		m = next.(model)
		if m.pendingClose {
			t.Fatalf("%v left confirm armed", key)
		}
	}
	if len(closed) != 0 {
		t.Fatalf("cancel called close: %v", closed)
	}

	m = closeSpaces()
	m.closeItem = track
	m.selectedID = selectionID(KindSpace, "w2")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	next, cmd = next.(model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = feedCmd(next.(model), cmd)
	if m.quitting || hasItemID(m.visible, selectionID(KindSpace, "w2")) || len(closed) != 1 || closed[0] != KindSpace {
		t.Fatalf("space close: quitting=%v closed=%v", m.quitting, closed)
	}

	closed = nil
	agents := newModel(ViewAgents, herdr.SessionSnapshot{
		FocusedPaneID: "w1:p1",
		Agents: []herdr.AgentRow{
			{PaneRow: herdr.PaneRow{PaneID: "w1:p1", WorkspaceID: "w1", Agent: "pi", AgentStatus: "idle"}},
			{PaneRow: herdr.PaneRow{PaneID: "w1:p2", WorkspaceID: "w1", Agent: "pi", AgentStatus: "idle"}},
		},
	}, focus.EmptyHistory(herdr.ContinuityWitness{}), 80)
	agents.closeItem = track
	agents.selectedID = selectionID(KindAgent, "w1:p2#1")
	next, _ = agents.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	next, cmd = next.(model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := feedCmd(next.(model), cmd)
	if got.quitting || len(closed) != 1 || closed[0] != KindAgent {
		t.Fatalf("agent close: quitting=%v closed=%v", got.quitting, closed)
	}

	closed = nil
	m = closeSpaces()
	m.closeItem = track
	m.setSpaceCatalog([]space.Definition{{ID: "def-t", Name: "tmpl", ResolvedDir: t.TempDir(), WorkingDir: t.TempDir()}}, nil, nil, nil)
	m.selectedID = selectionID(KindDefinition, "def-t")
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if next.(model).pendingClose || cmd != nil || len(closed) != 0 {
		t.Fatal("ctrl+x on template")
	}

	m = closeSpaces()
	m.closeItem = track
	m.selectedID = selectionID(KindSpace, "w2")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	next, cmd = next.(model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !next.(model).quitting || cmd == nil || len(closed) != 0 {
		t.Fatal("escape closed a space")
	}

	m = closeSpaces()
	m.closeItem = func(context.Context, Item) error { return errors.New("nope") }
	m.selectedID = selectionID(KindSpace, "w2")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	next, cmd = next.(model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = feedCmd(next.(model), cmd)
	if got.quitting || got.statusErr != "nope" || !hasItemID(got.visible, selectionID(KindSpace, "w2")) {
		t.Fatalf("error: quitting=%v status=%q", got.quitting, got.statusErr)
	}
}
