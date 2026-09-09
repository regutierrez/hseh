package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
	"github.com/regutierrez/hseh/internal/space"
)

func useHsehTestSession(t *testing.T, stateDir string) {
	t.Helper()
	t.Setenv("HERDR_SESSION", "hseh-test")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", stateDir)
}

func readSpaceAssociationFile(t *testing.T, stateDir string) space.AssociationState {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(stateDir, "hseh-test", "associations.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state space.AssociationState
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

func TestCompiledRecoverKeepsSiblingUnresolvedAfterFreshInvocation(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	config := t.TempDir()
	hsehtest.WriteDefinition(t, config, "def-a", "alpha", dirA, "echo A")
	hsehtest.WriteDefinition(t, config, "def-b", "beta", dirB, "echo B")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{Version: "0.9.0"}}
	socket, state := hsehtest.Start(t, h)
	useHsehTestSession(t, state)
	stale := space.AssociationState{
		Witness: herdr.ContinuityWitness{SocketPath: socket, PeerPID: 999999, PeerStartTime: "1", BootTime: "1"},
		Records: []space.AssociationRecord{
			{DefinitionID: "def-a", ResolvedDir: dirA, WorkspaceID: "w9"},
			{DefinitionID: "def-b", ResolvedDir: dirB, WorkspaceID: "w8"},
		},
	}
	if err := space.WriteAssociationFile(state, stale); err != nil {
		t.Fatal(err)
	}
	env := hsehtest.Env(socket, state, config)
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
	if len(h.Created()) != 1 {
		t.Fatalf("created %v", h.Created())
	}
}

func TestCompiledRecoverRepeatGenerationKeepsUnresolvedSiblings(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	config := t.TempDir()
	hsehtest.WriteDefinition(t, config, "def-a", "alpha", dirA, "echo A")
	hsehtest.WriteDefinition(t, config, "def-b", "beta", dirB, "echo B")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{Version: "0.9.0"}}
	socket, state := hsehtest.Start(t, h)
	useHsehTestSession(t, state)
	stale := space.AssociationState{
		Witness: herdr.ContinuityWitness{SocketPath: socket, PeerPID: 1, PeerStartTime: "1", BootTime: "1"},
		Records: []space.AssociationRecord{
			{DefinitionID: "def-a", ResolvedDir: dirA, WorkspaceID: "w9"},
			{DefinitionID: "def-b", ResolvedDir: dirB, WorkspaceID: "w8"},
		},
	}
	if err := space.WriteAssociationFile(state, stale); err != nil {
		t.Fatal(err)
	}
	env := hsehtest.Env(socket, state, config)
	if out, err := runCompiledHseh(env, "recover", "def-a", "--create"); err != nil {
		t.Fatalf("first recover: %v %s", err, out)
	}
	mid := readSpaceAssociationFile(t, state)
	if len(mid.Records) != 1 || mid.Records[0].DefinitionID != "def-a" || len(mid.Unresolved) != 1 || mid.Unresolved[0].DefinitionID != "def-b" {
		t.Fatalf("after first recover %+v / %+v", mid.Records, mid.Unresolved)
	}
	created := mid.Records[0].WorkspaceID
	mid.Witness = herdr.ContinuityWitness{SocketPath: socket, PeerPID: 2, PeerStartTime: "99", BootTime: "1"}
	if err := space.WriteAssociationFile(state, mid); err != nil {
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
	hsehtest.WriteDefinition(t, config, "def-a", "alpha", dirA, "echo A")
	hsehtest.WriteDefinition(t, config, "def-b", "beta", dirB, "echo B")
	mix := t.TempDir()
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{
		Version:    "0.9.0",
		Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w-mix", Label: "mixed", ActiveTabID: "w-mix:t1"}},
		Panes: []herdr.PaneRow{
			{PaneID: "w-mix:p1", WorkspaceID: "w-mix", Cwd: mix},
			{PaneID: "w-mix:p2", WorkspaceID: "w-mix", Cwd: dirB},
		},
	}}
	socket, state := hsehtest.Start(t, h)
	useHsehTestSession(t, state)
	env := hsehtest.Env(socket, state, config)
	if out, err := runCompiledHseh(env, "open", "def-a"); err != nil {
		t.Fatalf("open a: %v %s", err, out)
	}
	stored := readSpaceAssociationFile(t, state)
	owned := stored.Records[0].WorkspaceID
	stored.Unresolved = append(stored.Unresolved, space.AssociationRecord{DefinitionID: "def-b", ResolvedDir: dirB, WorkspaceID: "w-old"})
	if err := space.WriteAssociationFile(state, stored); err != nil {
		t.Fatal(err)
	}
	out, err := runCompiledHseh(env, "recover", "def-b", "--workspace", owned)
	if err == nil || !strings.Contains(out, "associated with another definition") {
		t.Fatalf("steal: %v %s", err, out)
	}
	creates := h.Count("workspace.create")
	sends := len(h.Commands())
	out, err = runCompiledHseh(env, "recover", "def-b", "--workspace", "w-mix")
	if err != nil {
		t.Fatalf("mixed reconnect: %v %s", err, out)
	}
	if !strings.Contains(out, "reconnect") {
		t.Fatalf("want reconnect mixed: %s", out)
	}
	if h.Count("workspace.create") != creates {
		t.Fatalf("mixed reconnect created %v", h.Created())
	}
	if len(h.Commands()) != sends {
		t.Fatalf("mixed reconnect sent %v", h.Commands())
	}
}

