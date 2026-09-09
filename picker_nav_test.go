package main

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestQueryRetainedAcrossViewsAndClearedOnRebuild(t *testing.T) {
	snapshot := HerdrSessionSnapshot{
		Workspaces: []HerdrWorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha"},
			{WorkspaceID: "w2", Label: "beta"},
		},
		Agents: []HerdrAgentRow{{HerdrPaneRow: HerdrPaneRow{PaneID: "w1:p1", Agent: "pi", DisplayAgent: "beta-bot", AgentStatus: "idle"}}},
	}
	m := newPickerModel("spaces", snapshot, emptyFocusHistory(ServerContinuityWitness{}), defaultSidebarLayout(), 0, 80)
	m.query = "beta"
	m.applyQuery()
	if len(m.visible) != 1 || m.visible[0].WorkspaceID != "w2" {
		t.Fatalf("spaces query: %+v", m.visible)
	}
	m.cycleView(1)
	if m.view != pickerViewAgents {
		t.Fatalf("view %s", m.view)
	}
	if m.query != "beta" {
		t.Fatalf("query not kept: %q", m.query)
	}
	if len(m.visible) != 1 || m.visible[0].Kind != pickerKindAgent {
		t.Fatalf("agents query: %+v", m.visible)
	}
	m.query = ""
	m.applyQuery()
	if len(m.visible) != 1 {
		t.Fatalf("cleared query should show all agents, got %d", len(m.visible))
	}
}

func TestPreviewDoesNotChangePreselectHistory(t *testing.T) {
	history := emptyFocusHistory(ServerContinuityWitness{})
	history.Spaces = []string{"w2", "w1"}
	snapshot := HerdrSessionSnapshot{
		FocusedWorkspaceID: "w1",
		Workspaces: []HerdrWorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha"},
			{WorkspaceID: "w2", Label: "beta"},
		},
	}
	m := newPickerModel("spaces", snapshot, history, defaultSidebarLayout(), 0, 40)
	if m.selectedID != pickerSelectionID(pickerKindSpace, "w2") {
		t.Fatalf("preselect %s", m.selectedID)
	}
	m.previewText = "ticks"
	if m.history.Spaces[0] != "w2" {
		t.Fatalf("preview mutated MRU: %v", m.history.Spaces)
	}
}

func startCountingHerdr(t *testing.T, snapshot HerdrSessionSnapshot, reads *atomic.Int32, focused *atomic.Value) (socketPath, stateDir string) {
	t.Helper()
	dir := t.TempDir()
	socketPath = filepath.Join(dir, "herdr.sock")
	stateDir = filepath.Join(dir, "state")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, err := bufio.NewReader(c).ReadBytes('\n')
				if err != nil {
					return
				}
				var req struct {
					ID     string          `json:"id"`
					Method string          `json:"method"`
					Params json.RawMessage `json:"params"`
				}
				if json.Unmarshal(line, &req) != nil {
					return
				}
				var result any
				switch req.Method {
				case "session.snapshot":
					result = herdrSnapshotEnvelope{Type: "session_snapshot", Snapshot: &snapshot}
				case "pane.read":
					if reads != nil {
						reads.Add(1)
					}
					result = herdrPaneReadEnvelope{Type: "pane_read", Read: &HerdrPaneReadResult{PaneID: "w2:p1", Text: "tick", Format: "ansi", Source: "visible"}}
				case "workspace.focus":
					var params struct {
						WorkspaceID string `json:"workspace_id"`
					}
					_ = json.Unmarshal(req.Params, &params)
					if focused != nil {
						focused.Store(params.WorkspaceID)
					}
					result = map[string]any{"type": "ok"}
				case "agent.focus":
					var params struct {
						Target string `json:"target"`
					}
					_ = json.Unmarshal(req.Params, &params)
					agent := HerdrAgentRow{HerdrPaneRow: HerdrPaneRow{PaneID: params.Target}}
					for _, row := range snapshot.Agents {
						if row.PaneID == params.Target {
							agent = row
							break
						}
					}
					result = herdrAgentFocusEnvelope{Type: "agent_info", Agent: &agent}
				case "tab.focus":
					result = map[string]any{"type": "ok"}
				default:
					result = map[string]any{"type": "ok"}
				}
				payload, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
				_, _ = c.Write(append(payload, '\n'))
			}(conn)
		}
	}()
	return socketPath, stateDir
}

func TestNarrowStopsPreviewReadsAndWideResumes(t *testing.T) {
	var reads atomic.Int32
	snapshot := HerdrSessionSnapshot{Version: "0.9.0", Workspaces: []HerdrWorkspaceRow{{WorkspaceID: "w2", Label: "beta", ActiveTabID: "w2:t1"}}, Layouts: []HerdrPaneLayout{{TabID: "w2:t1", FocusedPaneID: "w2:p1"}}}
	socket, state := startCountingHerdr(t, snapshot, &reads, nil)
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	t.Setenv("HERDR_SESSION", "hseh-test")
	m := pickerModel{widePreviewMinCols: 40, previewLoadingDelay: time.Millisecond, snapshotReady: true, selectedID: "w2", visible: []PickerItem{{ID: "w2", PreviewPane: "w2:p1"}}}
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	got := next.(pickerModel)
	if cmd == nil {
		t.Fatal("expected wide preview read")
	}
	got = feedCmd(got, cmd)
	if reads.Load() != 1 {
		t.Fatalf("wide reads %d", reads.Load())
	}
	// Too short for a stacked preview: the preview is hidden and reads stop.
	next, cmd = got.Update(tea.WindowSizeMsg{Width: 30, Height: 6})
	got = next.(pickerModel)
	if got.showsPreview() {
		t.Fatal("6-row popup still shows a preview")
	}
	if cmd != nil || got.startPreview(false) != nil || got.startPreview(true) != nil {
		t.Fatal("hidden preview started a read")
	}
	if got.previewText != "tick" {
		t.Fatalf("hidden preview dropped the last frame: %q", got.previewText)
	}
	if reads.Load() != 1 {
		t.Fatalf("narrow extra reads %d", reads.Load())
	}
	next, cmd = got.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	got = next.(pickerModel)
	if cmd == nil {
		t.Fatal("expected resume preview read")
	}
	got = feedCmd(got, cmd)
	if reads.Load() != 2 {
		t.Fatalf("resume reads %d", reads.Load())
	}
}

func TestEnterFocusesSelectedWorkspace(t *testing.T) {
	snapshot := HerdrSessionSnapshot{
		Version:            "0.9.0",
		FocusedWorkspaceID: "w1",
		Workspaces: []HerdrWorkspaceRow{
			{WorkspaceID: "w1", Label: "alpha", ActiveTabID: "w1:t1"},
			{WorkspaceID: "w2", Label: "beta", ActiveTabID: "w2:t1"},
		},
	}
	var focused atomic.Value
	socket, state := startCountingHerdr(t, snapshot, nil, &focused)
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	m := newPickerModel("spaces", snapshot, emptyFocusHistory(ServerContinuityWitness{SocketPath: socket, PeerPID: 1, PeerStartTime: "1"}), defaultSidebarLayout(), 0, 80)
	m.selectedID = pickerSelectionID(pickerKindSpace, "w2")
	cmd := m.startAccept()
	if cmd == nil {
		t.Fatal("expected accept")
	}
	msg := cmd()
	if _, ok := msg.(pickerAcceptedMsg); !ok {
		t.Fatalf("got %#v", msg)
	}
	if focused.Load() != "w2" {
		t.Fatalf("focused %v", focused.Load())
	}
}
