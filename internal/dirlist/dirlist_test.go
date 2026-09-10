package dirlist

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func withLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	prev, prevPath := lookPath, ezaPath
	lookPath = fn
	ezaOnce = sync.Once{}
	ezaPath = ""
	t.Cleanup(func() {
		lookPath, ezaPath = prev, prevPath
		ezaOnce = sync.Once{}
	})
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"zeta.txt", ".hidden", "alpha.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBuiltinFallbackListsDirectoriesFirstWithHidden(t *testing.T) {
	withLookPath(t, func(string) (string, error) { return "", os.ErrNotExist })
	got, err := Read(context.Background(), fixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if got != "sub/\n.hidden\nalpha.go\nzeta.txt" {
		t.Fatalf("got %q", got)
	}
}

func TestBuiltinFallbackReportsMissingDirectory(t *testing.T) {
	withLookPath(t, func(string) (string, error) { return "", os.ErrNotExist })
	if _, err := Read(context.Background(), filepath.Join(t.TempDir(), "gone")); err == nil {
		t.Fatal("missing directory did not fail")
	}
	if _, err := Read(context.Background(), ""); err == nil {
		t.Fatal("empty directory path did not fail")
	}
}

func TestEmptyDirectoryHasCopy(t *testing.T) {
	withLookPath(t, func(string) (string, error) { return "", os.ErrNotExist })
	got, err := Read(context.Background(), t.TempDir())
	if err != nil || got != copyEmptyDirectory {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestCapLinesTruncatesLargeListings(t *testing.T) {
	text := strings.TrimRight(strings.Repeat("line\n", maxLines+7), "\n")
	got := capLines(text)
	if lines := strings.Split(got, "\n"); len(lines) != maxLines+1 || lines[maxLines] != "… 7 more" {
		t.Fatalf("got %d lines, last %q", len(lines), lines[len(lines)-1])
	}
}

// fakeEza is a shell script standing in for eza so the exec path is tested without depending on the real tool.
func fakeEza(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "eza")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEzaReceivesFixedArgumentsAndDirectory(t *testing.T) {
	eza := fakeEza(t, `printf '%s\n' "$@"`)
	withLookPath(t, func(string) (string, error) { return eza, nil })
	dir := t.TempDir()
	got, err := Read(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join(append(append([]string{}, ezaArgs...), "--", dir), "\n")
	if got != want {
		t.Fatalf("args %q want %q", got, want)
	}
}

func TestEzaFailureSurfacesStderr(t *testing.T) {
	eza := fakeEza(t, `echo 'eza: "/nope": No such file or directory (os error 2)' >&2; exit 2`)
	withLookPath(t, func(string) (string, error) { return eza, nil })
	_, err := Read(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "No such file or directory") {
		t.Fatalf("err %v", err)
	}
}

// A cancelled read must kill eza's whole process group, not just the shell: the
// `sleep` child it spawned has to be gone too, and the caller's error wins.
func TestEzaTimeoutKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("HSEH_TEST_PIDFILE", pidFile)
	eza := fakeEza(t, `sleep 30 & echo $! > "$HSEH_TEST_PIDFILE"; wait`)
	withLookPath(t, func(string) (string, error) { return eza, nil })
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := Read(ctx, t.TempDir())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want the caller's deadline error, got %v", err)
	}
	raw, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("fake eza never recorded its child: %v", readErr)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("pid file %q: %v", raw, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("sleep child %d survived the process-group kill", pid)
	}
}