func TestCompiledRecoverInvalidWorkspaceDoesNotMutate(t *testing.T) {
	dirA := t.TempDir()
	config := t.TempDir()
	hsehtest.WriteDefinition(t, config, "def-a", "alpha", dirA, "echo A")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{Version: "0.9.0"}}
	socket, state := hsehtest.Start(t, h)
	useHsehTestSession(t, state)
	stale := space.AssociationState{
		Witness: herdr.ContinuityWitness{SocketPath: socket, PeerPID: 9, PeerStartTime: "1"},
		Records: []space.AssociationRecord{{DefinitionID: "def-a", ResolvedDir: dirA, WorkspaceID: "w9"}},
	}
	if err := space.WriteAssociationFile(state, stale); err != nil {
		t.Fatal(err)
	}
	env := hsehtest.Env(socket, state, config)
	out, err := runCompiledHseh(env, "recover", "def-a", "--workspace", "w-missing")
	if err == nil || !strings.Contains(out, "is not live") {
		t.Fatalf("missing ws: %v %s", err, out)
	}
	if len(h.Created()) != 0 || len(h.Commands()) != 0 {
		t.Fatalf("mutated %v %v", h.Created(), h.Commands())
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
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{Version: "0.9.0"}, FailMethod: "tab.create"}
	socket, state := hsehtest.Start(t, h)
	useHsehTestSession(t, state)
	stale := space.AssociationState{
		Witness: herdr.ContinuityWitness{SocketPath: socket, PeerPID: 3, PeerStartTime: "1"},
		Records: []space.AssociationRecord{{DefinitionID: "def-partial", ResolvedDir: work, WorkspaceID: "w9"}},
	}
	if err := space.WriteAssociationFile(state, stale); err != nil {
		t.Fatal(err)
	}
	env := hsehtest.Env(socket, state, config)
	out, err := runCompiledHseh(env, "recover", "def-partial", "--create")
	if err == nil || !strings.Contains(out, "tab.create") {
		t.Fatalf("expected partial: %v %s", err, out)
	}
	stored := readSpaceAssociationFile(t, state)
	if len(stored.Records) != 1 || stored.Records[0].DefinitionID != "def-partial" {
		t.Fatalf("partial must persist current %+v", stored.Records)
	}
	created := append([]string{}, h.Created()...)
	commands := append([]string{}, h.Commands()...)
	out, err = runCompiledHseh(env, "recover", "def-partial", "--create")
	if err != nil {
		t.Fatalf("repeat: %v %s", err, out)
	}
	if !strings.Contains(out, "focus") {
		t.Fatalf("repeat action: %s", out)
	}
	if len(h.Created()) != len(created) {
		t.Fatalf("repeat created %v", h.Created())
	}
	if len(h.Commands()) != len(commands) {
		t.Fatalf("repeat commands %v", h.Commands())
	}
}

func TestCompiledRecoverCreateIsNotGenericDuplicate(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	hsehtest.WriteDefinition(t, config, "def-new", "fresh", work, "echo NEW")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{Version: "0.9.0"}}
	socket, state := hsehtest.Start(t, h)
	env := hsehtest.Env(socket, state, config)
	out, err := runCompiledHseh(env, "recover", "def-new", "--create")
	if err == nil || !strings.Contains(out, "does not need recovery") {
		t.Fatalf("generic create: %v %s", err, out)
	}
	if len(h.Created()) != 0 {
		t.Fatalf("created %v", h.Created())
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
	hsehtest.WriteDefinition(t, config, "def-a", "alpha", dirA, "echo A")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{
		Version:    "0.9.0",
		Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w1", Label: "live", ActiveTabID: "w1:t1"}},
		Tabs:       []herdr.TabRow{{TabID: "w1:t1", WorkspaceID: "w1", Label: "live"}},
		Panes:      []herdr.PaneRow{{PaneID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Cwd: dirA}},
	}}
	socket, state := hsehtest.Start(t, h)
	useHsehTestSession(t, state)
	stale := space.AssociationState{
		Witness: herdr.ContinuityWitness{SocketPath: socket, PeerPID: 8, PeerStartTime: "1"},
		Records: []space.AssociationRecord{{DefinitionID: "def-a", ResolvedDir: dirA, WorkspaceID: "w1"}},
	}
	if err := space.WriteAssociationFile(state, stale); err != nil {
		t.Fatal(err)
	}
	env := hsehtest.Env(socket, state, config)
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
	hsehtest.WriteDefinition(t, config, "def-a", "alpha", dirA, "echo A")
	hsehtest.WriteDefinition(t, config, "def-b", "beta", dirB, "echo B")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{Version: "0.9.0"}}
	socket, state := hsehtest.Start(t, h)
	useHsehTestSession(t, state)
	stale := space.AssociationState{
		Witness: herdr.ContinuityWitness{SocketPath: socket, PeerPID: 4, PeerStartTime: "1"},
		Records: []space.AssociationRecord{{DefinitionID: "def-a", ResolvedDir: dirA, WorkspaceID: "w9"}},
	}
	if err := space.WriteAssociationFile(state, stale); err != nil {
		t.Fatal(err)
	}
	env := hsehtest.Env(socket, state, config)
	out, err := runCompiledHseh(env, "open", "def-b")
	if err != nil {
		t.Fatalf("unrelated open: %v %s", err, out)
	}
	if !strings.Contains(out, "create") {
		t.Fatalf("unrelated action: %s", out)
	}
}
