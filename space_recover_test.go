package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func useHsehTestSession(t *testing.T, stateDir string) {
	t.Helper()
	t.Setenv("HERDR_SESSION", "hseh-test")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
}

func readSpaceAssociationFile(t *testing.T, stateDir string) SpaceAssociationState {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(stateDir, "hseh-test", "associations.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state SpaceAssociationState
	if err := json.Unmarshal(payload, &state); err != nil {
		t.Fatalf("decode %s: %v", payload, err)
	}
	return state
}

func runCompiledHseh(env []string, args ...string) (string, error) {
	cmd := exec.Command(compiledHseh, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func countHerdrMethod(h *recordingHerdr, name string) int {
	n := 0
	for _, method := range h.methods {
		if method == name {
			n++
		}
	}
	return n
}

func TestParseRecoverArgsRejectsUnknownAndMissingChoice(t *testing.T) {
	if _, _, _, err := parseRecoverArgs(nil); err == nil || !strings.Contains(err.Error(), "definition id is required") {
		t.Fatalf("empty: %v", err)
	}
	if _, _, _, err := parseRecoverArgs([]string{"def-a"}); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("missing choice: %v", err)
	}
	if _, _, _, err := parseRecoverArgs([]string{"def-a", "--create", "--workspace", "w1"}); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("both: %v", err)
	}
	if _, _, _, err := parseRecoverArgs([]string{"def-a", "--create", "extra"}); err == nil || !strings.Contains(err.Error(), "unknown argument extra") {
		t.Fatalf("trailing: %v", err)
	}
	for _, args := range [][]string{{"demo", "--workspace=", "--create"}, {"demo", "--workspace", "", "--create"}, {"demo", "--workspace", " \t", "--create"}, {"demo", "--workspace="}} {
		if _, _, _, err := parseRecoverArgs(args); err == nil {
			t.Fatalf("empty workspace accepted: %q", args)
		}
	}
	id, ws, create, err := parseRecoverArgs([]string{"def-a", "--workspace", "w2"})
	if err != nil || id != "def-a" || ws != "w2" || create {
		t.Fatalf("workspace form %s %s %v %v", id, ws, create, err)
	}
}

func TestReconcileMovesCurrentRecordsToUnresolvedWithoutWrite(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("HERDR_SESSION", "hseh-test")
	stale := SpaceAssociationState{
		Witness: ServerContinuityWitness{SocketPath: "/tmp/s", PeerPID: 9, PeerStartTime: "1", BootTime: "1"},
		Records: []SpaceAssociationRecord{
			{DefinitionID: "def-a", ResolvedDir: "/tmp/a", WorkspaceID: "w1"},
			{DefinitionID: "def-b", ResolvedDir: "/tmp/b", WorkspaceID: "w2"},
		},
	}
	if err := writeSpaceAssociationFile(stateDir, stale); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(spaceAssociationStatePath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	live := ServerContinuityWitness{SocketPath: "/tmp/s", PeerPID: 9, PeerStartTime: "2", BootTime: "1"}
	got, err := loadReconciledSpaceAssociationState(stateDir, live)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 0 {
		t.Fatalf("current records after mismatch: %+v", got.Records)
	}
	if len(got.Unresolved) != 2 {
		t.Fatalf("unresolved %+v", got.Unresolved)
	}
	after, err := os.ReadFile(spaceAssociationStatePath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("picker-style load wrote disk")
	}
}

func TestDefinitionPickerShowsRecoveryCommands(t *testing.T) {
	def := SpaceDefinition{ID: "def-rec", Name: "rec", ResolvedDir: "/tmp/x"}
	item := definitionPickerItem(def, true)
	joined := strings.Join(item.Rows, "\n")
	if !strings.Contains(joined, "recovery needed") {
		t.Fatalf("rows %v", item.Rows)
	}
	if !strings.Contains(joined, "hseh recover def-rec --workspace <live-workspace-id>") || !strings.Contains(item.PreviewText, "hseh recover def-rec --create") {
		t.Fatalf("commands missing: rows=%v preview=%s", item.Rows, item.PreviewText)
	}
}

func TestCompiledRecoverKeepsSiblingUnresolvedAfterFreshInvocation(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-a", "alpha", dirA, "echo A")
	writeTestDefinition(t, config, "def-b", "beta", dirB, "echo B")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}}
	socket, state := startRecordingHerdr(t, h)
	useHsehTestSession(t, state)
	stale := SpaceAssociationState{
		Witness: ServerContinuityWitness{SocketPath: socket, PeerPID: 999999, PeerStartTime: "1", BootTime: "1"},
		Records: []SpaceAssociationRecord{
			{DefinitionID: "def-a", ResolvedDir: dirA, WorkspaceID: "w9"},
			{DefinitionID: "def-b", ResolvedDir: dirB, WorkspaceID: "w8"},
		},
	}
	if err := writeSpaceAssociationFile(state, stale); err != nil {
		t.Fatal(err)
	}
	env := hsehOpenEnv(socket, state, config)
	out, err := runCompiledHseh(env, "open", "def-a")
	if err == nil || !strings.Contains(out, "needs recovery") {
		t.Fatalf("open a: %v %s", err, out)
	}
	out, err = runCompiledHseh(env, "recover", "def-a", "--create")
	if err != nil {
		t.Fatalf("recover a: %v %s", err, out)
	}
	if !strings.Contains(out, "create") {
		t.Fatalf("recover a action: %s", out)
	}
	out, err = runCompiledHseh(env, "open", "def-b")
	if err == nil || !strings.Contains(out, "needs recovery") {
		t.Fatalf("open b after a recovered: %v %s", err, out)
	}
	stored := readSpaceAssociationFile(t, state)
	if len(stored.Records) != 1 || stored.Records[0].DefinitionID != "def-a" {
		t.Fatalf("current %+v", stored.Records)
	}
	if len(stored.Unresolved) != 1 || stored.Unresolved[0].DefinitionID != "def-b" {
		t.Fatalf("unresolved sibling %+v", stored.Unresolved)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != 1 {
		t.Fatalf("created %v", h.created)
	}
}

func TestCompiledRecoverRepeatGenerationKeepsUnresolvedSiblings(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-a", "alpha", dirA, "echo A")
	writeTestDefinition(t, config, "def-b", "beta", dirB, "echo B")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}}
	socket, state := startRecordingHerdr(t, h)
	useHsehTestSession(t, state)
	stale := SpaceAssociationState{
		Witness: ServerContinuityWitness{SocketPath: socket, PeerPID: 1, PeerStartTime: "1", BootTime: "1"},
		Records: []SpaceAssociationRecord{
			{DefinitionID: "def-a", ResolvedDir: dirA, WorkspaceID: "w9"},
			{DefinitionID: "def-b", ResolvedDir: dirB, WorkspaceID: "w8"},
		},
	}
	if err := writeSpaceAssociationFile(state, stale); err != nil {
		t.Fatal(err)
	}
	env := hsehOpenEnv(socket, state, config)
	if out, err := runCompiledHseh(env, "recover", "def-a", "--create"); err != nil {
		t.Fatalf("first recover: %v %s", err, out)
	}
	mid := readSpaceAssociationFile(t, state)
	if len(mid.Records) != 1 || mid.Records[0].DefinitionID != "def-a" || len(mid.Unresolved) != 1 || mid.Unresolved[0].DefinitionID != "def-b" {
		t.Fatalf("after first recover %+v / %+v", mid.Records, mid.Unresolved)
	}
	created := mid.Records[0].WorkspaceID
	mid.Witness = ServerContinuityWitness{SocketPath: socket, PeerPID: 2, PeerStartTime: "99", BootTime: "1"}
	if err := writeSpaceAssociationFile(state, mid); err != nil {
		t.Fatal(err)
	}
	out, err := runCompiledHseh(env, "open", "def-a")
	if err == nil || !strings.Contains(out, "needs recovery") {
		t.Fatalf("open a after second generation: %v %s", err, out)
	}
	out, err = runCompiledHseh(env, "open", "def-b")
	if err == nil || !strings.Contains(out, "needs recovery") {
		t.Fatalf("open b after second generation: %v %s", err, out)
	}
	out, err = runCompiledHseh(env, "recover", "def-a", "--workspace", created)
	if err != nil {
		t.Fatalf("reconnect a: %v %s", err, out)
	}
	if !strings.Contains(out, "reconnect") {
		t.Fatalf("want reconnect: %s", out)
	}
	got := readSpaceAssociationFile(t, state)
	if len(got.Records) != 1 || got.Records[0].DefinitionID != "def-a" {
		t.Fatalf("current after second recover %+v", got.Records)
	}
	if len(got.Unresolved) != 1 || got.Unresolved[0].DefinitionID != "def-b" {
		t.Fatalf("lost sibling %+v", got.Unresolved)
	}
}

