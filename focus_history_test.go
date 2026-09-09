package main

import "testing"

func TestSameServerContinuityWitnessRequiresPidAndStartTime(t *testing.T) {
	live := ServerContinuityWitness{SocketPath: "/tmp/s", PeerPID: 9, PeerStartTime: "100"}
	if sameServerContinuityWitness(ServerContinuityWitness{SocketPath: "/tmp/s", PeerPID: 9, PeerStartTime: "99"}, live) {
		t.Fatal("start time mismatch must invalidate history")
	}
	if sameServerContinuityWitness(ServerContinuityWitness{SocketPath: "/tmp/s", PeerPID: 8, PeerStartTime: "100"}, live) {
		t.Fatal("pid mismatch must invalidate history")
	}
	if !sameServerContinuityWitness(live, live) {
		t.Fatal("identical witness must match")
	}
}

func TestApplyVerifiedOccupantTransitionIgnoresRepeatDetection(t *testing.T) {
	history := emptyFocusHistory(ServerContinuityWitness{})
	history = ApplyVerifiedOccupantTransition(history, "w1:p1", "pi", "sess-a", false)
	history = RecordAgentPaneFocus(history, "w1:p1")
	history = ApplyVerifiedOccupantTransition(history, "w1:p1", "pi", "sess-a", false)
	id, ok := currentAgentLiveID(history, "w1:p1")
	if !ok || id.Generation != 1 {
		t.Fatalf("repeat detection changed occupant: %+v", id)
	}
	if len(history.Agents) != 1 || history.Agents[0].Generation != 1 {
		t.Fatalf("repeat detection dropped history: %+v", history.Agents)
	}
}

func TestApplyVerifiedOccupantTransitionReleaseAndReplacement(t *testing.T) {
	history := emptyFocusHistory(ServerContinuityWitness{})
	history = ApplyVerifiedOccupantTransition(history, "w1:p1", "pi", "sess-a", false)
	history = RecordAgentPaneFocus(history, "w1:p1")
	history = ApplyVerifiedOccupantTransition(history, "w1:p1", "pi", "sess-a", true)
	if _, ok := history.Occupants["w1:p1"]; ok {
		t.Fatal("release must clear occupant")
	}
	history = ApplyVerifiedOccupantTransition(history, "w1:p1", "pi", "sess-b", false)
	id, ok := currentAgentLiveID(history, "w1:p1")
	if !ok || id.Generation != 2 {
		t.Fatalf("new occupant after release: %+v", id)
	}
	history = ApplyVerifiedOccupantTransition(history, "w1:p1", "claude", "sess-c", false)
	id, ok = currentAgentLiveID(history, "w1:p1")
	if !ok || id.Generation != 3 {
		t.Fatalf("replacement must bump generation: %+v", id)
	}
}

func TestRekeyMovedPaneOccupantKeepsGeneration(t *testing.T) {
	history := emptyFocusHistory(ServerContinuityWitness{})
	history = ApplyVerifiedOccupantTransition(history, "w1:p1", "pi", "sess-a", false)
	history.Occupants["w1:p1"] = PaneOccupant{Generation: 3, AgentKind: "pi", SessionValue: "sess-a"}
	history = RecordAgentPaneFocus(history, "w1:p1")
	history = RekeyMovedPaneOccupant(history, "w1:p1", "w2:p9")
	id, ok := currentAgentLiveID(history, "w2:p9")
	if !ok || id.Generation != 3 || id.PaneID != "w2:p9" {
		t.Fatalf("move must rekey same occupant: %+v", id)
	}
	if _, ok := history.Occupants["w1:p1"]; ok {
		t.Fatal("old pane id must not keep occupant")
	}
	if history.Agents[0].PaneID != "w2:p9" || history.Agents[0].Generation != 3 {
		t.Fatalf("agent history must rekey: %+v", history.Agents[0])
	}
}

func TestLoadValidatedFocusHistoryDropsOnWitnessMismatch(t *testing.T) {
	dir := t.TempDir()
	stored := emptyFocusHistory(ServerContinuityWitness{SocketPath: "/tmp/a", PeerPID: 1, PeerStartTime: "1"})
	stored.Spaces = []string{"w9"}
	if err := writeFocusHistoryFile(dir, stored); err != nil {
		t.Fatal(err)
	}
	live := ServerContinuityWitness{SocketPath: "/tmp/a", PeerPID: 1, PeerStartTime: "2"}
	got, err := LoadValidatedFocusHistory(dir, live)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Spaces) != 0 {
		t.Fatalf("unverifiable restart history must drop, got %v", got.Spaces)
	}
}

