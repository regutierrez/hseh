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
	// Snapshot is returned by session.snapshot and grows as workspaces, tabs and panes are created.
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

	mu            sync.Mutex
	failHits      int
	methods       []string
	commands      []string
	created       []string
	popupOpens    []PopupOpen
	popupOpen     bool
	nextWorkspace int
	nextTab       int
	nextPane      int
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
	if s.nextTab == 0 {
		s.nextTab = len(s.Snapshot.Tabs)
	}
	if s.nextPane == 0 {
		s.nextPane = len(s.Snapshot.Panes)
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
			writeError(conn, req.ID, "failed", "injected "+req.Method)
			return
		}
	}
	result, errCode, errMsg := s.handle(req.Method, req.Params)
	if errCode != "" {
		writeError(conn, req.ID, errCode, errMsg)
		return
	}
	writeResult(conn, req.ID, result)
}

func (s *Server) handle(method string, raw json.RawMessage) (result any, errCode, errMsg string) {
	switch method {
	case "session.snapshot":
		snap := s.Snapshot
		return herdr.SnapshotEnvelope{Type: "session_snapshot", Snapshot: &snap}, "", ""
	case "pane.read":
		s.Reads.Add(1)
		text := s.PaneReadText
		if text == "" {
			text = "hello"
		}
		return herdr.PaneReadEnvelope{Type: "pane_read", Read: &herdr.PaneReadResult{Text: text}}, "", ""
	case "plugin.pane.open":
		var params PopupOpen
		_ = json.Unmarshal(raw, &params)
		s.popupOpens = append(s.popupOpens, params)
		if s.popupOpen {
			return nil, "ui_busy", "popup already open"
		}
		s.popupOpen = true
		return map[string]any{"type": "ok"}, "", ""
	case "workspace.focus":
		var params struct {
			WorkspaceID string `json:"workspace_id"`
		}
		_ = json.Unmarshal(raw, &params)
		s.Focused.Store(params.WorkspaceID)
		return map[string]any{"type": "ok"}, "", ""
	case "tab.focus":
		var params struct {
			TabID string `json:"tab_id"`
		}
		_ = json.Unmarshal(raw, &params)
		s.FocusedTab.Store(params.TabID)
		return map[string]any{"type": "ok"}, "", ""
	case "agent.focus":
		var params struct {
			Target string `json:"target"`
		}
		_ = json.Unmarshal(raw, &params)
		agent := herdr.AgentRow{PaneRow: herdr.PaneRow{PaneID: params.Target}}
		for _, row := range s.Snapshot.Agents {
			if row.PaneID == params.Target {
				agent = row
				break
			}
		}
		return herdr.AgentFocusEnvelope{Type: "agent_info", Agent: &agent}, "", ""
	case "workspace.create":
		var params struct {
			Cwd   string `json:"cwd"`
			Label string `json:"label"`
		}
		_ = json.Unmarshal(raw, &params)
		s.nextWorkspace++
		ws := "w" + strconv.Itoa(s.nextWorkspace)
		tab := s.allocTabID(ws)
		pane := s.allocPaneID(ws)
		s.created = append(s.created, ws)
		s.Snapshot.Workspaces = append(s.Snapshot.Workspaces, herdr.WorkspaceRow{WorkspaceID: ws, Label: params.Label, ActiveTabID: tab})
		s.Snapshot.Tabs = append(s.Snapshot.Tabs, herdr.TabRow{TabID: tab, Label: params.Label})
		s.Snapshot.Panes = append(s.Snapshot.Panes, herdr.PaneRow{PaneID: pane, WorkspaceID: ws, TabID: tab, Cwd: params.Cwd})
		s.Snapshot.Layouts = append(s.Snapshot.Layouts, herdr.PaneLayout{TabID: tab, FocusedPaneID: pane})
		return map[string]any{
			"type":      "workspace_created",
			"workspace": map[string]any{"workspace_id": ws},
			"tab":       map[string]any{"tab_id": tab},
			"root_pane": map[string]any{"pane_id": pane},
		}, "", ""
	case "tab.create":
		var params struct {
			WorkspaceID string `json:"workspace_id"`
			Label       string `json:"label"`
			Cwd         string `json:"cwd"`
			Focus       bool   `json:"focus"`
		}
		_ = json.Unmarshal(raw, &params)
		if !s.workspaceExists(params.WorkspaceID) {
			return nil, "not_found", "workspace " + params.WorkspaceID + " not found"
		}
		tab := s.allocTabID(params.WorkspaceID)
		pane := s.allocPaneID(params.WorkspaceID)
		s.Snapshot.Tabs = append(s.Snapshot.Tabs, herdr.TabRow{TabID: tab, Label: params.Label})
		s.Snapshot.Panes = append(s.Snapshot.Panes, herdr.PaneRow{PaneID: pane, WorkspaceID: params.WorkspaceID, TabID: tab, Cwd: params.Cwd})
		s.Snapshot.Layouts = append(s.Snapshot.Layouts, herdr.PaneLayout{TabID: tab, FocusedPaneID: pane})
		if params.Focus {
			s.setWorkspaceActiveTab(params.WorkspaceID, tab)
			s.Snapshot.FocusedPaneID = pane
		}
		return map[string]any{"type": "tab_created", "tab": map[string]any{"tab_id": tab}, "root_pane": map[string]any{"pane_id": pane}}, "", ""
	case "tab.rename":
		var params struct {
			TabID string `json:"tab_id"`
			Label string `json:"label"`
		}
		_ = json.Unmarshal(raw, &params)
		if !s.renameTab(params.TabID, params.Label) {
			return nil, "not_found", "tab " + params.TabID + " not found"
		}
		return map[string]any{"type": "ok"}, "", ""
	case "tab.close":
		var params struct {
			TabID string `json:"tab_id"`
		}
		_ = json.Unmarshal(raw, &params)
		if !s.closeTab(params.TabID) {
			return nil, "not_found", "tab " + params.TabID + " not found"
		}
		return map[string]any{"type": "ok"}, "", ""
	case "pane.split":
		var params struct {
			TargetPaneID string `json:"target_pane_id"`
			Cwd          string `json:"cwd"`
			Focus        bool   `json:"focus"`
		}
		_ = json.Unmarshal(raw, &params)
		target, ok := s.findPane(params.TargetPaneID)
		if !ok {
			return nil, "not_found", "pane " + params.TargetPaneID + " not found"
		}
		cwd := params.Cwd
		if cwd == "" {
			cwd = target.Cwd
		}
		pane := s.allocPaneID(target.WorkspaceID)
		s.Snapshot.Panes = append(s.Snapshot.Panes, herdr.PaneRow{PaneID: pane, WorkspaceID: target.WorkspaceID, TabID: target.TabID, Cwd: cwd})
		if params.Focus {
			s.setFocusedPane(target.TabID, pane)
			s.Snapshot.FocusedPaneID = pane
		}
		return map[string]any{"type": "pane_created", "pane": map[string]any{"pane_id": pane}}, "", ""
	case "pane.rename":
		var params struct {
			PaneID string `json:"pane_id"`
			Label  string `json:"label"`
		}
		_ = json.Unmarshal(raw, &params)
		if !s.renamePane(params.PaneID, params.Label) {
			return nil, "not_found", "pane " + params.PaneID + " not found"
		}
		return map[string]any{"type": "ok"}, "", ""
	case "pane.send_input":
		var params struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(raw, &params)
		s.commands = append(s.commands, params.Text)
		return map[string]any{"type": "ok"}, "", ""
	default:
		return nil, "unknown_method", "unknown method "+method
	}
}

