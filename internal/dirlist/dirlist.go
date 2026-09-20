package dirlist

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/regutierrez/hseh/internal/trace"
)

// ezaTimeout bounds one eza run. A directory on a stalled mount must not hold the preview.
const ezaTimeout = 2 * time.Second

// maxLines caps listing output so a huge directory cannot flood the preview.
const maxLines = 500

// ezaArgs are fixed: icons and names only. Color comes from Herdr tokens in
// the picker, not eza's own theme. -F marks directories so we can paint them.
var ezaArgs = []string{"--icons=always", "--color=never", "--group-directories-first", "-a", "-F"}

const copyEmptyDirectory = "(empty directory)"

var (
	ezaOnce sync.Once
	ezaPath string
	// lookPath is replaced by tests to force the builtin fallback.
	lookPath = exec.LookPath
)

func ezaBinary() string {
	ezaOnce.Do(func() { ezaPath, _ = lookPath("eza") })
	return ezaPath
}

// Read lists dir. It runs eza when available and falls back to a builtin
// listing otherwise. Color is applied later from Herdr tokens.
func Read(ctx context.Context, dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("no directory to preview")
	}
	span := trace.Span("dirlist.read", "dir", dir)
	defer span()
	if eza := ezaBinary(); eza != "" {
		return readEza(ctx, eza, dir)
	}
	return readBuiltin(dir)
}

func readEza(parent context.Context, eza, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, ezaTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, eza, append(append([]string{}, ezaArgs...), "--", dir)...)
	// eza runs in its own process group so a timeout kills any child it spawned too.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 250 * time.Millisecond
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if parent.Err() != nil {
		return "", parent.Err()
	}
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("eza timed out after %s", ezaTimeout)
	}
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			line, _, _ := strings.Cut(msg, "\n")
			return "", errors.New(line)
		}
		return "", err
	}
	return capLines(strings.TrimRight(out.String(), "\n")), nil
}

func readBuiltin(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		lines = append(lines, name)
	}
	return capLines(strings.Join(lines, "\n")), nil
}

func capLines(text string) string {
	if text == "" {
		return copyEmptyDirectory
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}
	return strings.Join(lines[:maxLines], "\n") + fmt.Sprintf("\n… %d more", len(lines)-maxLines)
}