func TestPruneDoesNotTransferSameConversationWithoutMove(t *testing.T) {
	h := emptyFocusHistory(ServerContinuityWitness{})
	h = ApplyVerifiedOccupantTransition(h, "w1:p1", "pi", "shared", false)
	h = RecordAgentPaneFocus(h, "w1:p1")
	snapshot := HerdrSessionSnapshot{Agents: []HerdrAgentRow{
		{HerdrPaneRow: HerdrPaneRow{PaneID: "w2:p9", Agent: "pi", AgentSession: &HerdrAgentSession{Value: "shared"}}},
		{HerdrPaneRow: HerdrPaneRow{PaneID: "w3:p1", Agent: "pi", AgentSession: &HerdrAgentSession{Value: "shared"}}},
	}}
	h = pruneFocusHistory(h, snapshot)
	for _, id := range h.Agents {
		if id.PaneID == "w2:p9" {
			t.Fatalf("conversation transferred to new pane without move: %+v", h.Agents)
		}
	}
	if id, ok := currentAgentLiveID(h, "w2:p9"); ok && id.Generation != 1 {
		t.Fatalf("new pane inherited old generation: %+v", id)
	}
}

func TestVerifiedMoveThenPruneKeepsOccupant(t *testing.T) {
	h := emptyFocusHistory(ServerContinuityWitness{})
	h = ApplyVerifiedOccupantTransition(h, "w1:p1", "pi", "a", false)
	h = RecordAgentPaneFocus(h, "w1:p1")
	h = RekeyMovedPaneOccupant(h, "w1:p1", "w2:p9")
	snapshot := HerdrSessionSnapshot{Agents: []HerdrAgentRow{{HerdrPaneRow: HerdrPaneRow{PaneID: "w2:p9", Agent: "pi", AgentSession: &HerdrAgentSession{Value: "a"}}}}}
	h = pruneFocusHistory(h, snapshot)
	if len(h.Agents) != 1 || h.Agents[0].PaneID != "w2:p9" {
		t.Fatalf("verified move lost: %+v", h.Agents)
	}
}

func TestHistoryFilesAreNamespacedPerSession(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HERDR_SESSION", "alpha")
	a := emptyFocusHistory(ServerContinuityWitness{SocketPath: "/tmp/a", PeerPID: 1, PeerStartTime: "1", BootTime: "1"})
	a.Spaces = []string{"wA"}
	if err := writeFocusHistoryFile(dir, a); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_SESSION", "beta")
	b := emptyFocusHistory(ServerContinuityWitness{SocketPath: "/tmp/b", PeerPID: 2, PeerStartTime: "2", BootTime: "1"})
	b.Spaces = []string{"wB"}
	if err := writeFocusHistoryFile(dir, b); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_SESSION", "alpha")
	got, err := loadFocusHistoryFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Spaces) != 1 || got.Spaces[0] != "wA" {
		t.Fatalf("alpha history overwritten: %+v", got.Spaces)
	}
}

func TestSelectNextWorkspaceUsesPreviousThenCycle(t *testing.T) {
	history := emptyFocusHistory(ServerContinuityWitness{})
	history.Spaces = []string{"wA", "wC", "wB"}
	snapshot := HerdrSessionSnapshot{
		FocusedWorkspaceID: "wA",
		Workspaces: []HerdrWorkspaceRow{
			{WorkspaceID: "wA", Focused: true},
			{WorkspaceID: "wB"},
			{WorkspaceID: "wC"},
		},
	}
	history, target := SelectNextWorkspace(history, snapshot, 1000)
	if target != "wC" {
		t.Fatalf("previous space, got %q", target)
	}
	snapshot.FocusedWorkspaceID = "wC"
	snapshot.Workspaces[0].Focused = false
	snapshot.Workspaces[2].Focused = true
	history = RecordWorkspaceFocus(history, "wC")
	_, target = SelectNextWorkspace(history, snapshot, 1250)
	if target != "wA" {
		t.Fatalf("cycle, got %q", target)
	}
}
