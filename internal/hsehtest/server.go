package hsehtest

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/regutierrez/hseh/internal/herdr"
)

// Server is an in-process stand-in for the Herdr API socket. Tests configure
// the snapshot and failure behaviour, call Start, and point HERDR_SOCKET_PATH
// at the returned socket. Every request is answered on its own connection,
// matching Herdr's one-request-per-connection protocol.
type Server struct {
	// Snapshot is returned by session.snapshot and grows as workspaces are created.
	Snapshot herdr.SessionSnapshot
	// FailMethod, when set, makes that method fail on its FailAfter-th call (default first).
	FailMethod string
	FailAfter  int
	// SnapshotDelay slows session.snapshot.
	SnapshotDelay time.Duration
	// PaneReadText is the pane.read body; empty means "hello".
	PaneReadText string

	// Reads counts pane.read calls; Focused and FocusedTab hold the last
	// workspace.focus and tab.focus ids.
	Reads      atomic.Int32
	Focused    atomic.Value
	FocusedTab atomic.Value

	mu               sync.Mutex
	failHits         int
	methods          []string
	commands         []string
	created          []string
	closedWorkspaces []string
	closedPanes      []string
	popupOpens       []PopupOpen
	nextWorkspace    int
}

// PopupOpen is the geometry of one plugin.pane.open call. Width and Height
// decode as string ("70%") or float64 (cells), mirroring the JSON Herdr sees.
type PopupOpen struct {
	Entrypoint string `json:"entrypoint"`
	Width      any    `json:"width"`
	Height     any    `json:"height"`
}

// Start listens on a fresh unix socket and returns it with an empty state dir.
func Start(t *testing.T, s *Server) (socketPath, stateDir string) {
	t.Helper()
	dir := t.TempDir()
	socketPath = filepath.Join(dir, "herdr.sock")
	stateDir = filepath.Join(dir, "state")
	if s.nextWorkspace == 0 {
		s.nextWorkspace = len(s.Snapshot.Workspaces)
	}
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
			go s.serve(conn)
		}
	}()
	return socketPath, stateDir
}

// Methods lists every method called so far, in order.
func (s *Server) Methods() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.methods...)
}

// Commands lists the text of every pane.send_input call.
func (s *Server) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.commands...)
}

// Created lists workspace ids produced by workspace.create.
func (s *Server) Created() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.created...)
}

// ClosedWorkspaces lists workspace ids passed to workspace.close.
func (s *Server) ClosedWorkspaces() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.closedWorkspaces...)
}

// ClosedPanes lists pane ids passed to pane.close.
func (s *Server) ClosedPanes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.closedPanes...)
}

// PopupOpens lists every plugin.pane.open call so far, in order.
func (s *Server) PopupOpens() []PopupOpen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]PopupOpen{}, s.popupOpens...)
}

// Count returns how many times method was called.
func (s *Server) Count(method string) int {
	n := 0
	for _, m := range s.Methods() {
		if m == method {
			n++
		}
	}
	return n
}

// applyWorkspaceClose removes a workspace and its tabs, panes, agents, and layouts.
// Caller must hold s.mu.
func (s *Server) applyWorkspaceClose(workspaceID string) {
	if workspaceID == "" {
		return
	}
	s.closedWorkspaces = append(s.closedWorkspaces, workspaceID)
	tabIDs := map[string]bool{}
	for _, workspace := range s.Snapshot.Workspaces {
		if workspace.WorkspaceID == workspaceID && workspace.ActiveTabID != "" {
			tabIDs[workspace.ActiveTabID] = true
		}
	}
	var panes []herdr.PaneRow
	for _, pane := range s.Snapshot.Panes {
		if pane.WorkspaceID == workspaceID {
			tabIDs[pane.TabID] = true
			if s.Snapshot.FocusedPaneID == pane.PaneID {
				s.Snapshot.FocusedPaneID = ""
			}
			continue
		}
		panes = append(panes, pane)
	}
	s.Snapshot.Panes = panes
	var workspaces []herdr.WorkspaceRow
	for _, workspace := range s.Snapshot.Workspaces {
		if workspace.WorkspaceID != workspaceID {
			workspaces = append(workspaces, workspace)
		}
	}
	s.Snapshot.Workspaces = workspaces
	var agents []herdr.AgentRow
	for _, agent := range s.Snapshot.Agents {
		if agent.WorkspaceID != workspaceID {
			agents = append(agents, agent)
		}
	}
	s.Snapshot.Agents = agents
	s.dropTabs(tabIDs)
	if s.Snapshot.FocusedWorkspaceID == workspaceID {
		s.Snapshot.FocusedWorkspaceID = ""
	}
}

