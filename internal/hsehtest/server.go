package hsehtest

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
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
	// SnapshotDelay and CreateDelay slow session.snapshot and workspace.create.
	SnapshotDelay time.Duration
	CreateDelay   time.Duration
	// PaneReadText is the pane.read body; empty means "hello".
	PaneReadText string

	// Reads counts pane.read calls; Focused holds the last workspace.focus id.
	Reads   atomic.Int32
	Focused atomic.Value

	mu            sync.Mutex
	failHits      int
	methods       []string
	commands      []string
	created       []string
	nextWorkspace int
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
	if req.Method == "workspace.create" && s.CreateDelay > 0 {
		delay := s.CreateDelay
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
		var params struct {
			PaneID string `json:"pane_id"`
		}
		_ = json.Unmarshal(req.Params, &params)
		text := s.PaneReadText
		if text == "" {
			text = "hello"
		}
		result = herdr.PaneReadEnvelope{Type: "pane_read", Read: &herdr.PaneReadResult{PaneID: params.PaneID, Text: text, Format: "ansi", Source: "visible"}}
	case "plugin.pane.open":
		result = map[string]any{"type": "ok"}
	case "workspace.focus":
		var params struct {
			WorkspaceID string `json:"workspace_id"`
		}
		_ = json.Unmarshal(req.Params, &params)
		s.Focused.Store(params.WorkspaceID)
		result = map[string]any{"type": "ok"}
	case "agent.focus":
		var params struct {
			Target string `json:"target"`
		}
		_ = json.Unmarshal(req.Params, &params)
		agent := herdr.AgentRow{PaneRow: herdr.PaneRow{PaneID: params.Target}}
		for _, row := range s.Snapshot.Agents {
			if row.PaneID == params.Target {
				agent = row
				break
			}
		}
		result = herdr.AgentFocusEnvelope{Type: "agent_info", Agent: &agent}
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
		s.Snapshot.Tabs = append(s.Snapshot.Tabs, herdr.TabRow{TabID: tab, WorkspaceID: ws, Label: params.Label})
		s.Snapshot.Panes = append(s.Snapshot.Panes, herdr.PaneRow{PaneID: pane, WorkspaceID: ws, TabID: tab, Cwd: params.Cwd})
		s.Snapshot.Layouts = append(s.Snapshot.Layouts, herdr.PaneLayout{WorkspaceID: ws, TabID: tab, FocusedPaneID: pane})
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
	default:
		result = map[string]any{"type": "ok"}
	}
	payload, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
	_, _ = conn.Write(append(payload, '\n'))
}

// WriteDefinition writes a one-tab space definition under configDir/spaces.
func WriteDefinition(t *testing.T, configDir, id, name, work, command string) {
	t.Helper()
	dir := filepath.Join(configDir, "spaces")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "id = \"" + id + "\"\nname = \"" + name + "\"\nworking_dir = \"" + work + "\"\n[[tabs]]\nname = \"main\"\ncommand = \"" + command + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
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
