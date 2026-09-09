package picker

import (
	"strings"
	"testing"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/termtext"
)

func TestOracleSelectedRowIsVisible(t *testing.T) {
	m := model{selectedID: "last", visible: []Item{
		{ID: "first", Rows: []string{"first", "detail", "detail"}},
		{ID: "last", Rows: []string{"SELECTED_LAST"}},
	}}
	if got := m.renderList(30, 3); !strings.Contains(got, "SELECTED_LAST") {
		t.Fatalf("off-screen selection is invisible: %q", got)
	}
}

func TestOraclePreviewRejectsC1Controls(t *testing.T) {
	input := "text\u009b2J\u009d52;c;YQ==\u009ctail"
	got := termtext.KeepSGR(input)
	for _, r := range got {
		if r >= 0x80 && r <= 0x9f {
			t.Fatalf("C1 terminal control survives: U+%04X in %q", r, got)
		}
	}
}

func TestOracleReplacementCannotReuseSelectedID(t *testing.T) {
	h := focus.EmptyHistory(herdr.ContinuityWitness{})
	h = focus.ApplyVerifiedOccupantTransition(h, "w1:p1", "pi", "a", false)
	old, _ := focus.CurrentAgentLiveID(h, "w1:p1")
	h = focus.ApplyVerifiedOccupantTransition(h, "w1:p1", "pi", "a", true)
	h = focus.ApplyVerifiedOccupantTransition(h, "w1:p1", "pi", "b", false)
	next, _ := focus.CurrentAgentLiveID(h, "w1:p1")
	if old.String() == next.String() {
		t.Fatalf("replacement reuses selected target id %q", next.String())
	}
}

func TestOracleSnapshotReplacementDropsHistory(t *testing.T) {
	h := focus.EmptyHistory(herdr.ContinuityWitness{})
	h = focus.ApplyVerifiedOccupantTransition(h, "w1:p1", "pi", "old", false)
	h = focus.RecordAgentPaneFocus(h, "w1:p1")
	snapshot := herdr.SessionSnapshot{Agents: []herdr.AgentRow{{PaneRow: herdr.PaneRow{PaneID: "w1:p1", Agent: "pi", AgentSession: &herdr.AgentSession{Value: "new"}}}}}
	h = focus.Prune(h, snapshot)
	if len(h.Agents) > 0 {
		t.Fatalf("replacement inherits old history: %+v", h.Agents)
	}
}

func TestOracleEventEnvelopePreservesLifecycleData(t *testing.T) {
	_, data, err := herdr.ParsePluginEventJSON(`{"event":"pane_agent_detected","data":{"type":"pane_agent_detected","pane_id":"w1:p1","workspace_id":"w1","agent":"pi","released":true}}`)
	if err != nil {
		t.Fatal(err)
	}
	if data.Agent != "pi" || !data.Released || herdr.PluginEventPaneID(data) != "w1:p1" {
		t.Fatalf("real envelope loses lifecycle data: %+v", data)
	}
}