// applyPaneClose removes a pane. If it was the last pane in its tab, the tab
// goes with it; if that was the last tab, the workspace goes too.
// Caller must hold s.mu.
func (s *Server) applyPaneClose(paneID string) {
	if paneID == "" {
		return
	}
	s.closedPanes = append(s.closedPanes, paneID)
	tabID, workspaceID := "", ""
	var panes []herdr.PaneRow
	for _, pane := range s.Snapshot.Panes {
		if pane.PaneID == paneID {
			tabID = pane.TabID
			workspaceID = pane.WorkspaceID
			continue
		}
		panes = append(panes, pane)
	}
	s.Snapshot.Panes = panes
	var agents []herdr.AgentRow
	for _, agent := range s.Snapshot.Agents {
		if agent.PaneID != paneID {
			agents = append(agents, agent)
		}
	}
	s.Snapshot.Agents = agents
	if s.Snapshot.FocusedPaneID == paneID {
		s.Snapshot.FocusedPaneID = ""
	}
	if tabID == "" {
		return
	}
	if hasPaneInTab(s.Snapshot.Panes, tabID) {
		return
	}
	s.dropTabs(map[string]bool{tabID: true})
	if workspaceID == "" || hasPaneInWorkspace(s.Snapshot.Panes, workspaceID) {
		s.repointWorkspaceTab(workspaceID)
		return
	}
	s.applyWorkspaceClose(workspaceID)
}

func (s *Server) dropTabs(tabIDs map[string]bool) {
	if len(tabIDs) == 0 {
		return
	}
	var tabs []herdr.TabRow
	for _, tab := range s.Snapshot.Tabs {
		if !tabIDs[tab.TabID] {
			tabs = append(tabs, tab)
		}
	}
	s.Snapshot.Tabs = tabs
	var layouts []herdr.PaneLayout
	for _, layout := range s.Snapshot.Layouts {
		if !tabIDs[layout.TabID] {
			layouts = append(layouts, layout)
		}
	}
	s.Snapshot.Layouts = layouts
}

func (s *Server) repointWorkspaceTab(workspaceID string) {
	if workspaceID == "" {
		return
	}
	next := ""
	for _, pane := range s.Snapshot.Panes {
		if pane.WorkspaceID == workspaceID {
			next = pane.TabID
			break
		}
	}
	for i, workspace := range s.Snapshot.Workspaces {
		if workspace.WorkspaceID == workspaceID {
			s.Snapshot.Workspaces[i].ActiveTabID = next
			return
		}
	}
}

func hasPaneInTab(panes []herdr.PaneRow, tabID string) bool {
	for _, pane := range panes {
		if pane.TabID == tabID {
			return true
		}
	}
	return false
}

func hasPaneInWorkspace(panes []herdr.PaneRow, workspaceID string) bool {
	for _, pane := range panes {
		if pane.WorkspaceID == workspaceID {
			return true
		}
	}
	return false
}