func TestCompiledRecoverRefusesStolenCurrentAssociation(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-a", "alpha", dirA, "echo A")
	writeTestDefinition(t, config, "def-b", "beta", dirB, "echo B")
	mix := t.TempDir()
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{
		Version:    "0.9.0",
		Workspaces: []HerdrWorkspaceRow{{WorkspaceID: "w-mix", Label: "mixed", ActiveTabID: "w-mix:t1"}},
		Panes: []HerdrPaneRow{
			{PaneID: "w-mix:p1", WorkspaceID: "w-mix", Cwd: mix},
			{PaneID: "w-mix:p2", WorkspaceID: "w-mix", Cwd: dirB},
		},
	}}
	socket, state := startRecordingHerdr(t, h)
	useHsehTestSession(t, state)
	env := hsehOpenEnv(socket, state, config)
	if out, err := runCompiledHseh(env, "open", "def-a"); err != nil {
		t.Fatalf("open a: %v %s", err, out)
	}
	stored := readSpaceAssociationFile(t, state)
	owned := stored.Records[0].WorkspaceID
	stored.Unresolved = append(stored.Unresolved, SpaceAssociationRecord{DefinitionID: "def-b", ResolvedDir: dirB, WorkspaceID: "w-old"})
	if err := writeSpaceAssociationFile(state, stored); err != nil {
		t.Fatal(err)
	}
	out, err := runCompiledHseh(env, "recover", "def-b", "--workspace", owned)
	if err == nil || !strings.Contains(out, "associated with another definition") {
		t.Fatalf("steal: %v %s", err, out)
	}
	h.mu.Lock()
	creates := countHerdrMethod(h, "workspace.create")
	sends := len(h.commands)
	h.mu.Unlock()
	out, err = runCompiledHseh(env, "recover", "def-b", "--workspace", "w-mix")
	if err != nil {
		t.Fatalf("mixed reconnect: %v %s", err, out)
	}
	if !strings.Contains(out, "reconnect") {
		t.Fatalf("want reconnect mixed: %s", out)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if countHerdrMethod(h, "workspace.create") != creates {
		t.Fatalf("mixed reconnect created %v", h.created)
	}
	if len(h.commands) != sends {
		t.Fatalf("mixed reconnect sent %v", h.commands)
	}
}

