package picker

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
	"github.com/regutierrez/hseh/internal/space"
)

func TestUnopenedDefinitionListedUntilAssociated(t *testing.T) {
	work := t.TempDir()
	def := space.Definition{ID: "def-list", Name: "listed", ResolvedDir: work, WorkingDir: work, Description: "desc"}
	snapshot := herdr.SessionSnapshot{Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w1", Label: "live"}}}
	items := AppendUnopenedDefinitionItems(BuildItemsWithLayout("spaces", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout()), "spaces", snapshot, []space.Definition{def}, nil, nil)
	if len(items) != 2 || items[1].Kind != KindDefinition {
		t.Fatalf("%+v", items)
	}
	records := []space.AssociationRecord{{DefinitionID: "def-list", ResolvedDir: work, WorkspaceID: "w1"}}
	hidden := AppendUnopenedDefinitionItems(BuildItemsWithLayout("spaces", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout()), "spaces", snapshot, []space.Definition{def}, records, nil)
	if len(hidden) != 1 {
		t.Fatalf("associated definition still listed: %+v", hidden)
	}
	if items[0].ID == items[1].ID {
		t.Fatalf("selection ids collided: %+v", items)
	}
}

func TestDefinitionPickerSanitizesDisplayNotCommand(t *testing.T) {
	def := space.Definition{
		ID: "w1", Name: "n\x1b[31mX", Description: "d\x1b]0;title\x07",
		ResolvedDir: "/tmp/x", Tabs: []space.DefinitionTab{{Name: "t", Command: "echo \x1b[31mKEEP"}},
	}
	item := definitionItem(def, false)
	if strings.Contains(item.Rows[0], "\x1b") || strings.Contains(item.PreviewText, "\x1b") {
		t.Fatalf("display still has controls: %+v %q", item.Rows, item.PreviewText)
	}
	if def.Tabs[0].Command != "echo \x1b[31mKEEP" {
		t.Fatalf("execution command rewritten: %q", def.Tabs[0].Command)
	}
	if item.DefinitionID != "w1" || item.ID == "w1" {
		t.Fatalf("need namespaced selection id: %+v", item)
	}
}

func TestRepeatedEnterCreatesOnce(t *testing.T) {
	work := t.TempDir()
	snapshot := herdr.SessionSnapshot{Version: "0.9.0", Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w0", Label: "other"}}}
	m := newModel("spaces", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout(), 0, 80)
	m.setSpaceCatalog([]space.Definition{{ID: "def-once", Name: "once", ResolvedDir: work, WorkingDir: work, Tabs: []space.DefinitionTab{{Name: "main", Command: "echo X"}}}}, nil, nil, nil)
	m.selectedID = SelectionID(KindDefinition, "def-once")
	first := m.startAccept()
	if first == nil {
		t.Fatal("first enter")
	}
	second := m.startAccept()
	if second != nil {
		t.Fatal("second enter started another accept")
	}
}

func TestPickerEscapeCancelsDefinitionAccept(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	hsehtest.WriteDefinition(t, config, "def-esc", "esc", work, "echo NO")
	snapshot := herdr.SessionSnapshot{Version: "0.9.0"}
	h := &hsehtest.Server{Snapshot: snapshot, SnapshotDelay: 400 * time.Millisecond}
	socket, state := hsehtest.Start(t, h)
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", config)
	t.Setenv("HERDR_SESSION", "hseh-test")
	m := newModel("spaces", snapshot, focus.EmptyHistory(herdr.ContinuityWitness{}), defaultSidebarLayout(), 0, 80)
	m.setSpaceCatalog([]space.Definition{{ID: "def-esc", Name: "esc", ResolvedDir: work, WorkingDir: work, Tabs: []space.DefinitionTab{{Name: "main"}}}}, nil, nil, nil)
	m.selectedID = SelectionID(KindDefinition, "def-esc")
	cmd := m.startAccept()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	time.Sleep(30 * time.Millisecond)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	msg := <-done
	if _, ok := msg.(errorMsg); !ok && msg != nil {
		if _, ok := msg.(acceptedMsg); ok {
			t.Fatal("escape accepted create")
		}
	}
	if len(h.Created()) != 0 {
		t.Fatalf("escape created %v", h.Created())
	}
}

func TestDefinitionPickerShowsRecoveryCommands(t *testing.T) {
	def := space.Definition{ID: "def-rec", Name: "rec", ResolvedDir: "/tmp/x"}
	item := definitionItem(def, true)
	joined := strings.Join(item.Rows, "\n")
	if !strings.Contains(joined, "recovery needed") {
		t.Fatalf("rows %v", item.Rows)
	}
	if !strings.Contains(joined, "hseh recover def-rec --workspace <live-workspace-id>") || !strings.Contains(item.PreviewText, "hseh recover def-rec --create") {
		t.Fatalf("commands missing: rows=%v preview=%s", item.Rows, item.PreviewText)
	}
}
