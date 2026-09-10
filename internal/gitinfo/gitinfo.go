package gitinfo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/regutierrez/hseh/internal/termtext"
	"github.com/regutierrez/hseh/internal/trace"
)

// WorkspaceGit is read-only checkout detail. Empty fields mean a non-repository directory.
// Root is the checkout's top-level directory (a linked worktree reports its own path).
type WorkspaceGit struct {
	Branch string
	Status string
	Root   string
}

func Read(ctx context.Context, dir string) WorkspaceGit {
	if dir == "" {
		return WorkspaceGit{}
	}
	defer trace.Span("git.status", "dir", dir)()
	root, ok := readRoot(ctx, dir)
	if !ok {
		return WorkspaceGit{}
	}
	cmd := gitCommand(ctx, dir, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return WorkspaceGit{}
		}
		return WorkspaceGit{Status: "git status unavailable", Root: root}
	}
	result := ParseStatus(string(output))
	result.Root = root
	return result
}

// readRoot resolves the checkout top level. ok is false for non-repositories,
// cancelled reads, and directories git cannot inspect.
func readRoot(ctx context.Context, dir string) (root string, ok bool) {
	output, err := gitCommand(ctx, dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", false
	}
	root = strings.TrimSpace(string(output))
	return root, root != ""
}

func gitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", dir, "-c", "core.fsmonitor=false"}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return cmd
}

func ParseStatus(output string) WorkspaceGit {
	var result WorkspaceGit
	var oid string
	var staged, modified, untracked, conflicts, ahead, behind int
	records := strings.Split(output, "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		switch {
		case strings.HasPrefix(record, "# branch.head "):
			result.Branch = strings.TrimPrefix(record, "# branch.head ")
		case strings.HasPrefix(record, "# branch.oid "):
			oid = strings.TrimPrefix(record, "# branch.oid ")
		case strings.HasPrefix(record, "# branch.ab "):
			fields := strings.Fields(record)
			if len(fields) == 4 {
				ahead, _ = strconv.Atoi(strings.TrimPrefix(fields[2], "+"))
				behind, _ = strconv.Atoi(strings.TrimPrefix(fields[3], "-"))
			}
		case strings.HasPrefix(record, "? "):
			untracked++
		case strings.HasPrefix(record, "u "):
			conflicts++
		case strings.HasPrefix(record, "1 "), strings.HasPrefix(record, "2 "):
			if len(record) >= 4 {
				if record[2] != '.' {
					staged++
				}
				if record[3] != '.' {
					modified++
				}
			}
			if record[0] == '2' {
				i++
			} // The next NUL record is the original rename path, not another status.
		}
	}
	if result.Branch == "(detached)" {
		result.Branch = "@" + oid[:min(7, len(oid))]
	}
	var status []string
	for _, count := range []struct {
		symbol string
		n      int
	}{{"+", staged}, {"~", modified}, {"?", untracked}, {"!", conflicts}, {"↑", ahead}, {"↓", behind}} {
		if count.n > 0 {
			status = append(status, fmt.Sprintf("%s%d", count.symbol, count.n))
		}
	}
	result.Branch = strings.TrimSpace(termtext.StripControls(result.Branch))
	result.Status = strings.Join(status, " ")
	return result
}