func (s *Server) allocTabID(workspaceID string) string {
	s.nextTab++
	return workspaceID + ":t" + strconv.Itoa(s.nextTab)
}

func (s *Server) allocPaneID(workspaceID string) string {
	s.nextPane++
	return workspaceID + ":p" + strconv.Itoa(s.nextPane)
}

func (s *Server) workspaceExists(id string) bool {
	for _, ws := range s.Snapshot.Workspaces {
		if ws.WorkspaceID == id {
			return true
		}
	}
	return false
}

func (s *Server) findPane(id string) (herdr.PaneRow, bool) {
	for _, pane := range s.Snapshot.Panes {
		if pane.PaneID == id {
			return pane, true
		}
	}
	return herdr.PaneRow{}, false
}

func (s *Server) setWorkspaceActiveTab(workspaceID, tabID string) {
	for i, ws := range s.Snapshot.Workspaces {
		if ws.WorkspaceID == workspaceID {
			s.Snapshot.Workspaces[i].ActiveTabID = tabID
			return
		}
	}
}

func (s *Server) setFocusedPane(tabID, paneID string) {
	for i, layout := range s.Snapshot.Layouts {
		if layout.TabID == tabID {
			s.Snapshot.Layouts[i].FocusedPaneID = paneID
			return
		}
	}
	s.Snapshot.Layouts = append(s.Snapshot.Layouts, herdr.PaneLayout{TabID: tabID, FocusedPaneID: paneID})
}

