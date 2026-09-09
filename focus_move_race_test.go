package main

import "testing"

func TestVerifiedMoveAfterSnapshotKeepsPublicMRU(t *testing.T) {
	snapshot := HerdrSessionSnapshot{
		Version: "0.9.0",
		Agents: []HerdrAgentRow{
			{HerdrPaneRow: HerdrPaneRow{PaneID: "w2:p9", Agent: "pi", AgentSession: &HerdrAgentSession{Value: "a"}, AgentStatus: "idle"}},
		},
	}
	socket, stateDir := startFakeHerdr(t, snapshot)
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
	t.Setenv("HERDR_SESSION", "hseh-test")
	witness, err := ReadServerContinuityWitness()
	if err != nil {
		t.Fatal(err)
	}
	history := emptyFocusHistory(witness)
	history = ApplyVerifiedOccupantTransition(history, "w1:p1", "pi", "a", false)
	history = RecordAgentPaneFocus(history, "w1:p1")
	old, _ := currentAgentLiveID(history, "w1:p1")
	history = pruneFocusHistory(history, snapshot)
	if err := writeFocusHistoryFile(stateDir, history); err != nil {
		t.Fatal(err)
	}
	if err := applyPluginEvent("pane.moved", pluginEventData{
		PreviousPaneID: "w1:p1",
		Pane:           &pluginEventPane{PaneID: "w2:p9"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := loadFocusHistoryFile(stateDir)
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
	want := pickerSelectionID(pickerKindAgent, AgentLiveID{PaneID: "w2:p9", Generation: old.Generation}.String())
	items := BuildPickerItems("agents", snapshot, got, nil, [][]string{{"agent"}})
	if !pickerItemByID(items, want) {
		t.Fatalf("live list missing rekeyed id %s items=%+v", want, items)
	}
	sel := PreselectPickerItemID("agents", items, got, pickerLaunchContext{CurrentAgent: "other"})
	if sel != want {
		t.Fatalf("preselect %q want %s", sel, want)
	}
}
