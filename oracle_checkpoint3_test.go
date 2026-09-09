package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestOracleSnapshotCommandTracksInFlight(t *testing.T) {
	m := pickerModel{widePreviewMinCols: 80}
	next, _ := m.Update(snapshotTickMsg{})
	got := next.(pickerModel)
	if !got.snapshotInFlight || got.snapshotSeq == 0 {
		t.Fatalf("snapshot command changes lost: inFlight=%v seq=%d", got.snapshotInFlight, got.snapshotSeq)
	}
}

func TestOracleFirstPreviewReplyCanRender(t *testing.T) {
	m := pickerModel{widePreviewMinCols: 80, selectedID: "w1", visible: []PickerItem{{ID: "w1", PreviewPane: "w1:p1"}}}
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	got := next.(pickerModel)
	if cmd == nil {
		t.Fatal("expected preview command")
	}
	next, _ = got.Update(previewLoadedMsg{seq: 1, targetID: "w1", paneID: "w1:p1", text: "ACTUAL PREVIEW"})
	got = next.(pickerModel)
	if got.previewText != "ACTUAL PREVIEW" {
		t.Fatalf("first reply never renders: seq=%d text=%q", got.previewSeq, got.previewText)
	}
}

func TestOracleMoveReconcilesBeforePruning(t *testing.T) {
	h := emptyFocusHistory(ServerContinuityWitness{})
	h = ApplyVerifiedOccupantTransition(h, "w1:p1", "pi", "a", false)
	h = RecordAgentPaneFocus(h, "w1:p1")
	snapshot := HerdrSessionSnapshot{Agents: []HerdrAgentRow{{HerdrPaneRow: HerdrPaneRow{PaneID: "w2:p9", Agent: "pi", AgentSession: &HerdrAgentSession{Value: "a"}}}}}
	// A verified move must be applied before pruning its previous pane identity.
	h = RekeyMovedPaneOccupant(h, "w1:p1", "w2:p9")
	h = pruneFocusHistory(h, snapshot)
	if len(h.Agents) != 1 || h.Agents[0].PaneID != "w2:p9" {
		t.Fatalf("move loses history: %+v", h.Agents)
	}
}
