package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type recordingHerdr struct {
	mu            sync.Mutex
	snapshot      HerdrSessionSnapshot
	methods       []string
	commands      []string
	failMethod    string
	failAfter     int
	failHits      int
	created       []string
	nextWorkspace int
	snapshotDelay time.Duration
	createDelay   time.Duration
}

func startRecordingHerdr(t *testing.T, h *recordingHerdr) (socketPath, stateDir string) {
	t.Helper()
	dir := t.TempDir()
	socketPath = filepath.Join(dir, "herdr.sock")
	stateDir = filepath.Join(dir, "state")
	if h.nextWorkspace == 0 {
		h.nextWorkspace = len(h.snapshot.Workspaces)
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
			go h.serve(conn)
		}
	}()
	return socketPath, stateDir
}

func (h *recordingHerdr) serve(conn net.Conn) {
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
	h.mu.Lock()
	defer h.mu.Unlock()
	h.methods = append(h.methods, req.Method)
	if req.Method == "session.snapshot" && h.snapshotDelay > 0 {
		delay := h.snapshotDelay
		h.mu.Unlock()
		time.Sleep(delay)
		h.mu.Lock()
	}
	if req.Method == "workspace.create" && h.createDelay > 0 {
		delay := h.createDelay
		h.mu.Unlock()
		time.Sleep(delay)
		h.mu.Lock()
	}
	if h.failMethod != "" && req.Method == h.failMethod {
		h.failHits++
		after := h.failAfter
		if after == 0 {
			after = 1
		}
		if h.failHits >= after {
			payload, _ := json.Marshal(map[string]any{"id": req.ID, "error": map[string]any{"code": "failed", "message": "injected " + req.Method}})
			_, _ = conn.Write(append(payload, '\n'))
			return
		}
	}
	var result any
	switch req.Method {
	case "session.snapshot":
		snap := h.snapshot
		result = herdrSnapshotEnvelope{Type: "session_snapshot", Snapshot: &snap}
	case "workspace.focus":
		result = map[string]any{"type": "ok"}
	case "agent.focus":
		var params struct {
			Target string `json:"target"`
		}
		_ = json.Unmarshal(req.Params, &params)
		agent := HerdrAgentRow{HerdrPaneRow: HerdrPaneRow{PaneID: params.Target}}
		for _, row := range h.snapshot.Agents {
			if row.PaneID == params.Target {
				agent = row
				break
			}
		}
		result = herdrAgentFocusEnvelope{Type: "agent_info", Agent: &agent}
	case "tab.focus":
		result = map[string]any{"type": "ok"}
	case "workspace.create":
		var params struct {
			Cwd   string `json:"cwd"`
			Label string `json:"label"`
		}
		_ = json.Unmarshal(req.Params, &params)
		h.nextWorkspace++
		ws := "w" + itoaDecimal(h.nextWorkspace)
		tab := ws + ":t1"
		pane := ws + ":p1"
		h.created = append(h.created, ws)
		h.snapshot.Workspaces = append(h.snapshot.Workspaces, HerdrWorkspaceRow{WorkspaceID: ws, Label: params.Label, ActiveTabID: tab})
		h.snapshot.Tabs = append(h.snapshot.Tabs, HerdrTabRow{TabID: tab, WorkspaceID: ws, Label: params.Label})
		h.snapshot.Panes = append(h.snapshot.Panes, HerdrPaneRow{PaneID: pane, WorkspaceID: ws, TabID: tab, Cwd: params.Cwd})
		h.snapshot.Layouts = append(h.snapshot.Layouts, HerdrPaneLayout{WorkspaceID: ws, TabID: tab, FocusedPaneID: pane})
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
	case "tab.rename", "tab.close", "pane.rename":
		result = map[string]any{"type": "ok"}
	case "pane.split":
		result = map[string]any{"type": "pane_created", "pane": map[string]any{"pane_id": "w1:p-split"}}
	case "pane.send_input":
		var params struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(req.Params, &params)
		h.commands = append(h.commands, params.Text)
		result = map[string]any{"type": "ok"}
	default:
		result = map[string]any{"type": "ok"}
	}
	payload, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
	_, _ = conn.Write(append(payload, '\n'))
}