func TestCompiledRecoverInvalidWorkspaceDoesNotMutate(t *testing.T) {
	dirA := t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-a", "alpha", dirA, "echo A")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}}
	socket, state := startRecordingHerdr(t, h)
	useHsehTestSession(t, state)
	stale := SpaceAssociationState{
		Witness: ServerContinuityWitness{SocketPath: socket, PeerPID: 9, PeerStartTime: "1"},
		Records: []SpaceAssociationRecord{{DefinitionID: "def-a", ResolvedDir: dirA, WorkspaceID: "w9"}},
	}
	if err := writeSpaceAssociationFile(state, stale); err != nil {
		t.Fatal(err)
	}
	env := hsehOpenEnv(socket, state, config)
	out, err := runCompiledHseh(env, "recover", "def-a", "--workspace", "w-missing")
	if err == nil || !strings.Contains(out, "is not live") {
		t.Fatalf("missing ws: %v %s", err, out)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != 0 || len(h.commands) != 0 {
		t.Fatalf("mutated %v %v", h.created, h.commands)
	}
}

func TestCompiledRecoverCreatePartialThenRepeatFocuses(t *testing.T) {
	work := t.TempDir()
	sub := filepath.Join(work, "web")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	config := t.TempDir()
	dir := filepath.Join(config, "spaces")
	os.MkdirAll(dir, 0o700)
	body := "id = \"def-partial\"\nname = \"partial\"\nworking_dir = \"" + work + "\"\n" +
		"[[tabs]]\nname = \"root\"\ncommand = \"echo FIRST\"\n" +
		"[[tabs]]\nname = \"web\"\nworking_dir = \"web\"\ncommand = \"echo SECOND\"\n"
	os.WriteFile(filepath.Join(dir, "partial.toml"), []byte(body), 0o600)
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}, failMethod: "tab.create"}
	socket, state := startRecordingHerdr(t, h)
	useHsehTestSession(t, state)
	stale := SpaceAssociationState{
		Witness: ServerContinuityWitness{SocketPath: socket, PeerPID: 3, PeerStartTime: "1"},
		Records: []SpaceAssociationRecord{{DefinitionID: "def-partial", ResolvedDir: work, WorkspaceID: "w9"}},
	}
	if err := writeSpaceAssociationFile(state, stale); err != nil {
		t.Fatal(err)
	}
	env := hsehOpenEnv(socket, state, config)
	out, err := runCompiledHseh(env, "recover", "def-partial", "--create")
	if err == nil || !strings.Contains(out, "tab.create") {
		t.Fatalf("expected partial: %v %s", err, out)
	}
	stored := readSpaceAssociationFile(t, state)
	if len(stored.Records) != 1 || stored.Records[0].DefinitionID != "def-partial" {
		t.Fatalf("partial must persist current %+v", stored.Records)
	}
	h.mu.Lock()
	created := append([]string{}, h.created...)
	commands := append([]string{}, h.commands...)
	h.mu.Unlock()
	out, err = runCompiledHseh(env, "recover", "def-partial", "--create")
	if err != nil {
		t.Fatalf("repeat: %v %s", err, out)
	}
	if !strings.Contains(out, "focus") {
		t.Fatalf("repeat action: %s", out)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != len(created) {
		t.Fatalf("repeat created %v", h.created)
	}
	if len(h.commands) != len(commands) {
		t.Fatalf("repeat commands %v", h.commands)
	}
}

