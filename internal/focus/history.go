package focus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/regutierrez/hseh/internal/config"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/lockfile"
)

const workspaceSwitchCycleTimeoutMs = 500

// AgentLiveID is pane id plus occupant generation, not a conversation id.
type AgentLiveID struct {
	PaneID     string `json:"pane_id"`
	Generation int    `json:"generation"`
}

func (id AgentLiveID) String() string {
	return fmt.Sprintf("%s#%d", id.PaneID, id.Generation)
}

// PaneOccupant is the verified agent occupant of one pane.
type PaneOccupant struct {
	Generation   int    `json:"generation"`
	AgentKind    string `json:"agent_kind"`
	SessionValue string `json:"session_value,omitempty"`
}

// WorkspaceSwitchCycle is an in-progress previous-space cycle: repeated switches within
// workspaceSwitchCycleTimeoutMs walk the sidebar order instead of bouncing between two spaces.
type WorkspaceSwitchCycle struct {
	Order        []string `json:"order"`
	Target       string   `json:"target"`
	LastSwitchAt int64    `json:"last_switch_at"`
}

// History is session-scoped space and agent recency for one proven server.
type History struct {
	Witness                herdr.ContinuityWitness `json:"witness"`
	Spaces                 []string                `json:"spaces"`
	Agents                 []AgentLiveID           `json:"agents,omitempty"`
	Occupants              map[string]PaneOccupant `json:"occupants"`
	LastOccupantGeneration map[string]int          `json:"last_occupant_generation,omitempty"`
	Cycle                  *WorkspaceSwitchCycle   `json:"cycle,omitempty"`
	Pending                []string                `json:"pending,omitempty"`
	// AgentPresentationSeeded is true after this switcher took an idle baseline of live agent state_change_seq values.
	AgentPresentationSeeded bool `json:"agent_presentation_seeded,omitempty"`
	// AcknowledgedStateChangeSeq is the last presented state_change_seq per agent pane. Idle/done with a higher seq is shown as done until the pane is focused.
	AcknowledgedStateChangeSeq map[string]uint64 `json:"acknowledged_state_change_seq,omitempty"`
}

func EmptyHistory(witness herdr.ContinuityWitness) History {
	return History{
		Witness:                witness,
		Occupants:              map[string]PaneOccupant{},
		LastOccupantGeneration: map[string]int{},
	}
}

func historyStatePath(stateDir string) string {
	return filepath.Join(config.SessionHistoryDir(stateDir), "history.json")
}

func historyLockPath(stateDir string) string {
	return filepath.Join(config.SessionHistoryDir(stateDir), "history.lock")
}

func LoadFile(stateDir string) (History, error) {
	payload, err := os.ReadFile(historyStatePath(stateDir))
	if err != nil {
		if os.IsNotExist(err) {
			return EmptyHistory(herdr.ContinuityWitness{}), nil
		}
		return History{}, fmt.Errorf("hseh history: read: %w", err)
	}
	var history History
	if err := json.Unmarshal(payload, &history); err != nil {
		return History{}, fmt.Errorf("hseh history: parse: %w", err)
	}
	if history.Occupants == nil {
		history.Occupants = map[string]PaneOccupant{}
	}
	if history.LastOccupantGeneration == nil {
		history.LastOccupantGeneration = map[string]int{}
	}
	return history, nil
}

func WriteFile(stateDir string, history History) error {
	dir := config.SessionHistoryDir(stateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("hseh history: mkdir: %w", err)
	}
	payload, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("hseh history: encode: %w", err)
	}
	temporaryPath := filepath.Join(dir, fmt.Sprintf("history.%d.tmp", os.Getpid()))
	if err := os.WriteFile(temporaryPath, append(payload, '\n'), 0o600); err != nil {
		return fmt.Errorf("hseh history: write: %w", err)
	}
	if err := os.Rename(temporaryPath, historyStatePath(stateDir)); err != nil {
		return fmt.Errorf("hseh history: rename: %w", err)
	}
	return nil
}