func itoaDecimal(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func writeTestDefinition(t *testing.T, configDir, id, name, work, command string) {
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

func hsehOpenEnv(socket, state, config string) []string {
	return append(os.Environ(),
		"HERDR_SOCKET_PATH="+socket,
		"HERDR_PLUGIN_STATE_DIR="+state,
		"HERDR_PLUGIN_CONFIG_DIR="+config,
		"HERDR_SESSION=hseh-test",
	)
}

func TestCompiledOpenCreatesThenFocusesWithoutRepeatingCommands(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-one", "one", work, "echo MARK")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}}
	socket, state := startRecordingHerdr(t, h)
	run := func() []byte {
		cmd := exec.Command(compiledHseh, "open", "def-one")
		cmd.Env = hsehOpenEnv(socket, state, config)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return out
	}
	first := run()
	if !strings.Contains(string(first), "create") {
		t.Fatalf("first open: %s", first)
	}
	second := run()
	if !strings.Contains(string(second), "focus") {
		t.Fatalf("second open: %s", second)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != 1 {
		t.Fatalf("created %v", h.created)
	}
	if len(h.commands) != 1 || h.commands[0] != "echo MARK" {
		t.Fatalf("commands %v", h.commands)
	}
}

func TestCompiledOpenAdoptsUnassociatedSameDir(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-adopt", "adoptme", work, "echo SHOULD_NOT_RUN")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{
		Version:    "0.9.0",
		Workspaces: []HerdrWorkspaceRow{{WorkspaceID: "w7", Label: "live", ActiveTabID: "w7:t1"}},
		Panes:      []HerdrPaneRow{{PaneID: "w7:p1", WorkspaceID: "w7", Cwd: work}},
	}}
	socket, state := startRecordingHerdr(t, h)
	cmd := exec.Command(compiledHseh, "open", "def-adopt")
	cmd.Env = hsehOpenEnv(socket, state, config)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(string(out), "adopt") {
		t.Fatalf("want adopt, got %s", out)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != 0 {
		t.Fatalf("created %v", h.created)
	}
	if len(h.commands) != 0 {
		t.Fatalf("commands %v", h.commands)
	}
}

func TestCompiledOpenMissingDirDoesNotCreate(t *testing.T) {
	config := t.TempDir()
	writeTestDefinition(t, config, "def-missing", "missing", filepath.Join(t.TempDir(), "nope"), "echo X")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}}
	socket, state := startRecordingHerdr(t, h)
	cmd := exec.Command(compiledHseh, "open", "def-missing")
	cmd.Env = hsehOpenEnv(socket, state, config)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected error, got %s", out)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, method := range h.methods {
		if method == "workspace.create" {
			t.Fatalf("created despite missing dir: %v", h.methods)
		}
	}
}

func TestCompiledOpenRetainsWorkspaceWhenTabCreateFails(t *testing.T) {
	work := t.TempDir()
	sub := filepath.Join(work, "web")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	config := t.TempDir()
	dir := filepath.Join(config, "spaces")
	os.MkdirAll(dir, 0o700)
	body := "id = \"def-partial\"\nname = \"partial\"\nworking_dir = \"" + work + "\"\n" +
		"[[tabs]]\nname = \"root\"\n" +
		"[[tabs]]\nname = \"web\"\nworking_dir = \"web\"\ncommand = \"echo SECOND\"\n"
	os.WriteFile(filepath.Join(dir, "partial.toml"), []byte(body), 0o600)
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}, failMethod: "tab.create"}
	socket, state := startRecordingHerdr(t, h)
	cmd := exec.Command(compiledHseh, "open", "def-partial")
	cmd.Env = hsehOpenEnv(socket, state, config)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected tab.create failure, got %s", out)
	}
	if !strings.Contains(string(out), "tab.create") {
		t.Fatalf("error should name failed step: %s", out)
	}
	h.mu.Lock()
	if len(h.created) != 1 {
		t.Fatalf("expected retained workspace, created %v", h.created)
	}
	h.mu.Unlock()
	payload, err := os.ReadFile(filepath.Join(state, "hseh-test", "associations.json"))
	if err != nil {
		t.Fatalf("association missing after partial create: %v", err)
	}
	if !strings.Contains(string(payload), h.created[0]) {
		t.Fatalf("association does not retain workspace: %s", payload)
	}
}

func TestCompiledOpenConcurrentCreatesOnce(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-race", "race", work, "echo ONCE")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}}
	socket, state := startRecordingHerdr(t, h)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(compiledHseh, "open", "def-race")
			cmd.Env = hsehOpenEnv(socket, state, config)
			out, err := cmd.CombinedOutput()
			if err != nil {
				errs[i] = err
				t.Errorf("%v\n%s", err, out)
			}
		}(i)
	}
	wg.Wait()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != 1 {
		t.Fatalf("created %v methods %v", h.created, h.methods)
	}
	if len(h.commands) != 1 {
		t.Fatalf("commands %v", h.commands)
	}
}

