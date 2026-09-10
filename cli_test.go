package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/regutierrez/hseh/internal/focus"
	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
	"github.com/regutierrez/hseh/internal/picker"
)

// compiledHseh is the binary under test; every Compiled* test drives it end to end
// against an hsehtest.Server, with hsehtest.Env keeping it off the developer's real config.
var compiledHseh string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hseh-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// In-process helpers must not read the developer's real hseh.toml either.
	os.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)
	bin := filepath.Join(dir, "hseh")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build hseh:", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	compiledHseh = bin
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func runCompiledHseh(env []string, args ...string) (string, error) {
	cmd := exec.Command(compiledHseh, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// useHsehTestSession points in-process focus/space calls at the same session the compiled binary uses.
func useHsehTestSession(t *testing.T, stateDir string) {
	t.Helper()
	t.Setenv("HERDR_SESSION", "hseh-test")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
}

// seedAgentHistory writes history in which pane w1:p1 hosts a pi agent with the given
// session value and is the most recent agent target.
func seedAgentHistory(t *testing.T, socket, stateDir, sessionValue string) {
	t.Helper()
	useHsehTestSession(t, stateDir)
	history := focus.EmptyHistory(hsehtest.Witness(t, socket))
	history = focus.ApplyVerifiedOccupantTransition(history, "w1:p1", "pi", sessionValue, false)
	history = focus.RecordAgentPaneFocus(history, "w1:p1")
	if err := focus.WriteFile(stateDir, history); err != nil {
		t.Fatal(err)
	}
}

func TestCompiledCLIListJSON(t *testing.T) {
	snapshot := herdr.SessionSnapshot{
		FocusedWorkspaceID: "w1",
		Workspaces: []herdr.WorkspaceRow{
			{WorkspaceID: "w1", Label: "one", AgentStatus: "idle", ActiveTabID: "w1:t1"},
			{WorkspaceID: "w2", Label: "two", AgentStatus: "unknown", ActiveTabID: "w2:t1"},
		},
		Layouts: []herdr.PaneLayout{{TabID: "w1:t1", FocusedPaneID: "w1:p1"}},
		Agents: []herdr.AgentRow{
			{PaneRow: herdr.PaneRow{PaneID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Agent: "pi", AgentStatus: "idle", DisplayAgent: "pi"}, StateChangeSeq: 3},
		},
	}
	socketPath, stateDir := hsehtest.Start(t, &hsehtest.Server{Snapshot: snapshot})
	out, err := runCompiledHseh(hsehtest.Env(socketPath, stateDir, t.TempDir()), "list", "--json", "--view", "spaces")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var doc picker.ListDocument
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if doc.View != picker.ViewSpaces || doc.Session.Name != "hseh-test" {
		t.Fatalf("session/view: %+v", doc)
	}
	if len(doc.Items) != 2 {
		t.Fatalf("want 2 spaces, got %#v", doc.Items)
	}
	if doc.Items[0].Kind != picker.KindSpace || doc.Items[1].Kind != picker.KindSpace {
		t.Fatalf("order: %#v", doc.Items)
	}
	for _, item := range doc.Items {
		if item.Rows == nil {
			t.Fatalf("rows omitted incorrectly: %+v", item)
		}
	}
}

func TestCompiledCLIRejectsBadArguments(t *testing.T) {
	env := hsehtest.Env("", t.TempDir(), t.TempDir())
	cases := map[string][]string{
		"--json is required":         {"list"},
		"invalid view nope":          {"list", "--json", "--view", "nope"},
		"invalid view all":           {"list", "--json", "--view=all"},
		"unknown argument --yes":     {"recover", "def-a", "--create", "--yes"},
		"repeated argument --create": {"recover", "def-a", "--create", "--create"},
		"definition id is required":  {"open"},
		"unknown command frob":       {"frob"},
	}
	for want, args := range cases {
		out, err := runCompiledHseh(env, args...)
		if err == nil || !strings.Contains(out, want) {
			t.Fatalf("%v: err=%v out=%s want %q", args, err, out, want)
		}
	}
}

func TestCompiledLaunchOpensPluginPopup(t *testing.T) {
	snapshot := herdr.SessionSnapshot{}
	server := &hsehtest.Server{Snapshot: snapshot}
	socketPath, stateDir := hsehtest.Start(t, server)
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "hseh.toml"), []byte("popup_width = \"70%\"\npopup_height = 30\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runCompiledHseh(append(hsehtest.Env(socketPath, stateDir, configDir), "HERDR_PLUGIN_ID=hseh"), "launch", "spaces")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	opens := server.PopupOpens()
	if len(opens) == 0 {
		t.Fatal("launch did not call plugin.pane.open")
	}
	// The hedge may deliver the request twice; every copy must carry the configured size.
	for _, open := range opens {
		if open.Entrypoint != "spaces" || open.Width != "70%" || open.Height != float64(30) {
			t.Fatalf("plugin.pane.open params %+v, want spaces 70%% x 30 cells", open)
		}
	}
}

func TestCompiledEventHookReleaseAndMove(t *testing.T) {
	snapshot := herdr.SessionSnapshot{
		Agents: []herdr.AgentRow{
			{PaneRow: herdr.PaneRow{PaneID: "w1:p1", Agent: "pi", AgentSession: &herdr.AgentSession{Value: "a"}}},
		},
	}
	socketPath, stateDir := hsehtest.Start(t, &hsehtest.Server{Snapshot: snapshot})
	useHsehTestSession(t, stateDir)
	env := hsehtest.Env(socketPath, stateDir, t.TempDir())
	run := func(event, payload string) {
		out, err := runCompiledHseh(append(env, "HERDR_PLUGIN_EVENT="+event, "HERDR_PLUGIN_EVENT_JSON="+payload), "event")
		if err != nil {
			t.Fatalf("%s %v\n%s", event, err, out)
		}
	}
	run("pane.agent_detected", `{"event":"pane_agent_detected","data":{"type":"pane_agent_detected","pane_id":"w1:p1","workspace_id":"w1","agent":"pi","released":false}}`)
	run("pane.agent_detected", `{"event":"pane_agent_detected","data":{"type":"pane_agent_detected","pane_id":"w1:p1","workspace_id":"w1","agent":"pi","released":true}}`)
	run("pane.moved", `{"event":"pane_moved","data":{"type":"pane_moved","previous_pane_id":"w1:p1","pane":{"pane_id":"w2:p9"}}}`)
	got, err := focus.LoadFile(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if occupant, ok := got.Occupants["w2:p9"]; !ok || occupant.Generation != 2 {
		t.Fatalf("release must start a new generation and move must carry it to w2:p9: %+v", got.Occupants)
	}
}

func TestCompiledListDoesNotAdoptConversationWithoutMove(t *testing.T) {
	snapshot := herdr.SessionSnapshot{
		Agents: []herdr.AgentRow{
			{PaneRow: herdr.PaneRow{PaneID: "w2:p9", Agent: "pi", AgentSession: &herdr.AgentSession{Value: "shared"}}},
		},
	}
	socketPath, stateDir := hsehtest.Start(t, &hsehtest.Server{Snapshot: snapshot})
	seedAgentHistory(t, socketPath, stateDir, "shared")
	if out, err := runCompiledHseh(hsehtest.Env(socketPath, stateDir, t.TempDir()), "list", "--json", "--view", "agents"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, err := focus.LoadFile(stateDir)
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
	snapshot := herdr.SessionSnapshot{
		Agents: []herdr.AgentRow{
			{PaneRow: herdr.PaneRow{PaneID: "w2:p9", Agent: "pi", AgentSession: &herdr.AgentSession{Value: "a"}}},
		},
	}
	socketPath, stateDir := hsehtest.Start(t, &hsehtest.Server{Snapshot: snapshot})
	seedAgentHistory(t, socketPath, stateDir, "a")
	env := append(hsehtest.Env(socketPath, stateDir, t.TempDir()),
		"HERDR_PLUGIN_EVENT=pane.moved",
		`HERDR_PLUGIN_EVENT_JSON={"event":"pane_moved","data":{"type":"pane_moved","previous_pane_id":"w1:p1","pane":{"pane_id":"w2:p9"}}}`,
	)
	out, err := runCompiledHseh(env, "event")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, err := focus.LoadFile(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Agents) != 1 || got.Agents[0].PaneID != "w2:p9" || got.Agents[0].Generation != 1 {
		t.Fatalf("compiled move must rekey, not replace, the occupant: %+v", got.Agents)
	}
}