func (s *Server) serve(conn net.Conn) {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.methods = append(s.methods, req.Method)
	if req.Method == "session.snapshot" && s.SnapshotDelay > 0 {
		delay := s.SnapshotDelay
		s.mu.Unlock()
		time.Sleep(delay)
		s.mu.Lock()
	}
	if s.FailMethod != "" && req.Method == s.FailMethod {
		s.failHits++
		after := s.FailAfter
		if after == 0 {
			after = 1
		}
		if s.failHits >= after {
			payload, _ := json.Marshal(map[string]any{"id": req.ID, "error": map[string]any{"code": "failed", "message": "injected " + req.Method}})
			_, _ = conn.Write(append(payload, '\n'))
			return
		}
	}
	var result any
	switch req.Method {
	case "session.snapshot":
		snap := s.Snapshot
		result = herdr.SnapshotEnvelope{Type: "session_snapshot", Snapshot: &snap}
	case "pane.read":
		s.Reads.Add(1)
		text := s.PaneReadText
		if text == "" {
			text = "hello"
		}
		result = herdr.PaneReadEnvelope{Type: "pane_read", Read: &herdr.PaneReadResult{Text: text}}
	case "plugin.pane.open":
		var params PopupOpen
		_ = json.Unmarshal(req.Params, &params)
		s.popupOpens = append(s.popupOpens, params)
		result = map[string]any{"type": "ok"}
	case "workspace.focus":
		var params struct {
			WorkspaceID string `json:"workspace_id"`
		}
		_ = json.Unmarshal(req.Params, &params)
		s.Focused.Store(params.WorkspaceID)
		result = map[string]any{"type": "ok"}
	case "tab.focus":
		var params struct {
			TabID string `json:"tab_id"`
		}
		_ = json.Unmarshal(req.Params, &params)
		s.FocusedTab.Store(params.TabID)
		result = map[string]any{"type": "ok"}
	case "workspace.create":
		var params struct {
			Cwd   string `json:"cwd"`
			Label string `json:"label"`
		}
		_ = json.Unmarshal(req.Params, &params)
		s.nextWorkspace++
		ws := "w" + strconv.Itoa(s.nextWorkspace)
		tab := ws + ":t1"
		pane := ws + ":p1"
		s.created = append(s.created, ws)
		s.Snapshot.Workspaces = append(s.Snapshot.Workspaces, herdr.WorkspaceRow{WorkspaceID: ws, Label: params.Label, ActiveTabID: tab})
		s.Snapshot.Tabs = append(s.Snapshot.Tabs, herdr.TabRow{TabID: tab, Label: params.Label})
		s.Snapshot.Panes = append(s.Snapshot.Panes, herdr.PaneRow{PaneID: pane, WorkspaceID: ws, TabID: tab, Cwd: params.Cwd})
		s.Snapshot.Layouts = append(s.Snapshot.Layouts, herdr.PaneLayout{TabID: tab, FocusedPaneID: pane})
		result = map[string]any{
			"type":      "workspace_created",
			"workspace": map[string]any{"workspace_id": ws},
			"tab":       map[string]any{"tab_id": tab},
			"root_pane": map[string]any{"pane_id": pane},
		}
	case "tab.create":
		var params struct {
			WorkspaceID string `json:"workspace_id"`
			Label       string `json:"label"`
			Cwd         string `json:"cwd"`
		}
		_ = json.Unmarshal(req.Params, &params)
		tab := params.WorkspaceID + ":t-extra"
		pane := params.WorkspaceID + ":p-extra"
		result = map[string]any{"type": "tab_created", "tab": map[string]any{"tab_id": tab}, "root_pane": map[string]any{"pane_id": pane}}
	case "pane.split":
		result = map[string]any{"type": "pane_created", "pane": map[string]any{"pane_id": "w1:p-split"}}
	case "pane.send_input":
		var params struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(req.Params, &params)
		s.commands = append(s.commands, params.Text)
		result = map[string]any{"type": "ok"}
	case "workspace.close":
		var params struct {
			WorkspaceID string `json:"workspace_id"`
		}
		_ = json.Unmarshal(req.Params, &params)
		s.applyWorkspaceClose(params.WorkspaceID)
		result = map[string]any{"type": "ok"}
	case "pane.close":
		var params struct {
			PaneID string `json:"pane_id"`
		}
		_ = json.Unmarshal(req.Params, &params)
		s.applyPaneClose(params.PaneID)
		result = map[string]any{"type": "ok"}
	default:
		result = map[string]any{"type": "ok"}
	}
	payload, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
	_, _ = conn.Write(append(payload, '\n'))
}

// WriteDefinition writes a one-tab space definition under configDir/spaces.
func WriteDefinition(t *testing.T, configDir, id, name, work, command string) {
	t.Helper()
	body := "id = \"" + id + "\"\nname = \"" + name + "\"\nworking_dir = \"" + work + "\"\n[[tabs]]\nname = \"main\"\ncommand = \"" + command + "\"\n"
	WriteDefinitionTOML(t, configDir, name, body)
}

// WriteDefinitionTOML writes a raw definition body as configDir/spaces/<name>.toml.
func WriteDefinitionTOML(t *testing.T, configDir, name, body string) {
	t.Helper()
	dir := filepath.Join(configDir, "spaces")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Witness reads the continuity witness of the fake server listening at socket, the
// same way a real hseh call does, so seeded history passes the witness check.
func Witness(t *testing.T, socket string) herdr.ContinuityWitness {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	witness, err := herdr.ReadContinuityWitnessFromConn(conn, socket)
	if err != nil {
		t.Fatal(err)
	}
	return witness
}

// Git runs one git command in dir with hooks, signing and identity pinned so fixtures
// behave the same on every machine. It fails the test on a non-zero exit.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return string(out)
}

// Env is the process environment for a compiled hseh pointed at a fake server.
func Env(socket, state, config string) []string {
	return append(os.Environ(),
		"HERDR_SOCKET_PATH="+socket,
		"HERDR_PLUGIN_STATE_DIR="+state,
		"HERDR_PLUGIN_CONFIG_DIR="+config,
		"HERDR_SESSION=hseh-test",
	)
}

// Main runs a package's tests with HERDR_PLUGIN_CONFIG_DIR pointed at an empty
// directory, so in-process code that loads hseh.toml (tracing, popup size) never
// reads the developer's real config or writes to a real trace file.
func Main(m *testing.M) {
	dir, err := os.MkdirTemp("", "hseh-test-config-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