func TestCompiledOpenDistinctDefinitionsSameDir(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-a", "alpha", work, "echo A")
	writeTestDefinition(t, config, "def-b", "beta", work, "echo B")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}}
	socket, state := startRecordingHerdr(t, h)
	for _, id := range []string{"def-a", "def-b"} {
		cmd := exec.Command(compiledHseh, "open", id)
		cmd.Env = hsehOpenEnv(socket, state, config)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", id, err, out)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != 2 {
		t.Fatalf("want two workspaces, created %v", h.created)
	}
	if len(h.commands) != 2 {
		t.Fatalf("commands %v", h.commands)
	}
}

func TestCompiledOpenStopsOnMismatchedAssociationWitness(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-restart", "restart", work, "echo NO")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}}
	socket, state := startRecordingHerdr(t, h)
	t.Setenv("HERDR_SESSION", "hseh-test")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	stale := SpaceAssociationState{
		Witness: ServerContinuityWitness{SocketPath: socket, PeerPID: 999999, PeerStartTime: "1", BootTime: "1"},
		Records: []SpaceAssociationRecord{{DefinitionID: "def-restart", ResolvedDir: work, WorkspaceID: "w9"}},
	}
	if err := writeSpaceAssociationFile(state, stale); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(compiledHseh, "open", "def-restart")
	cmd.Env = hsehOpenEnv(socket, state, config)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected unresolved restart, got %s", out)
	}
	if !strings.Contains(string(out), "needs recovery after Herdr restart") {
		t.Fatalf("got %s", out)
	}
	if !strings.Contains(string(out), "hseh recover def-restart --workspace <live-workspace-id>") || !strings.Contains(string(out), "hseh recover def-restart --create") {
		t.Fatalf("missing recovery commands: %s", out)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, method := range h.methods {
		if method == "workspace.create" || method == "workspace.focus" {
			t.Fatalf("mutated on unresolved restart: %v", h.methods)
		}
	}
}

