package space

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/regutierrez/hseh/internal/herdr"
	"github.com/regutierrez/hseh/internal/hsehtest"
	"github.com/regutierrez/hseh/internal/lockfile"
)

// useTestSession points Open at the fake server and the test's own state and config dirs.
func useTestSession(t *testing.T, socket, state, config string) {
	t.Helper()
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", config)
	t.Setenv("HERDR_SESSION", "hseh-test")
}

func TestOpenReusableSpaceCancelBeforeMutation(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	hsehtest.WriteDefinition(t, config, "def-cancel", "cancel", work, "echo NO")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{}, SnapshotDelay: 400 * time.Millisecond}
	socket, state := hsehtest.Start(t, h)
	useTestSession(t, socket, state, config)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Open(ctx, "def-cancel")
		done <- err
	}()
	time.Sleep(40 * time.Millisecond)
	cancel()
	err := <-done
	if err == nil {
		t.Fatal("expected cancel")
	}
	if len(h.Created()) != 0 {
		t.Fatalf("created after cancel: %v", h.Created())
	}
}

func TestOpenReusableSpaceLockedCancelBeforeMutation(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	hsehtest.WriteDefinition(t, config, "def-lock", "lock", work, "echo NO")
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{}}
	socket, state := hsehtest.Start(t, h)
	useTestSession(t, socket, state, config)
	release := make(chan struct{})
	held := make(chan struct{})
	go func() {
		_ = lockfile.WithExclusive(openLockPath(state), func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Open(ctx, "def-lock")
		done <- err
	}()
	time.Sleep(40 * time.Millisecond)
	cancel()
	err := <-done
	close(release)
	if err == nil {
		t.Fatal("expected cancel while lock held")
	}
	if len(h.Created()) != 0 {
		t.Fatalf("created while cancelled: %v", h.Created())
	}
}

func TestOpenReusableSpaceStopsAtFirstFailedCommand(t *testing.T) {
	work := t.TempDir()
	config := t.TempDir()
	dir := filepath.Join(config, "spaces")
	os.MkdirAll(dir, 0o700)
	body := "id = \"def-cmds\"\nname = \"cmds\"\nworking_dir = \"" + work + "\"\n" +
		"[[tabs]]\nname = \"a\"\ncommand = \"echo A\"\n" +
		"[[tabs]]\nname = \"b\"\ncommand = \"echo B\"\n"
	os.WriteFile(filepath.Join(dir, "cmds.toml"), []byte(body), 0o600)
	h := &hsehtest.Server{Snapshot: herdr.SessionSnapshot{}, FailMethod: "pane.send_input", FailAfter: 2}
	socket, state := hsehtest.Start(t, h)
	useTestSession(t, socket, state, config)
	result, err := Open(context.Background(), "def-cmds")
	if err == nil {
		t.Fatal("expected command failure")
	}
	if !strings.Contains(err.Error(), "pane.send_input") {
		t.Fatalf("step %v", err)
	}
	if result.WorkspaceID == "" {
		t.Fatalf("result %+v", result)
	}
	if got := h.Commands(); len(got) != 1 || got[0] != "echo A" {
		t.Fatalf("commands %v", got)
	}
}