func TestCompiledRecoverCreateIsNotGenericDuplicate(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-new", "fresh", work, "echo NEW")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}}
	socket, state := startRecordingHerdr(t, h)
	env := hsehOpenEnv(socket, state, config)
	out, err := runCompiledHseh(env, "recover", "def-new", "--create")
	if err == nil || !strings.Contains(out, "does not need recovery") {
		t.Fatalf("generic create: %v %s", err, out)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != 0 {
		t.Fatalf("created %v", h.created)
	}
}

func TestCompiledRecoverUnknownArgs(t *testing.T) {
	env := append(os.Environ(), "HERDR_SESSION=hseh-test")
	out, err := runCompiledHseh(env, "recover", "def-a", "--create", "--yes")
	if err == nil || !strings.Contains(out, "unknown argument --yes") {
		t.Fatalf("got %v %s", err, out)
	}
}

func TestCompiledListShowsRecoveryWhileLiveItemsRemain(t *testing.T) {
	dirA := t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-a", "alpha", dirA, "echo A")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{
		Version:    "0.9.0",
		Workspaces: []HerdrWorkspaceRow{{WorkspaceID: "w1", Label: "live", ActiveTabID: "w1:t1"}},
		Tabs:       []HerdrTabRow{{TabID: "w1:t1", WorkspaceID: "w1", Label: "live"}},
		Panes:      []HerdrPaneRow{{PaneID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Cwd: dirA}},
	}}
	socket, state := startRecordingHerdr(t, h)
	useHsehTestSession(t, state)
	stale := SpaceAssociationState{
		Witness: ServerContinuityWitness{SocketPath: socket, PeerPID: 8, PeerStartTime: "1"},
		Records: []SpaceAssociationRecord{{DefinitionID: "def-a", ResolvedDir: dirA, WorkspaceID: "w1"}},
	}
	if err := writeSpaceAssociationFile(state, stale); err != nil {
		t.Fatal(err)
	}
	env := hsehOpenEnv(socket, state, config)
	out, err := runCompiledHseh(env, "list", "--json")
	if err != nil {
		t.Fatalf("list: %v %s", err, out)
	}
	if !strings.Contains(out, `"kind":"space"`) && !strings.Contains(out, `"kind": "space"`) {
		if !strings.Contains(out, "live") {
			t.Fatalf("live item missing: %s", out)
		}
	}
	if !strings.Contains(out, "recovery needed") || !strings.Contains(out, "hseh recover def-a --create") {
		t.Fatalf("recovery missing: %s", out)
	}
}

func TestCompiledOpenUnrelatedDefinitionStillWorksAfterRestart(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	config := t.TempDir()
	writeTestDefinition(t, config, "def-a", "alpha", dirA, "echo A")
	writeTestDefinition(t, config, "def-b", "beta", dirB, "echo B")
	h := &recordingHerdr{snapshot: HerdrSessionSnapshot{Version: "0.9.0"}}
	socket, state := startRecordingHerdr(t, h)
	useHsehTestSession(t, state)
	stale := SpaceAssociationState{
		Witness: ServerContinuityWitness{SocketPath: socket, PeerPID: 4, PeerStartTime: "1"},
		Records: []SpaceAssociationRecord{{DefinitionID: "def-a", ResolvedDir: dirA, WorkspaceID: "w9"}},
	}
	if err := writeSpaceAssociationFile(state, stale); err != nil {
		t.Fatal(err)
	}
	env := hsehOpenEnv(socket, state, config)
	out, err := runCompiledHseh(env, "open", "def-b")
	if err != nil {
		t.Fatalf("unrelated open: %v %s", err, out)
	}
	if !strings.Contains(out, "create") {
		t.Fatalf("unrelated action: %s", out)
	}
}
