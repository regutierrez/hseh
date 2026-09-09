package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
	"github.com/regutierrez/hseh/internal/space"
)

func TestCompiledOpenCreatesThenFocusesWithoutRepeatingCommands(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	hsehtest.WriteDefinition(t, config, "def-one", "one", work, "echo MARK")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{Version: "0.9.0"}}
	socket, state := hsehtest.Start(t, h)
	run := func() []byte {
		cmd := exec.Command(compiledHseh, "open", "def-one")
		cmd.Env = hsehtest.Env(socket, state, config)
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
	if len(h.Created()) != 1 {
		t.Fatalf("created %v", h.Created())
	}
	if len(h.Commands()) != 1 || h.Commands()[0] != "echo MARK" {
		t.Fatalf("commands %v", h.Commands())
	}
}

func TestCompiledOpenAdoptsUnassociatedSameDir(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	hsehtest.WriteDefinition(t, config, "def-adopt", "adoptme", work, "echo SHOULD_NOT_RUN")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{
		Version:    "0.9.0",
		Workspaces: []herdr.WorkspaceRow{{WorkspaceID: "w7", Label: "live", ActiveTabID: "w7:t1"}},
		Panes:      []herdr.PaneRow{{PaneID: "w7:p1", WorkspaceID: "w7", Cwd: work}},
	}}
	socket, state := hsehtest.Start(t, h)
	cmd := exec.Command(compiledHseh, "open", "def-adopt")
	cmd.Env = hsehtest.Env(socket, state, config)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(string(out), "adopt") {
		t.Fatalf("want adopt, got %s", out)
	}
	if len(h.Created()) != 0 {
		t.Fatalf("created %v", h.Created())
	}
	if len(h.Commands()) != 0 {
		t.Fatalf("commands %v", h.Commands())
	}
}

func TestCompiledOpenMissingDirDoesNotCreate(t *testing.T) {
	config := t.TempDir()
	hsehtest.WriteDefinition(t, config, "def-missing", "missing", filepath.Join(t.TempDir(), "nope"), "echo X")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{Version: "0.9.0"}}
	socket, state := hsehtest.Start(t, h)
	cmd := exec.Command(compiledHseh, "open", "def-missing")
	cmd.Env = hsehtest.Env(socket, state, config)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected error, got %s", out)
	}
	for _, method := range h.Methods() {
		if method == "workspace.create" {
			t.Fatalf("created despite missing dir: %v", h.Methods())
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
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{Version: "0.9.0"}, FailMethod: "tab.create"}
	socket, state := hsehtest.Start(t, h)
	cmd := exec.Command(compiledHseh, "open", "def-partial")
	cmd.Env = hsehtest.Env(socket, state, config)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected tab.create failure, got %s", out)
	}
	if !strings.Contains(string(out), "tab.create") {
		t.Fatalf("error should name failed step: %s", out)
	}
	if len(h.Created()) != 1 {
		t.Fatalf("expected retained workspace, created %v", h.Created())
	}
	payload, err := os.ReadFile(filepath.Join(state, "hseh-test", "associations.json"))
	if err != nil {
		t.Fatalf("association missing after partial create: %v", err)
	}
	if !strings.Contains(string(payload), h.Created()[0]) {
		t.Fatalf("association does not retain workspace: %s", payload)
	}
}

func TestCompiledOpenConcurrentCreatesOnce(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	hsehtest.WriteDefinition(t, config, "def-race", "race", work, "echo ONCE")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{Version: "0.9.0"}}
	socket, state := hsehtest.Start(t, h)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			cmd := exec.Command(compiledHseh, "open", "def-race")
			cmd.Env = hsehtest.Env(socket, state, config)
			out, err := cmd.CombinedOutput()
			if err != nil {
				errs[i] = err
				t.Errorf("%v\n%s", err, out)
			}
		}(i)
	}
	wg.Wait()
	if len(h.Created()) != 1 {
		t.Fatalf("created %v methods %v", h.Created(), h.Methods())
	}
	if len(h.Commands()) != 1 {
		t.Fatalf("commands %v", h.Commands())
	}
}

func TestCompiledOpenDistinctDefinitionsSameDir(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	hsehtest.WriteDefinition(t, config, "def-a", "alpha", work, "echo A")
	hsehtest.WriteDefinition(t, config, "def-b", "beta", work, "echo B")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{Version: "0.9.0"}}
	socket, state := hsehtest.Start(t, h)
	for _, id := range []string{"def-a", "def-b"} {
		cmd := exec.Command(compiledHseh, "open", id)
		cmd.Env = hsehtest.Env(socket, state, config)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", id, err, out)
		}
	}
	if len(h.Created()) != 2 {
		t.Fatalf("want two workspaces, created %v", h.Created())
	}
	if len(h.Commands()) != 2 {
		t.Fatalf("commands %v", h.Commands())
	}
}

func TestCompiledOpenStopsOnMismatchedAssociationWitness(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	hsehtest.WriteDefinition(t, config, "def-restart", "restart", work, "echo NO")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{Version: "0.9.0"}}
	socket, state := hsehtest.Start(t, h)
	t.Setenv("HERDR_SESSION", "hseh-test")
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	stale := space.AssociationState{
		Witness: herdr.ContinuityWitness{SocketPath: socket, PeerPID: 999999, PeerStartTime: "1", BootTime: "1"},
		Records: []space.AssociationRecord{{DefinitionID: "def-restart", ResolvedDir: work, WorkspaceID: "w9"}},
	}
	if err := space.WriteAssociationFile(state, stale); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(compiledHseh, "open", "def-restart")
	cmd.Env = hsehtest.Env(socket, state, config)
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
	for _, method := range h.Methods() {
		if method == "workspace.create" || method == "workspace.focus" {
			t.Fatalf("mutated on unresolved restart: %v", h.Methods())
		}
	}
}

func TestCompiledOpenCorruptAssociationDoesNotCreate(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	hsehtest.WriteDefinition(t, config, "def-corrupt", "corrupt", work, "echo NO")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{Version: "0.9.0"}}
	socket, state := hsehtest.Start(t, h)
	os.MkdirAll(filepath.Join(state, "hseh-test"), 0o700)
	if err := os.WriteFile(filepath.Join(state, "hseh-test", "associations.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(compiledHseh, "open", "def-corrupt")
	cmd.Env = hsehtest.Env(socket, state, config)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected corrupt error, got %s", out)
	}
	if !strings.Contains(string(out), "corrupt") {
		t.Fatalf("got %s", out)
	}
	if len(h.Created()) != 0 {
		t.Fatalf("created %v", h.Created())
	}
}
