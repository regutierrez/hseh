package main

import (
	"testing"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
	"github.com/regutierrez/hseh/internal/picker"
)

func TestVerifiedMoveAfterSnapshotKeepsPublicMRU(t *testing.T) {
	snapshot := herdr.SessionSnapshot{
		Version: "0.9.0",
		Agents: []herdr.AgentRow{
			{PaneRow: herdr.PaneRow{PaneID: "w2:p9", Agent: "pi", AgentSession: &herdr.AgentSession{Value: "a"}, AgentStatus: "idle"}},
		},
	}
	socket, stateDir := hsehtest.Start(t, &hsehtest.Server{Snapshot: snapshot})
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
	t.Setenv("HERDR_SESSION", "hseh-test")
	witness, err := herdr.ReadContinuityWitness()
	if err != nil {
		t.Fatal(err)
	}
	history := focus.EmptyHistory(witness)
	history = focus.ApplyVerifiedOccupantTransition(history, "w1:p1", "pi", "a", false)
	history = focus.RecordAgentPaneFocus(history, "w1:p1")
	old, _ := focus.CurrentAgentLiveID(history, "w1:p1")
	history = focus.Prune(history, snapshot)
	if err := focus.WriteFile(stateDir, history); err != nil {
		t.Fatal(err)
	}
	if err := focus.ApplyPluginEvent("pane.moved", herdr.PluginEventData{
		PreviousPaneID: "w1:p1",
		Pane:           &herdr.PluginEventPane{PaneID: "w2:p9"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := focus.LoadFile(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range got.Agents {
		if id.PaneID == "w2:p9" && id.Generation == old.Generation {
			found = true
		}
		if id.PaneID == "w2:p9" && id.Generation != old.Generation {
			t.Fatalf("move created a new occupant instead of rekey: %+v", got.Agents)
		}
	}
	if !found {
		t.Fatalf("verified move lost prior MRU: %+v", got.Agents)
	}
	want := picker.SelectionID(picker.KindAgent, focus.AgentLiveID{PaneID: "w2:p9", Generation: old.Generation}.String())
	items := picker.BuildItems("agents", snapshot, got, nil, [][]string{{"agent"}})
	if !picker.HasItemID(items, want) {
		t.Fatalf("live list missing rekeyed id %s items=%+v", want, items)
	}
	sel := picker.PreselectItemID("agents", items, got, picker.LaunchContext{CurrentAgent: "other"})
	if sel != want {
		t.Fatalf("preselect %q want %s", sel, want)
	}
}