func (s *Server) renameTab(tabID, label string) bool {
	for i, tab := range s.Snapshot.Tabs {
		if tab.TabID == tabID {
			s.Snapshot.Tabs[i].Label = label
			return true
		}
	}
	return false
}

func (s *Server) renamePane(paneID, label string) bool {
	for i, pane := range s.Snapshot.Panes {
		if pane.PaneID == paneID {
			s.Snapshot.Panes[i].Label = label
			return true
		}
	}
	return false
}

func (s *Server) closeTab(tabID string) bool {
	found := false
	tabs := make([]herdr.TabRow, 0, len(s.Snapshot.Tabs))
	for _, tab := range s.Snapshot.Tabs {
		if tab.TabID == tabID {
			found = true
			continue
		}
		tabs = append(tabs, tab)
	}
	if !found {
		return false
	}
	s.Snapshot.Tabs = tabs

	closedPanes := map[string]bool{}
	panes := make([]herdr.PaneRow, 0, len(s.Snapshot.Panes))
	for _, pane := range s.Snapshot.Panes {
		if pane.TabID == tabID {
			closedPanes[pane.PaneID] = true
			continue
		}
		panes = append(panes, pane)
	}
	s.Snapshot.Panes = panes

	layouts := make([]herdr.PaneLayout, 0, len(s.Snapshot.Layouts))
	for _, layout := range s.Snapshot.Layouts {
		if layout.TabID == tabID {
			continue
		}
		layouts = append(layouts, layout)
	}
	s.Snapshot.Layouts = layouts

	agents := make([]herdr.AgentRow, 0, len(s.Snapshot.Agents))
	for _, agent := range s.Snapshot.Agents {
		if agent.TabID == tabID {
			continue
		}
		agents = append(agents, agent)
	}
	s.Snapshot.Agents = agents

	if closedPanes[s.Snapshot.FocusedPaneID] {
		s.Snapshot.FocusedPaneID = ""
	}
	for i, ws := range s.Snapshot.Workspaces {
		if ws.ActiveTabID == tabID {
			s.Snapshot.Workspaces[i].ActiveTabID = s.firstTabInWorkspace(ws.WorkspaceID)
		}
	}
	return true
}

func (s *Server) firstTabInWorkspace(workspaceID string) string {
	for _, pane := range s.Snapshot.Panes {
		if pane.WorkspaceID == workspaceID {
			return pane.TabID
		}
	}
	return ""
}

func writeResult(conn net.Conn, id string, result any) {
	payload, _ := json.Marshal(map[string]any{"id": id, "result": result})
	_, _ = conn.Write(append(payload, '\n'))
}

func writeError(conn net.Conn, id, code, message string) {
	payload, _ := json.Marshal(map[string]any{"id": id, "error": map[string]any{"code": code, "message": message}})
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
