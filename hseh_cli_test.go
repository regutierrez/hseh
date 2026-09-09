package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var compiledHseh string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hseh-bin-")
	if err != nil {
		os.Exit(1)
	}
	bin := filepath.Join(dir, "hseh")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.RemoveAll(dir)
		os.Exit(1)
	}
	compiledHseh = bin
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func startFakeHerdr(t *testing.T, snapshot HerdrSessionSnapshot) (socketPath, stateDir string) {
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
			go serveFakeHerdrConn(conn, snapshot)
		}
	}()
	return socketPath, stateDir
}

func serveFakeHerdrConn(conn net.Conn, snapshot HerdrSessionSnapshot) {
	defer conn.Close()
	line, err := bufio.NewReader(conn).ReadBytes('\n')
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
		result = herdrPaneReadEnvelope{Type: "pane_read", Read: &HerdrPaneReadResult{PaneID: "w1:p1", Text: "hello", Format: "ansi", Source: "visible"}}
	case "plugin.pane.open":
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
	_, _ = conn.Write(append(payload, '\n'))
}

func TestCompiledCLIListJSON(t *testing.T) {
	snapshot := HerdrSessionSnapshot{
		Version:            "0.9.0",
		FocusedWorkspaceID: "w1",
		Workspaces: []HerdrWorkspaceRow{
			{WorkspaceID: "w1", Label: "one", AgentStatus: "idle", ActiveTabID: "w1:t1"},
			{WorkspaceID: "w2", Label: "two", AgentStatus: "unknown", ActiveTabID: "w2:t1"},
		},
		Layouts: []HerdrPaneLayout{{TabID: "w1:t1", FocusedPaneID: "w1:p1"}},
		Agents: []HerdrAgentRow{
			{HerdrPaneRow: HerdrPaneRow{PaneID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Agent: "pi", AgentStatus: "idle", DisplayAgent: "pi"}, StateChangeSeq: 3},
		},
	}
	socketPath, stateDir := startFakeHerdr(t, snapshot)
	cmd := exec.Command(compiledHseh, "list", "--json", "--view", "spaces")
	cmd.Env = append(os.Environ(),
		"HERDR_SOCKET_PATH="+socketPath,
		"HERDR_PLUGIN_STATE_DIR="+stateDir,
		"HERDR_SESSION=hseh-test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var doc PickerListDocument
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if doc.View != pickerViewSpaces || doc.Session.Name != "hseh-test" {
		t.Fatalf("session/view: %+v", doc)
	}
	if len(doc.Items) != 2 {
		t.Fatalf("want 2 spaces, got %#v", doc.Items)
	}
	if doc.Items[0].Kind != pickerKindSpace || doc.Items[1].Kind != pickerKindSpace {
		t.Fatalf("order: %#v", doc.Items)
	}
	for _, item := range doc.Items {
		if item.Rows == nil {
			t.Fatalf("rows omitted incorrectly: %+v", item)
		}
	}
}

func TestCompiledCLIListRequiresJSONFlag(t *testing.T) {
	cmd := exec.Command(compiledHseh, "list")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected error, got %s", out)
	}
}

func TestCompiledCLIListRejectsInvalidView(t *testing.T) {
	for _, view := range []string{"nope", "all"} {
		cmd := exec.Command(compiledHseh, "list", "--json", "--view", view)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected error for %s, got %s", view, out)
		}
	}
}

