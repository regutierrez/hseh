package focus

import "github.com/regutierrez/hseh/internal/herdr"

// ProjectedAgentStatus maps idle/done onto Herdr's client agent-panel rule:
// a state_change_seq the switcher has not presented is done, and focusing the
// pane acknowledges it back to idle. Working, blocked, and unknown pass through.
func ProjectedAgentStatus(status, paneID string, stateChangeSeq uint64, history History) string {
	if !history.AgentPresentationSeeded {
		return status
	}
	switch status {
	case "idle", "done":
		if seq, ok := history.AcknowledgedStateChangeSeq[paneID]; ok && seq >= stateChangeSeq {
			return "idle"
		}
		return "done"
	default:
		return status
	}
}

// ProjectedWorkspaceAgentStatus is the highest projected agent status in a workspace.
func ProjectedWorkspaceAgentStatus(workspaceID, snapshotStatus string, agents []herdr.AgentRow, history History) string {
	if !history.AgentPresentationSeeded {
		return snapshotStatus
	}
	best := ""
	bestRank := AgentPriorityRank("") + 1
	for _, agent := range agents {
		if agent.WorkspaceID != workspaceID {
			continue
		}
		status := ProjectedAgentStatus(agent.AgentStatus, agent.PaneID, agent.StateChangeSeq, history)
		if rank := AgentPriorityRank(status); rank < bestRank {
			best = status
			bestRank = rank
		}
	}
	if best == "" {
		return snapshotStatus
	}
	return best
}

// syncAgentPresentation takes an idle baseline of live state_change_seq values
// the first time this switcher sees a session, then keeps only panes that still
// exist. New panes after the baseline stay unacknowledged so idle becomes done.
func syncAgentPresentation(history History, snapshot herdr.SessionSnapshot) History {
	if !history.AgentPresentationSeeded {
		acks := make(map[string]uint64, len(snapshot.Agents))
		for _, agent := range snapshot.Agents {
			if agent.PaneID == "" {
				continue
			}
			acks[agent.PaneID] = agent.StateChangeSeq
		}
		history.AcknowledgedStateChangeSeq = acks
		history.AgentPresentationSeeded = true
		return history
	}
	next := make(map[string]uint64, len(snapshot.Agents))
	for _, agent := range snapshot.Agents {
		if agent.PaneID == "" {
			continue
		}
		if seq, ok := history.AcknowledgedStateChangeSeq[agent.PaneID]; ok {
			next[agent.PaneID] = seq
		}
	}
	history.AcknowledgedStateChangeSeq = next
	return history
}

// AcknowledgeAgentPresentation records that this pane's current state_change_seq
// was presented, so idle/done projects back to idle.
func AcknowledgeAgentPresentation(history History, snapshot herdr.SessionSnapshot, paneID string) History {
	if !history.AgentPresentationSeeded || paneID == "" {
		return history
	}
	for _, agent := range snapshot.Agents {
		if agent.PaneID != paneID {
			continue
		}
		if history.AcknowledgedStateChangeSeq == nil {
			history.AcknowledgedStateChangeSeq = map[string]uint64{}
		}
		history.AcknowledgedStateChangeSeq[paneID] = agent.StateChangeSeq
		return history
	}
	return history
}

func rekeyAgentPresentationAck(history History, previousPaneID, paneID string) History {
	if history.AcknowledgedStateChangeSeq == nil || previousPaneID == "" || paneID == "" || previousPaneID == paneID {
		return history
	}
	seq, ok := history.AcknowledgedStateChangeSeq[previousPaneID]
	if !ok {
		return history
	}
	delete(history.AcknowledgedStateChangeSeq, previousPaneID)
	history.AcknowledgedStateChangeSeq[paneID] = seq
	return history
}
