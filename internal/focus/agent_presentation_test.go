package focus

import (
	"testing"

	"github.com/regutierrez/hseh/internal/herdr"
)

func agentRow(paneID, status string, seq uint64) herdr.AgentRow {
	return herdr.AgentRow{
		PaneRow:        herdr.PaneRow{PaneID: paneID, WorkspaceID: "w1", AgentStatus: status},
		StateChangeSeq: seq,
	}
}

func TestFirstSnapshotEstablishesIdleBaselineWithoutServerSeen(t *testing.T) {
	snapshot := herdr.SessionSnapshot{Agents: []herdr.AgentRow{agentRow("w1:p1", "done", 4)}}
	history := Prune(EmptyHistory(herdr.ContinuityWitness{}), snapshot)
	got := ProjectedAgentStatus("done", "w1:p1", 4, history)
	if got != "idle" {
		t.Fatalf("first snapshot must ignore server done, got %q", got)
	}
}

func TestUnpresentedWorkingCompletionProjectsDone(t *testing.T) {
	history := Prune(EmptyHistory(herdr.ContinuityWitness{}), herdr.SessionSnapshot{
		Agents: []herdr.AgentRow{agentRow("w1:p1", "working", 4)},
	})
	history = Prune(history, herdr.SessionSnapshot{
		Agents: []herdr.AgentRow{agentRow("w1:p1", "idle", 5)},
	})
	got := ProjectedAgentStatus("idle", "w1:p1", 5, history)
	if got != "done" {
		t.Fatalf("unpresented completion must be done, got %q", got)
	}
}

func TestPresentedPaneAcknowledgesCompletion(t *testing.T) {
	snapshot := herdr.SessionSnapshot{Agents: []herdr.AgentRow{agentRow("w1:p1", "idle", 5)}}
	history := Prune(EmptyHistory(herdr.ContinuityWitness{}), herdr.SessionSnapshot{
		Agents: []herdr.AgentRow{agentRow("w1:p1", "working", 4)},
	})
	history = Prune(history, snapshot)
	history = acknowledgeAgentPresentation(history, snapshot, "w1:p1")
	got := ProjectedAgentStatus("idle", "w1:p1", 5, history)
	if got != "idle" {
		t.Fatalf("presented completion must be idle, got %q", got)
	}
}

func TestNewAgentAfterBaselineProjectsDone(t *testing.T) {
	history := Prune(EmptyHistory(herdr.ContinuityWitness{}), herdr.SessionSnapshot{
		Agents: []herdr.AgentRow{agentRow("w1:p1", "idle", 1)},
	})
	history = Prune(history, herdr.SessionSnapshot{
		Agents: []herdr.AgentRow{
			agentRow("w1:p1", "idle", 1),
			agentRow("w1:p2", "idle", 2),
		},
	})
	got := ProjectedAgentStatus("idle", "w1:p2", 2, history)
	if got != "done" {
		t.Fatalf("new pane after baseline must be done, got %q", got)
	}
}

func TestUnseededHistoryKeepsSnapshotStatus(t *testing.T) {
	history := EmptyHistory(herdr.ContinuityWitness{})
	if got := ProjectedAgentStatus("done", "w1:p1", 4, history); got != "done" {
		t.Fatalf("unseeded history must keep snapshot status, got %q", got)
	}
}

func TestWorkingAndBlockedPassThrough(t *testing.T) {
	history := Prune(EmptyHistory(herdr.ContinuityWitness{}), herdr.SessionSnapshot{
		Agents: []herdr.AgentRow{agentRow("w1:p1", "working", 4)},
	})
	if got := ProjectedAgentStatus("working", "w1:p1", 4, history); got != "working" {
		t.Fatalf("working must pass through, got %q", got)
	}
	if got := ProjectedAgentStatus("blocked", "w1:p1", 4, history); got != "blocked" {
		t.Fatalf("blocked must pass through, got %q", got)
	}
}

func TestRekeyMovedPaneKeepsPresentationAck(t *testing.T) {
	snapshot := herdr.SessionSnapshot{Agents: []herdr.AgentRow{agentRow("w1:p1", "idle", 4)}}
	history := Prune(EmptyHistory(herdr.ContinuityWitness{}), snapshot)
	history = rekeyMovedPaneOccupant(history, "w1:p1", "w2:p9")
	got := ProjectedAgentStatus("idle", "w2:p9", 4, history)
	if got != "idle" {
		t.Fatalf("moved pane must keep ack, got %q", got)
	}
}

func TestProjectedWorkspaceAgentStatusUsesDoneOverIdle(t *testing.T) {
	agents := []herdr.AgentRow{
		agentRow("w1:p1", "idle", 1),
		{PaneRow: herdr.PaneRow{PaneID: "w1:p2", WorkspaceID: "w1", AgentStatus: "idle"}, StateChangeSeq: 2},
	}
	history := Prune(EmptyHistory(herdr.ContinuityWitness{}), herdr.SessionSnapshot{
		Agents: []herdr.AgentRow{agents[0]},
	})
	history = Prune(history, herdr.SessionSnapshot{Agents: agents})
	got := ProjectedWorkspaceAgentStatus("w1", "idle", agents, history)
	if got != "done" {
		t.Fatalf("workspace aggregate must follow unpresented agent, got %q", got)
	}
}
