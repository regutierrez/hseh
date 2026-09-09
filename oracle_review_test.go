package main

import (
	"strings"
	"testing"
)

func TestOracleSelectedRowIsVisible(t *testing.T) {
	m := pickerModel{selectedID: "last", visible: []PickerItem{
		{ID: "first", Rows: []string{"first", "detail", "detail"}},
		{ID: "last", Rows: []string{"SELECTED_LAST"}},
	}}
	if got := m.renderList(30, 3); !strings.Contains(got, "SELECTED_LAST") {
		t.Fatalf("off-screen selection is invisible: %q", got)
	}
}

func TestOraclePreviewRejectsC1Controls(t *testing.T) {
	input := "text\u009b2J\u009d52;c;YQ==\u009ctail"
	got := AllowVisiblePreviewANSI(input)
	for _, r := range got {
		if r >= 0x80 && r <= 0x9f {
			t.Fatalf("C1 terminal control survives: U+%04X in %q", r, got)
		}
	}
}

func TestOracleReplacementCannotReuseSelectedID(t *testing.T) {
	h := emptyFocusHistory(ServerContinuityWitness{})
	h = ApplyVerifiedOccupantTransition(h, "w1:p1", "pi", "a", false)
	old, _ := currentAgentLiveID(h, "w1:p1")
	h = ApplyVerifiedOccupantTransition(h, "w1:p1", "pi", "a", true)
	h = ApplyVerifiedOccupantTransition(h, "w1:p1", "pi", "b", false)
	next, _ := currentAgentLiveID(h, "w1:p1")
	if old.String() == next.String() {
		t.Fatalf("replacement reuses selected target id %q", next.String())
	}
}

func TestOracleSnapshotReplacementDropsHistory(t *testing.T) {
	h := emptyFocusHistory(ServerContinuityWitness{})
	h = ApplyVerifiedOccupantTransition(h, "w1:p1", "pi", "old", false)
	h = RecordAgentPaneFocus(h, "w1:p1")
	snapshot := HerdrSessionSnapshot{Agents: []HerdrAgentRow{{HerdrPaneRow: HerdrPaneRow{PaneID: "w1:p1", Agent: "pi", AgentSession: &HerdrAgentSession{Value: "new"}}}}}
	h = pruneFocusHistory(h, snapshot)
	if len(h.Agents) > 0 {
		t.Fatalf("replacement inherits old history: %+v", h.Agents)
	}
}

func TestOracleEventEnvelopePreservesLifecycleData(t *testing.T) {
	_, data, err := parsePluginEventJSON(`{"event":"pane_agent_detected","data":{"type":"pane_agent_detected","pane_id":"w1:p1","workspace_id":"w1","agent":"pi","released":true}}`)
	if err != nil {
		t.Fatal(err)
	}
	if data.Agent != "pi" || !data.Released || pluginEventPaneID(data) != "w1:p1" {
		t.Fatalf("real envelope loses lifecycle data: %+v", data)
	}
}