func TestCompiledOpenCorruptAssociationDoesNotCreate(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-corrupt", "corrupt", work, "echo NO")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}}
	socket, state := startRecordingHerdr(t, h)
	os.MkdirAll(filepath.Join(state, "hseh-test"), 0o700)
	if err := os.WriteFile(filepath.Join(state, "hseh-test", "associations.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(compiledHseh, "open", "def-corrupt")
	cmd.Env = hsehOpenEnv(socket, state, config)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected corrupt error, got %s", out)
	}
	if !strings.Contains(string(out), "corrupt") {
		t.Fatalf("got %s", out)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != 0 {
		t.Fatalf("created %v", h.created)
	}
}

func TestUnopenedDefinitionListedUntilAssociated(t *testing.T) {
	work := t.TempDir()
	def := SpaceDefinition{ID: "def-list", Name: "listed", ResolvedDir: work, WorkingDir: work, Description: "desc"}
	snapshot := HerdrSessionSnapshot{Workspaces: []HerdrWorkspaceRow{{WorkspaceID: "w1", Label: "live"}}}
	items := appendUnopenedDefinitionItems(BuildPickerItemsWithLayout("spaces", snapshot, emptyFocusHistory(ServerContinuityWitness{}), defaultSidebarLayout()), "spaces", snapshot, []SpaceDefinition{def}, nil, nil)
	if len(items) != 2 || items[1].Kind != pickerKindDefinition {
		t.Fatalf("%+v", items)
	}
	records := []SpaceAssociationRecord{{DefinitionID: "def-list", ResolvedDir: work, WorkspaceID: "w1"}}
	hidden := appendUnopenedDefinitionItems(BuildPickerItemsWithLayout("spaces", snapshot, emptyFocusHistory(ServerContinuityWitness{}), defaultSidebarLayout()), "spaces", snapshot, []SpaceDefinition{def}, records, nil)
	if len(hidden) != 1 {
		t.Fatalf("associated definition still listed: %+v", hidden)
	}
	if items[0].ID == items[1].ID {
		t.Fatalf("selection ids collided: %+v", items)
	}
}

func TestDefinitionPickerSanitizesDisplayNotCommand(t *testing.T) {
	def := SpaceDefinition{
		ID: "w1", Name: "n\x1b[31mX", Description: "d\x1b]0;title\x07",
		ResolvedDir: "/tmp/x", Tabs: []SpaceDefinitionTab{{Name: "t", Command: "echo \x1b[31mKEEP"}},
	}
	item := definitionPickerItem(def, false)
	if strings.Contains(item.Rows[0], "\x1b") || strings.Contains(item.PreviewText, "\x1b") {
		t.Fatalf("display still has controls: %+v %q", item.Rows, item.PreviewText)
	}
	if def.Tabs[0].Command != "echo \x1b[31mKEEP" {
		t.Fatalf("execution command rewritten: %q", def.Tabs[0].Command)
	}
	if item.DefinitionID != "w1" || item.ID == "w1" {
		t.Fatalf("need namespaced selection id: %+v", item)
	}
}

func TestOpenReusableSpaceCancelBeforeMutation(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-cancel", "cancel", work, "echo NO")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}, snapshotDelay: 400 * time.Millisecond}
	socket, state := startRecordingHerdr(t, h)
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", config)
	t.Setenv("HERDR_SESSION", "hseh-test")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := OpenReusableSpace(ctx, "def-cancel")
		done <- err
	}()
	time.Sleep(40 * time.Millisecond)
	cancel()
	err := <-done
	if err == nil {
		t.Fatal("expected cancel")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != 0 {
		t.Fatalf("created after cancel: %v", h.created)
	}
}

func TestOpenReusableSpaceLockedCancelBeforeMutation(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-lock", "lock", work, "echo NO")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}}
	socket, state := startRecordingHerdr(t, h)
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", config)
	t.Setenv("HERDR_SESSION", "hseh-test")
	release := make(chan struct{})
	go func() {
		_ = withExclusiveFileLock(spaceOpenLockPath(state), func() error {
			<-release
			return nil
		})
	}()
	time.Sleep(20 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := OpenReusableSpace(ctx, "def-lock")
		done <- err
	}()
	time.Sleep(40 * time.Millisecond)
	cancel()
	err := <-done
	close(release)
	if err == nil {
		t.Fatal("expected cancel while lock held")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != 0 {
		t.Fatalf("created while cancelled: %v", h.created)
	}
}

func TestRepeatedEnterCreatesOnce(t *testing.T) {
	work := t.TempDir()
	snapshot := HerdrSessionSnapshot{Version: "0.9.0", Workspaces: []HerdrWorkspaceRow{{WorkspaceID: "w0", Label: "other"}}}
	m := newPickerModel("spaces", snapshot, emptyFocusHistory(ServerContinuityWitness{}), defaultSidebarLayout(), 0, 80)
	m.setSpaceCatalog([]SpaceDefinition{{ID: "def-once", Name: "once", ResolvedDir: work, WorkingDir: work, Tabs: []SpaceDefinitionTab{{Name: "main", Command: "echo X"}}}}, nil, nil, nil)
	m.selectedID = pickerSelectionID(pickerKindDefinition, "def-once")
	first := m.startAccept()
	if first == nil {
		t.Fatal("first enter")
	}
	second := m.startAccept()
	if second != nil {
		t.Fatal("second enter started another accept")
	}
}

func TestPickerEscapeCancelsDefinitionAccept(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-esc", "esc", work, "echo NO")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}, snapshotDelay: 400 * time.Millisecond}
	socket, state := startRecordingHerdr(t, h)
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", config)
	t.Setenv("HERDR_SESSION", "hseh-test")
	m := newPickerModel("spaces", h.snapshot, emptyFocusHistory(ServerContinuityWitness{}), defaultSidebarLayout(), 0, 80)
	m.setSpaceCatalog([]SpaceDefinition{{ID: "def-esc", Name: "esc", ResolvedDir: work, WorkingDir: work, Tabs: []SpaceDefinitionTab{{Name: "main"}}}}, nil, nil, nil)
	m.selectedID = pickerSelectionID(pickerKindDefinition, "def-esc")
	cmd := m.startAccept()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	time.Sleep(30 * time.Millisecond)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(pickerModel)
	msg := <-done
	if _, ok := msg.(pickerErrorMsg); !ok && msg != nil {
		if _, ok := msg.(pickerAcceptedMsg); ok {
			t.Fatal("escape accepted create")
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != 0 {
		t.Fatalf("escape created %v", h.created)
	}
}

func TestOpenReusableSpaceReportsSubmittedCountWhenLaterCommandFails(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	dir := filepath.Join(config, "spaces")
	os.MkdirAll(dir, 0o700)
	body := "id = \"def-cmds\"\nname = \"cmds\"\nworking_dir = \"" + work + "\"\n" +
		"[[tabs]]\nname = \"a\"\ncommand = \"echo A\"\n" +
		"[[tabs]]\nname = \"b\"\ncommand = \"echo B\"\n"
	os.WriteFile(filepath.Join(dir, "cmds.toml"), []byte(body), 0o600)
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}, failMethod: "pane.send_input", failAfter: 2}
	socket, state := startRecordingHerdr(t, h)
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", config)
	t.Setenv("HERDR_SESSION", "hseh-test")
	result, err := OpenReusableSpace(context.Background(), "def-cmds")
	if err == nil {
		t.Fatal("expected command failure")
	}
	if !strings.Contains(err.Error(), "pane.send_input") {
		t.Fatalf("step %v", err)
	}
	if result.WorkspaceID == "" || result.SubmittedCommands != 1 {
		t.Fatalf("submitted=%d ws=%s", result.SubmittedCommands, result.WorkspaceID)
	}
}