func withLock(stateDir string, fn func() error) error {
	return lockfile.WithExclusive(historyLockPath(stateDir), fn)
}

// update applies fn to the witness-checked history under the session lock and persists the result.
func update(stateDir string, witness herdr.ContinuityWitness, fn func(History) History) (History, error) {
	var history History
	err := withLock(stateDir, func() error {
		loaded, err := loadValidated(stateDir, witness)
		if err != nil {
			return err
		}
		history = fn(loaded)
		return WriteFile(stateDir, history)
	})
	return history, err
}

// loadValidated loads history or drops it when the server process changed.
func loadValidated(stateDir string, live herdr.ContinuityWitness) (History, error) {
	stored, err := LoadFile(stateDir)
	if err != nil {
		return EmptyHistory(live), err
	}
	if !herdr.SameContinuityWitness(stored.Witness, live) {
		return EmptyHistory(live), nil
	}
	stored.Witness = live
	return stored, nil
}

func Prune(history History, snapshot herdr.SessionSnapshot) History {
	liveSpaces := map[string]bool{}
	var spaceOrder []string
	for _, workspace := range snapshot.Workspaces {
		if workspace.WorkspaceID == "" {
			continue
		}
		liveSpaces[workspace.WorkspaceID] = true
		spaceOrder = append(spaceOrder, workspace.WorkspaceID)
	}
	history.Spaces = filterStrings(history.Spaces, liveSpaces)
	for _, id := range spaceOrder {
		if !slices.Contains(history.Spaces, id) {
			history.Spaces = append(history.Spaces, id)
		}
	}
	history.Pending = filterStrings(history.Pending, liveSpaces)

	livePanes := map[string]herdr.AgentRow{}
	for _, agent := range snapshot.Agents {
		livePanes[agent.PaneID] = agent
	}
	occupants := map[string]PaneOccupant{}
	for paneID, occupant := range history.Occupants {
		rememberPaneGeneration(&history, paneID, occupant.Generation)
		agent, ok := livePanes[paneID]
		if !ok {
			occupants[paneID] = occupant
			continue
		}
		if occupantMatchesAgent(occupant, agent) {
			occupants[paneID] = occupant
			continue
		}
		gen := nextPaneGeneration(history, paneID)
		occupants[paneID] = PaneOccupant{Generation: gen, AgentKind: agent.Agent, SessionValue: sessionValue(agent.AgentSession)}
		rememberPaneGeneration(&history, paneID, gen)
	}
	for paneID, agent := range livePanes {
		if _, ok := occupants[paneID]; ok {
			continue
		}
		gen := nextPaneGeneration(history, paneID)
		occupants[paneID] = PaneOccupant{Generation: gen, AgentKind: agent.Agent, SessionValue: sessionValue(agent.AgentSession)}
		rememberPaneGeneration(&history, paneID, gen)
	}
	history.Occupants = occupants

	var agents []AgentLiveID
	seen := map[AgentLiveID]bool{}
	for _, id := range history.Agents {
		occupant, ok := history.Occupants[id.PaneID]
		if !ok || occupant.Generation != id.Generation {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		agents = append(agents, id)
	}
	history.Agents = agents
	return syncAgentPresentation(history, snapshot)
}

func occupantMatchesAgent(occupant PaneOccupant, agent herdr.AgentRow) bool {
	if occupant.AgentKind != agent.Agent {
		return false
	}
	liveSession := sessionValue(agent.AgentSession)
	if occupant.SessionValue == "" && liveSession == "" {
		return true
	}
	return occupant.SessionValue != "" && occupant.SessionValue == liveSession
}

func sessionValue(session *herdr.AgentSession) string {
	if session == nil {
		return ""
	}
	return session.Value
}

func CurrentAgentLiveID(history History, paneID string) (AgentLiveID, bool) {
	occupant, ok := history.Occupants[paneID]
	if !ok {
		return AgentLiveID{}, false
	}
	return AgentLiveID{PaneID: paneID, Generation: occupant.Generation}, true
}

func rememberPaneGeneration(history *History, paneID string, generation int) {
	if generation > history.LastOccupantGeneration[paneID] {
		history.LastOccupantGeneration[paneID] = generation
	}
}

func nextPaneGeneration(history History, paneID string) int {
	n := history.LastOccupantGeneration[paneID]
	if occupant, ok := history.Occupants[paneID]; ok && occupant.Generation > n {
		n = occupant.Generation
	}
	return n + 1
}

// recordWorkspaceFocus records a space as most recently used unless plugin-pending.
func recordWorkspaceFocus(history History, workspaceID string) History {
	for index, id := range history.Pending {
		if id == workspaceID {
			history.Pending = append(history.Pending[:index], history.Pending[index+1:]...)
			return history
		}
	}
	history.Spaces = prependUnique(history.Spaces, workspaceID)
	history.Cycle = nil
	return history
}

func removeWorkspace(history History, workspaceID string) History {
	history.Spaces = removeString(history.Spaces, workspaceID)
	history.Pending = removeString(history.Pending, workspaceID)
	if history.Cycle != nil {
		history.Cycle.Order = removeString(history.Cycle.Order, workspaceID)
		if history.Cycle.Target == workspaceID || len(history.Cycle.Order) == 0 {
			history.Cycle = nil
		}
	}
	return history
}

func RecordAgentPaneFocus(history History, paneID string) History {
	id, ok := CurrentAgentLiveID(history, paneID)
	if !ok {
		return history
	}
	history.Agents = prependAgentID(history.Agents, id)
	return history
}

// rekeyMovedPaneOccupant keeps the same occupant under the new pane id.
func rekeyMovedPaneOccupant(history History, previousPaneID, paneID string) History {
	occupant, ok := history.Occupants[previousPaneID]
	if !ok {
		return history
	}
	delete(history.Occupants, previousPaneID)
	history.Occupants[paneID] = occupant
	if last, ok := history.LastOccupantGeneration[previousPaneID]; ok {
		delete(history.LastOccupantGeneration, previousPaneID)
		if last > history.LastOccupantGeneration[paneID] {
			history.LastOccupantGeneration[paneID] = last
		}
	}
	rememberPaneGeneration(&history, paneID, occupant.Generation)
	for index := range history.Agents {
		if history.Agents[index].PaneID == previousPaneID {
			history.Agents[index].PaneID = paneID
		}
	}
	return rekeyAgentPresentationAck(history, previousPaneID, paneID)
}

func clearPaneOccupant(history History, paneID string) History {
	if occupant, ok := history.Occupants[paneID]; ok {
		rememberPaneGeneration(&history, paneID, occupant.Generation)
	}
	delete(history.Occupants, paneID)
	var agents []AgentLiveID
	for _, id := range history.Agents {
		if id.PaneID != paneID {
			agents = append(agents, id)
		}
	}
	history.Agents = agents
	return history
}

// ApplyVerifiedOccupantTransition advances generation only when the occupant changed.
func ApplyVerifiedOccupantTransition(history History, paneID, agentKind, sessionVal string, released bool) History {
	if released || agentKind == "" {
		return clearPaneOccupant(history, paneID)
	}
	existing, ok := history.Occupants[paneID]
	if !ok {
		gen := nextPaneGeneration(history, paneID)
		history.Occupants[paneID] = PaneOccupant{Generation: gen, AgentKind: agentKind, SessionValue: sessionVal}
		rememberPaneGeneration(&history, paneID, gen)
		return history
	}
	if existing.AgentKind == agentKind && existing.SessionValue == sessionVal {
		return history
	}
	next := nextPaneGeneration(history, paneID)
	history.Occupants[paneID] = PaneOccupant{Generation: next, AgentKind: agentKind, SessionValue: sessionVal}
	rememberPaneGeneration(&history, paneID, next)
	var agents []AgentLiveID
	for _, id := range history.Agents {
		if !(id.PaneID == paneID && id.Generation == existing.Generation) {
			agents = append(agents, id)
		}
	}
	history.Agents = agents
	return history
}

// selectNextWorkspace chooses the previous space, then cycles in sidebar order.
func selectNextWorkspace(history History, snapshot herdr.SessionSnapshot, nowMs int64) (History, string) {
	var workspaceOrder []string
	current := snapshot.FocusedWorkspaceID
	for _, workspace := range snapshot.Workspaces {
		if workspace.WorkspaceID == "" {
			continue
		}
		if !slices.Contains(workspaceOrder, workspace.WorkspaceID) {
			workspaceOrder = append(workspaceOrder, workspace.WorkspaceID)
		}
		if workspace.Focused && current == "" {
			current = workspace.WorkspaceID
		}
	}
	history = Prune(history, snapshot)
	if current == "" || len(workspaceOrder) < 2 {
		return history, ""
	}
	continuing := history.Cycle != nil && history.Cycle.Target == current && nowMs-history.Cycle.LastSwitchAt < workspaceSwitchCycleTimeoutMs
	if !continuing {
		history.Spaces = prependUnique(history.Spaces, current)
		history.Cycle = nil
	}
	var order []string
	if continuing {
		for _, id := range history.Cycle.Order {
			if slices.Contains(workspaceOrder, id) {
				order = append(order, id)
			}
		}
		for _, id := range workspaceOrder {
			if !slices.Contains(order, id) {
				order = append(order, id)
			}
		}
	} else {
		order = append(order, workspaceOrder...)
	}
	var target string
	if continuing {
		index := -1
		for i, id := range order {
			if id == current {
				index = i
				break
			}
		}
		target = order[(index+1)%len(order)]
	} else {
		for _, id := range history.Spaces {
			if id != current {
				target = id
				break
			}
		}
	}
	history.Cycle = &WorkspaceSwitchCycle{Order: order, Target: target, LastSwitchAt: nowMs}
	history.Pending = append(history.Pending, target)
	return history, target
}

func prependUnique(ids []string, id string) []string {
	out := []string{id}
	for _, existing := range ids {
		if existing != id {
			out = append(out, existing)
		}
	}
	return out
}

func prependAgentID(ids []AgentLiveID, id AgentLiveID) []AgentLiveID {
	out := []AgentLiveID{id}
	for _, existing := range ids {
		if existing != id {
			out = append(out, existing)
		}
	}
	return out
}

func removeString(ids []string, target string) []string {
	var out []string
	for _, id := range ids {
		if id != target {
			out = append(out, id)
		}
	}
	return out
}

func filterStrings(ids []string, allowed map[string]bool) []string {
	var out []string
	for _, id := range ids {
		if allowed[id] {
			out = append(out, id)
		}
	}
	return out
}

func AgentPriorityRank(status string) int {
	switch status {
	case "blocked":
		return 0
	case "done":
		return 1
	case "working":
		return 2
	case "idle":
		return 3
	default:
		return 4
	}
}

func LoadPruned(snapshot herdr.SessionSnapshot, witness herdr.ContinuityWitness) (History, error) {
	return update(config.StateDir(), witness, func(history History) History {
		return Prune(history, snapshot)
	})
}

// SwitchWorkspace records the focused space and returns the one to switch to: the
// previous space, or the next in sidebar order while a cycle is in progress. An empty
// target means there is nothing to switch to.
func SwitchWorkspace(snapshot herdr.SessionSnapshot, witness herdr.ContinuityWitness, nowMs int64) (string, error) {
	var target string
	_, err := update(config.StateDir(), witness, func(history History) History {
		history, target = selectNextWorkspace(history, snapshot, nowMs)
		return history
	})
	return target, err
}