func TestCompiledLaunchOpensPluginPopup(t *testing.T) {
	snapshot := HerdrSessionSnapshot{Version: "0.9.0"}
	socketPath, stateDir := startFakeHerdr(t, snapshot)
	cmd := exec.Command(compiledHseh, "launch", "spaces")
	cmd.Env = append(os.Environ(),
		"HERDR_SOCKET_PATH="+socketPath,
		"HERDR_PLUGIN_STATE_DIR="+stateDir,
		"HERDR_SESSION=hseh-test",
		"HERDR_PLUGIN_ID=hseh",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestCompiledEventHookReleaseAndMove(t *testing.T) {
	snapshot := HerdrSessionSnapshot{
		Version: "0.9.0",
		Agents: []HerdrAgentRow{
			{HerdrPaneRow: HerdrPaneRow{PaneID: "w1:p1", Agent: "pi", AgentSession: &HerdrAgentSession{Value: "a"}}},
		},
	}
	socketPath, stateDir := startFakeHerdr(t, snapshot)
	env := append(os.Environ(),
		"HERDR_SOCKET_PATH="+socketPath,
		"HERDR_PLUGIN_STATE_DIR="+stateDir,
		"HERDR_SESSION=hseh-test",
	)
	run := func(event, payload string) {
		cmd := exec.Command(compiledHseh, "event")
		cmd.Env = append(env, "HERDR_PLUGIN_EVENT="+event, "HERDR_PLUGIN_EVENT_JSON="+payload)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v\n%s", event, err, out)
		}
	}
	run("pane.agent_detected", `{"event":"pane_agent_detected","data":{"type":"pane_agent_detected","pane_id":"w1:p1","workspace_id":"w1","agent":"pi","released":false}}`)
	run("pane.agent_detected", `{"event":"pane_agent_detected","data":{"type":"pane_agent_detected","pane_id":"w1:p1","workspace_id":"w1","agent":"pi","released":true}}`)
	run("pane.moved", `{"event":"pane_moved","data":{"type":"pane_moved","previous_pane_id":"w1:p1","pane":{"pane_id":"w2:p9"}}}`)
}

func TestCompiledListDoesNotAdoptConversationWithoutMove(t *testing.T) {
	snapshot := HerdrSessionSnapshot{
		Version: "0.9.0",
		Agents: []HerdrAgentRow{
			{HerdrPaneRow: HerdrPaneRow{PaneID: "w2:p9", Agent: "pi", AgentSession: &HerdrAgentSession{Value: "shared"}}},
		},
	}
	socketPath, stateDir := startFakeHerdr(t, snapshot)
	t.Setenv("HERDR_SOCKET_PATH", socketPath)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
	t.Setenv("HERDR_SESSION", "hseh-test")
	witness, err := ReadServerContinuityWitness()
	if err != nil {
		t.Fatal(err)
	}
	history := emptyFocusHistory(witness)
	history = ApplyVerifiedOccupantTransition(history, "w1:p1", "pi", "shared", false)
	history = RecordAgentPaneFocus(history, "w1:p1")
	if err := writeFocusHistoryFile(stateDir, history); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(compiledHseh, "list", "--json", "--view", "agents")
	cmd.Env = append(os.Environ(),
		"HERDR_SOCKET_PATH="+socketPath,
		"HERDR_PLUGIN_STATE_DIR="+stateDir,
		"HERDR_SESSION=hseh-test",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, err := loadFocusHistoryFile(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range got.Agents {
		if id.PaneID == "w2:p9" {
			t.Fatalf("list prune transferred conversation without move: %+v", got.Agents)
		}
	}
}

func TestCompiledEventHookMoveAfterSnapshotDrop(t *testing.T) {
	snapshot := HerdrSessionSnapshot{
		Version: "0.9.0",
		Agents: []HerdrAgentRow{
			{HerdrPaneRow: HerdrPaneRow{PaneID: "w2:p9", Agent: "pi", AgentSession: &HerdrAgentSession{Value: "a"}}},
		},
	}
	socketPath, stateDir := startFakeHerdr(t, snapshot)
	t.Setenv("HERDR_SOCKET_PATH", socketPath)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
	t.Setenv("HERDR_SESSION", "hseh-test")
	witness, err := ReadServerContinuityWitness()
	if err != nil {
		t.Fatal(err)
	}
	history := emptyFocusHistory(witness)
	history = ApplyVerifiedOccupantTransition(history, "w1:p1", "pi", "a", false)
	history = RecordAgentPaneFocus(history, "w1:p1")
	if err := writeFocusHistoryFile(stateDir, history); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(compiledHseh, "event")
	cmd.Env = append(os.Environ(),
		"HERDR_SOCKET_PATH="+socketPath,
		"HERDR_PLUGIN_STATE_DIR="+stateDir,
		"HERDR_SESSION=hseh-test",
		"HERDR_PLUGIN_EVENT=pane.moved",
		`HERDR_PLUGIN_EVENT_JSON={"event":"pane_moved","data":{"type":"pane_moved","previous_pane_id":"w1:p1","pane":{"pane_id":"w2:p9"}}}`,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, err := loadFocusHistoryFile(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Agents) != 1 || got.Agents[0].PaneID != "w2:p9" {
		t.Fatalf("compiled move lost history: %+v", got.Agents)
	}
}
